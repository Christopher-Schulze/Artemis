//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// orphanExitWait bounds how long recovery waits for a killed orphaned browser.
const orphanExitWait = 2 * time.Second

// ErrProfileOwnershipUnproven refuses a recovery that would signal a process
// it cannot prove belongs to the leased browser; the lease stays in place.
var ErrProfileOwnershipUnproven = errors.New("recover profile: a process of the recorded browser group is not proven to belong to this lease")

// errProcessGone reports a pid without a running process.
var errProcessGone = errors.New("process is gone")

// RecoverOrphanedProfile releases a profile Artemis leased to a process that
// died before closing its browser, for example after a hard kill during
// shutdown. The lease is live while its owner pid runs with the recorded start
// identity, so a reused owner pid does not keep an orphaned lease active.
// Otherwise the recorded browser group is ended, but only processes proven to
// belong to this lease are signalled: those that started with the lease token
// in their environment, and the guardian shell that names this owner and
// profile. Any other member of that group, for example after the group id was
// reused, refuses the recovery with ErrProfileOwnershipUnproven and keeps the
// lease. Every process whose command line carries exactly this profile's
// --user-data-dir is killed as well, and the stale lease and DevToolsActivePort
// file are removed. Without a lease nothing proves the browser is ours, so a
// profile without a lease is left unchanged.
func RecoverOrphanedProfile(ctx context.Context, profileDir string) error {
	abs, err := filepath.Abs(profileDir)
	if err != nil {
		return fmt.Errorf("recover profile: %w", err)
	}
	leasePath := filepath.Join(abs, profileLeaseName)
	raw, err := os.ReadFile(leasePath) // #nosec G304 -- leasePath is the fixed lease file inside the configured profile.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recover profile: read lease: %w", err)
	}
	lease := parseProfileLease(raw)
	if lease.owner == os.Getpid() || leaseOwnerAlive(lease) {
		return nil
	}
	// The recorded group reaches the helpers even when the main process already
	// exited and no helper names the profile on its command line. A lease from
	// before tokens existed cannot prove its group and leaves it alone.
	if lease.group > 1 && lease.token != "" {
		if err := killProvenBrowserGroup(ctx, lease, abs); err != nil {
			return err
		}
	}
	if err := killOrphanedBrowser(ctx, abs); err != nil {
		return err
	}
	for _, stale := range []string{leasePath, filepath.Join(abs, "DevToolsActivePort")} {
		if err := os.Remove(stale); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("recover profile: remove %s: %w", filepath.Base(stale), err)
		}
	}
	return nil
}

// leaseOwnerAlive reports whether the lease owner still runs as the process
// that wrote the lease. A legacy lease without start identity trusts the pid.
func leaseOwnerAlive(lease profileLease) bool {
	if lease.owner <= 1 || !pidAlive(lease.owner) {
		return false
	}
	if lease.ownerStart == "" {
		return true
	}
	start, err := processStartID(lease.owner)
	return err == nil && start == lease.ownerStart
}

// killOrphanedBrowser kills every process running on profileDir and waits for
// them to exit; the process list, not Chromium's SingletonLock, identifies
// them, because Chromium removes the lock early in its own shutdown while it
// still writes into the profile.
func killOrphanedBrowser(ctx context.Context, profileDir string) error {
	listing, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,command=").Output() // #nosec G204 -- fixed ps argv.
	if err != nil {
		return fmt.Errorf("recover profile: list processes: %w", err)
	}
	marker := "--user-data-dir=" + profileDir
	var orphans []int
	for _, line := range strings.Split(string(listing), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(line+" ", marker+" ") {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		orphans = append(orphans, pid)
	}
	for _, pid := range orphans {
		// The browser leads its own process group (Setpgid), so the group kill
		// also reaches helpers; a helper pid is not a group leader.
		if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("recover profile: kill orphaned browser %d: %w", pid, err)
		}
	}
	deadline := time.Now().Add(orphanExitWait)
	for _, pid := range orphans {
		for pidAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if pidAlive(pid) {
			return fmt.Errorf("recover profile: orphaned browser %d did not exit", pid)
		}
	}
	return nil
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// recordLeaseBrowserGroup adds the started browser's process group to the
// profile lease, keeping its owner identity and token, so a recovery after the
// owner died can end the proven members of that group.
func recordLeaseBrowserGroup(leasePath string, group int) error {
	if leasePath == "" {
		return nil
	}
	lease, err := readProfileLease(leasePath)
	if err != nil {
		return fmt.Errorf("record browser group: %w", err)
	}
	lease.group = group
	if err := os.WriteFile(leasePath, lease.encode(), 0o600); err != nil {
		return fmt.Errorf("record browser group: %w", err)
	}
	return nil
}

// killProvenBrowserGroup kills the members of the recorded browser group that
// prove they belong to this lease and waits for them to end. One unproven
// member refuses the whole recovery before any signal is sent.
func killProvenBrowserGroup(ctx context.Context, lease profileLease, profileDir string) error {
	listing, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,pgid=,command=").Output() // #nosec G204 -- fixed ps argv.
	if err != nil {
		return fmt.Errorf("recover profile: list processes: %w", err)
	}
	tokenEntry := profileLeaseTokenEnv + "=" + lease.token
	guardian := "artemis-process-owner " + strconv.Itoa(lease.owner) + " " + profileDir + " "
	var proven, unproven []int
	for _, line := range strings.Split(string(listing), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		group, groupErr := strconv.Atoi(fields[1])
		if pidErr != nil || groupErr != nil || group != lease.group || pid == os.Getpid() {
			continue
		}
		if strings.Contains(line+" ", guardian) {
			proven = append(proven, pid)
			continue
		}
		carries, envErr := processEnvironContains(pid, tokenEntry)
		if envErr != nil && !pidAlive(pid) {
			continue
		}
		if carries {
			proven = append(proven, pid)
			continue
		}
		unproven = append(unproven, pid)
	}
	if len(unproven) != 0 {
		return fmt.Errorf("%w: group %d, processes %v", ErrProfileOwnershipUnproven, lease.group, unproven)
	}
	for _, pid := range proven {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("recover profile: kill browser process %d: %w", pid, err)
		}
	}
	deadline := time.Now().Add(orphanExitWait)
	for _, pid := range proven {
		for pidAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if pidAlive(pid) {
			return fmt.Errorf("recover profile: browser process %d did not exit", pid)
		}
	}
	return nil
}
