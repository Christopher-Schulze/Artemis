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

// RecoverOrphanedProfile releases a profile Artemis leased to a process that
// died before closing its browser, for example after a hard kill during
// shutdown: every process whose command line carries exactly this profile's
// --user-data-dir is killed with its group (a reused pid of another program
// never matches), and the stale lease and DevToolsActivePort file that would
// block the next launch are removed. Without an Artemis lease nothing proves
// the browser is ours, so a profile without a lease, or leased by a live
// process, is left unchanged.
func RecoverOrphanedProfile(ctx context.Context, profileDir string) error {
	abs, err := filepath.Abs(profileDir)
	if err != nil {
		return fmt.Errorf("recover profile: %w", err)
	}
	lease := filepath.Join(abs, profileLeaseName)
	raw, err := os.ReadFile(lease) // #nosec G304 -- lease is the fixed lease file inside the configured profile.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recover profile: read lease: %w", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) > 0 {
		if owner, parseErr := strconv.Atoi(fields[0]); parseErr == nil && (owner == os.Getpid() || pidAlive(owner)) {
			return nil
		}
	}
	// The lease's second field is the browser's process group: it reaches the
	// helpers even when the main process already exited and no helper names
	// the profile on its command line.
	if len(fields) > 1 {
		if group, parseErr := strconv.Atoi(fields[1]); parseErr == nil && group > 1 {
			if err := killBrowserGroup(group); err != nil {
				return err
			}
		}
	}
	if err := killOrphanedBrowser(ctx, abs); err != nil {
		return err
	}
	for _, stale := range []string{lease, filepath.Join(abs, "DevToolsActivePort")} {
		if err := os.Remove(stale); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("recover profile: remove %s: %w", filepath.Base(stale), err)
		}
	}
	return nil
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
// profile lease, so a recovery after the owner died can end the whole group.
func recordLeaseBrowserGroup(lease string, group int) error {
	if lease == "" {
		return nil
	}
	if err := os.WriteFile(lease, []byte(fmt.Sprintf("%d\n%d\n", os.Getpid(), group)), 0o600); err != nil {
		return fmt.Errorf("record browser group: %w", err)
	}
	return nil
}

// killBrowserGroup kills an orphaned browser process group and waits for it
// to end.
func killBrowserGroup(group int) error {
	if err := syscall.Kill(-group, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) {
			return nil
		}
		return fmt.Errorf("recover profile: kill orphaned browser group %d: %w", group, err)
	}
	deadline := time.Now().Add(orphanExitWait)
	for syscall.Kill(-group, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(-group, 0) == nil {
		return fmt.Errorf("recover profile: orphaned browser group %d did not exit", group)
	}
	return nil
}
