//go:build darwin || linux

package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func startFakeBrowser(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"-c", "trap '' TERM INT; while :; do sleep 1; done", "fake-chromium"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Reap at once, as launchd or init reaps a real orphan, so a killed fake
	// browser does not linger as a zombie of the test process.
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-reaped
	})
	return cmd
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeProfileState(t *testing.T, profile string, owner, browserPID int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(profile, profileLeaseName), []byte(strconv.Itoa(owner)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte("43210\n/devtools/browser/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fmt.Sprintf("host-%d", browserPID), filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
}

// A browser left running on a profile whose leasing process died is killed
// and the stale lease and DevTools port are removed, so the next launch works.
func TestRecoverOrphanedProfileKillsTheOrphanAndReleasesTheProfile(t *testing.T) {
	profile, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	orphan := startFakeBrowser(t, "--user-data-dir="+profile)
	writeProfileState(t, profile, deadPID(t), orphan.Process.Pid)
	if err := RecoverOrphanedProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	if pidAlive(orphan.Process.Pid) {
		t.Fatal("the orphaned browser still runs")
	}
	for _, name := range []string{profileLeaseName, "DevToolsActivePort"} {
		if _, err := os.Stat(filepath.Join(profile, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was not removed: %v", name, err)
		}
	}
}

// A live leasing process keeps its profile untouched, and a process whose pid
// the SingletonLock names but that does not run on the profile is never killed.
func TestRecoverOrphanedProfileLeavesLiveOwnersAndForeignProcesses(t *testing.T) {
	live, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	browser := startFakeBrowser(t, "--user-data-dir="+live)
	writeProfileState(t, live, os.Getppid(), browser.Process.Pid)
	if err := RecoverOrphanedProfile(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	if !pidAlive(browser.Process.Pid) {
		t.Fatal("recovery killed the browser of a live owner")
	}
	if _, err := os.Stat(filepath.Join(live, profileLeaseName)); err != nil {
		t.Fatalf("the live lease was removed: %v", err)
	}

	reused, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign := startFakeBrowser(t, "--user-data-dir=/somewhere/else")
	writeProfileState(t, reused, deadPID(t), foreign.Process.Pid)
	if err := RecoverOrphanedProfile(t.Context(), reused); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !pidAlive(foreign.Process.Pid) {
		t.Fatal("recovery killed a process that does not run on the profile")
	}
}

// Without an Artemis lease nothing proves a browser on the profile is ours, so
// it is never killed; with a stale lease a browser on a sibling profile whose
// path merely starts with the same prefix is not touched either.
func TestRecoverOrphanedProfileKillsOnlyLeaseProvenBrowsersOfExactlyThisProfile(t *testing.T) {
	unleased, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign := startFakeBrowser(t, "--user-data-dir="+unleased)
	if err := RecoverOrphanedProfile(t.Context(), unleased); err != nil {
		t.Fatal(err)
	}
	if !pidAlive(foreign.Process.Pid) {
		t.Fatal("recovery killed a browser on a profile Artemis does not lease")
	}
	profile, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, profileLeaseName), []byte(strconv.Itoa(deadPID(t))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := startFakeBrowser(t, "--user-data-dir="+profile)
	sibling := startFakeBrowser(t, "--user-data-dir="+profile+"-2")
	if err := RecoverOrphanedProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	if pidAlive(orphan.Process.Pid) {
		t.Fatal("the orphan of the stale lease still runs")
	}
	if !pidAlive(sibling.Process.Pid) {
		t.Fatal("recovery killed a browser of a sibling profile")
	}
}
