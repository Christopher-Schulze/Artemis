//go:build darwin || linux

package process

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sleeperEnv turns the test binary into a long-running helper process. The
// test binary is not a platform binary, so macOS exposes its environment the
// way it exposes Chromium's.
const sleeperEnv = "ARTEMIS_PROCESS_TEST_SLEEPER"

func TestMain(m *testing.M) {
	if os.Getenv(sleeperEnv) == "1" {
		signal.Ignore(syscall.SIGTERM, syscall.SIGINT)
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startOrphanedGroup starts a group whose leader exits at once and leaves one
// helper of the test binary behind, with the given extra environment, as a
// browser leaves its helpers after a hard kill. It returns the group and the
// helper pid.
func startOrphanedGroup(t *testing.T, env ...string) (int, int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	leader := exec.Command("sh", "-c", `"$1" & echo $! > "$2"; exit 0`, "main", os.Args[0], pidFile)
	leader.Env = append(append(os.Environ(), sleeperEnv+"=1"), env...)
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Run(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	helper, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Kill(helper, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Error(err)
		}
	})
	// The helper must have exec'ed the test binary before its environment
	// can be read.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if found, err := processEnvironContains(helper, sleeperEnv+"=1"); err == nil && found {
			return leader.Process.Pid, helper
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the helper did not start")
	return 0, 0
}

func writeLease(t *testing.T, profile string, lease profileLease) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(profile, profileLeaseName), lease.encode(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func tempProfile(t *testing.T) string {
	t.Helper()
	profile, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

// When the browser's main process already exited, a helper of the recorded
// group that carries the lease token is killed, although it does not name the
// profile.
func TestRecoverOrphanedProfileKillsProvenHelpersAfterTheMainProcessExited(t *testing.T) {
	profile := tempProfile(t)
	group, helper := startOrphanedGroup(t, profileLeaseTokenEnv+"=token-a")
	writeLease(t, profile, profileLease{owner: deadPID(t), ownerStart: "1.000000", token: "token-a", group: group})
	if err := RecoverOrphanedProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for pidAlive(helper) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(helper) {
		t.Fatal("the proven helper of the recorded browser group still runs")
	}
	if _, err := os.Stat(filepath.Join(profile, profileLeaseName)); !os.IsNotExist(err) {
		t.Fatalf("the stale lease was not removed: %v", err)
	}
}

// A member of the recorded group without the lease token, as after the group
// id was reused, is never signalled: the recovery reports the conflict and
// keeps the lease. A legacy lease without token never signals its group.
func TestRecoverOrphanedProfileNeverSignalsUnprovenGroupMembers(t *testing.T) {
	profile := tempProfile(t)
	group, stranger := startOrphanedGroup(t, profileLeaseTokenEnv+"=someone-else")
	writeLease(t, profile, profileLease{owner: deadPID(t), ownerStart: "1.000000", token: "token-a", group: group})
	if err := RecoverOrphanedProfile(t.Context(), profile); !errors.Is(err, ErrProfileOwnershipUnproven) {
		t.Fatalf("recovery over an unproven group member = %v, want ErrProfileOwnershipUnproven", err)
	}
	if !pidAlive(stranger) {
		t.Fatal("recovery killed a process that does not carry the lease token")
	}
	if _, err := os.Stat(filepath.Join(profile, profileLeaseName)); err != nil {
		t.Fatalf("the conflicting lease was removed: %v", err)
	}

	legacy := tempProfile(t)
	legacyGroup, legacyHelper := startOrphanedGroup(t)
	if err := os.WriteFile(filepath.Join(legacy, profileLeaseName), []byte(strconv.Itoa(deadPID(t))+"\n"+strconv.Itoa(legacyGroup)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverOrphanedProfile(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !pidAlive(legacyHelper) {
		t.Fatal("a legacy lease without token signalled its group")
	}
}

// An owner pid that another process reuses does not keep an orphaned lease
// active: the start identity differs, so the orphan is recovered. The real
// owner with its recorded identity keeps the lease.
func TestRecoverOrphanedProfileDetectsAReusedOwnerPid(t *testing.T) {
	profile := tempProfile(t)
	orphan := startFakeBrowser(t, "--user-data-dir="+profile)
	reusedOwner := os.Getppid()
	writeLease(t, profile, profileLease{owner: reusedOwner, ownerStart: "1.000000", token: "token-a"})
	if err := RecoverOrphanedProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	if pidAlive(orphan.Process.Pid) {
		t.Fatal("a reused owner pid kept the orphaned browser alive")
	}

	live := tempProfile(t)
	browser := startFakeBrowser(t, "--user-data-dir="+live)
	start, err := processStartID(reusedOwner)
	if err != nil {
		t.Fatal(err)
	}
	writeLease(t, live, profileLease{owner: reusedOwner, ownerStart: start, token: "token-b"})
	if err := RecoverOrphanedProfile(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	if !pidAlive(browser.Process.Pid) {
		t.Fatal("recovery killed the browser of a live owner")
	}
}

// A new lease records this process with its start identity and a token, the
// browser group keeps both, and the browser environment carries the token.
func TestTheProfileLeaseRecordsOwnerIdentityAndToken(t *testing.T) {
	profile := tempProfile(t)
	path, err := acquireProfileLease(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordLeaseBrowserGroup(path, 4242); err != nil {
		t.Fatal(err)
	}
	lease, err := readProfileLease(path)
	if err != nil {
		t.Fatal(err)
	}
	start, err := processStartID(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if lease.owner != os.Getpid() || lease.ownerStart != start || len(lease.token) != 32 || lease.group != 4242 {
		t.Fatalf("lease %+v", lease)
	}
	found := false
	for _, entry := range browserEnvironment(lease) {
		found = found || entry == profileLeaseTokenEnv+"="+lease.token
	}
	if !found {
		t.Fatal("the browser environment does not carry the lease token")
	}
}
