//go:build darwin || linux

package process

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The main process exits on SIGTERM while a helper of its group ignores it
// and keeps writing into the profile, as Chromium helpers can during shutdown.
const browserWithLingeringHelperScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
sh -c 'trap "" TERM INT; while :; do date > "$1/helper-write"; sleep 0.05; done' helper "$profile" &
printf '%s\n' "$!" > "$profile/helper.pid"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
trap 'exit 0' TERM INT
while :; do sleep 1; done
`

// A graceful close releases the configured profile only after every process
// of the browser's group has ended, so nothing writes into it afterwards.
func TestCloseReleasesTheProfileOnlyAfterLingeringHelpersEnd(t *testing.T) {
	profile, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserWithLingeringHelperScript), Headless: true,
		UserDataDir: profile, StartupTimeout: 5 * time.Second, ShutdownTimeout: 4 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(profile, "helper.pid"))
	if err != nil {
		t.Fatal(err)
	}
	helper, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(profile, "helper-write")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the helper never wrote into the profile")
		}
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	if pidAlive(helper) {
		t.Fatal("a helper still runs after the profile was released")
	}
	if _, err := os.Stat(filepath.Join(profile, profileLeaseName)); !os.IsNotExist(err) {
		t.Fatalf("lease after close: %v", err)
	}
	before, err := os.Stat(filepath.Join(profile, "helper-write"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if after, err := os.Stat(filepath.Join(profile, "helper-write")); err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the profile was written after close: %v", err)
	}
}
