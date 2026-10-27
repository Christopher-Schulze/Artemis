//go:build darwin || linux

package corpus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/process"
)

// TestRunChromiumNamesTheLaunchPhaseOfAnExpiredDeadline drives the real
// Chromium launch owner with an executable that never becomes ready, so the
// owning deadline expires during the launch, and requires the error to name
// that phase while the phase error itself still unwraps to the deadline.
func TestRunChromiumNamesTheLaunchPhaseOfAnExpiredDeadline(t *testing.T) {
	manifest, data := chromiumHTMLCase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	result, err := RunChromium(ctx, manifest, data, process.LaunchConfig{BinaryPath: neverReadyBrowser(t)})
	var phase *fixture.PhaseError
	if !errors.As(err, &phase) || phase.Phase != fixture.PhaseLaunch || result.Passed {
		t.Fatalf("launch failure = %v (%+v)", err, result)
	}
	if !errors.Is(phase, context.DeadlineExceeded) || phase.Elapsed <= 0 || phase.Elapsed > 15*time.Second {
		t.Fatalf("the launch phase lost its deadline or elapsed time: %v", phase)
	}
	if !strings.HasPrefix(err.Error(), "browser launch failed after ") {
		t.Fatalf("message does not lead with the phase: %q", err)
	}
}

func chromiumHTMLCase(t *testing.T) (fixtureasset.Manifest, []byte) {
	t.Helper()
	manifests, data, err := DefaultAssets("chromium")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if strings.HasSuffix(manifest.ID, ":html-001") {
			return manifest, data
		}
	}
	t.Fatal("actual HTML corpus case missing")
	return fixtureasset.Manifest{}, nil
}

// neverReadyBrowser writes an executable that idles until it is terminated and
// never publishes a DevTools endpoint.
func neverReadyBrowser(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "never-ready-chromium")
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n")
	if err := errors.Join(writeErr, file.Chmod(0o700), file.Close()); err != nil {
		t.Fatal(err)
	}
	return path
}
