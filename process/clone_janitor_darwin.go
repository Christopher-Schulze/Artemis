//go:build darwin

package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sweepOrphanCodeSignClones removes orphaned macOS code-sign clone directories
// left behind by browser launches.
//
// When macOS launches and validates a signed application bundle (any browser,
// Chrome or Chromium alike) it creates a copy-on-write clone under the per-user
// app directory: <darwin_user_dir>/X/com.<bundle>.code_sign_clone/code_sign_clone.<id>.
// The clone is normally reclaimed when the process exits cleanly, but if the
// browser is killed before it exits (SIGKILL teardown, crash, cancelled run) the
// clone is orphaned and never reclaimed. On developer machines that repeatedly
// launch and tear down a browser this accumulates into tens of GB of dead clone
// directories.
//
// This best-effort sweep removes clone directories that no live process still
// holds open. It installs nothing, launches nothing, and never touches an
// installed browser bundle. It is a no-op on non-darwin platforms.
func SweepOrphanCodeSignClones() {
	base := codeSignCloneDir()
	if base == "" {
		return
	}
	inUse, ok := openCloneIDs()
	if !ok {
		// Could not determine which clones are live; do not risk deleting a
		// clone that still backs a running browser.
		return
	}
	removeOrphanClones(base, inUse)
}

// codeSignCloneDir returns the per-user directory that macOS uses for code-sign
// clones, derived from the process temp dir (.../T -> .../X). It returns "" when
// the directory cannot be resolved or does not exist.
func codeSignCloneDir() string {
	tmp := os.TempDir()
	if tmp == "" {
		return ""
	}
	x := filepath.Join(filepath.Dir(filepath.Clean(tmp)), "X")
	if info, err := os.Stat(x); err != nil || !info.IsDir() {
		return ""
	}
	return x
}

// removeOrphanClones removes every code-sign clone directory under base whose
// directory name is not present in inUse. It is best-effort: directories that
// fail to remove are skipped. It returns the directories it removed.
func removeOrphanClones(base string, inUse map[string]bool) []string {
	clones, err := filepath.Glob(filepath.Join(base, "*.code_sign_clone", "code_sign_clone.*"))
	if err != nil || len(clones) == 0 {
		return nil
	}
	removed := make([]string, 0, len(clones))
	for _, dir := range clones {
		if inUse[filepath.Base(dir)] {
			continue
		}
		if err := os.RemoveAll(dir); err == nil {
			removed = append(removed, dir)
		}
	}
	return removed
}

// openCloneIDs returns the set of code-sign clone directory names that a live
// process currently holds open, taken from a single lsof snapshot. The bool is
// false when lsof cannot be run, so callers can skip the sweep rather than treat
// "no signal" as "nothing in use".
func openCloneIDs() (map[string]bool, bool) {
	out, err := exec.CommandContext(context.Background(), "lsof", "-Fn").Output()
	if err != nil {
		return nil, false
	}
	ids := make(map[string]bool)
	const marker = "code_sign_clone."
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(marker):]
		if end := strings.IndexAny(rest, "/ \t"); end >= 0 {
			rest = rest[:end]
		}
		if rest != "" {
			ids[marker+rest] = true
		}
	}
	return ids, true
}
