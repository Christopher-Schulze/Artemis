//go:build darwin

package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumLaunchCloseCyclesDoNotGrowCodeSignClones(t *testing.T) {
	binary := requireChromium(t)
	cloneRoot := filepath.Join(filepath.Dir(filepath.Clean(os.TempDir())), "X")
	if info, err := os.Stat(cloneRoot); err != nil || !info.IsDir() {
		t.Fatalf("macOS code-sign clone root %q is unavailable: %v", cloneRoot, err)
	}
	before := codeSignCloneSnapshot(t, cloneRoot)
	profile := t.TempDir()
	for cycle := 0; cycle < 10; cycle++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{
			BinaryPath: binary.Path, UserDataDir: profile, Headless: true,
			StartupTimeout: 10 * time.Second, ShutdownTimeout: 5 * time.Second,
		})
		if err != nil {
			cancel()
			t.Fatalf("launch/close cycle %d: launch: %v", cycle+1, err)
		}
		closeErr := browser.Close()
		cancel()
		if closeErr != nil {
			t.Fatalf("launch/close cycle %d: close: %v", cycle+1, closeErr)
		}
	}
	after := codeSignCloneSnapshot(t, cloneRoot)
	t.Logf("code-sign clone count before=%d after=%d", len(before), len(after))
	for path := range after {
		if _, existed := before[path]; !existed {
			t.Errorf("launch/close cycles left new code-sign clone %q", path)
		}
	}
	if len(after) > len(before) {
		t.Fatalf("code-sign clone count grew from %d to %d", len(before), len(after))
	}
}

func codeSignCloneSnapshot(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "*.code_sign_clone", "code_sign_clone.*"))
	if err != nil {
		t.Fatalf("snapshot code-sign clones: %v", err)
	}
	snapshot := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		snapshot[path] = struct{}{}
	}
	return snapshot
}
