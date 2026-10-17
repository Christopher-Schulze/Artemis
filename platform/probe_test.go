package platform

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestPhysicalMemoryUnitsRejectZeroAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name            string
		ram, unit, want uint64
		fail            bool
	}{
		{"bytes", 8192, 1, 8192, false},
		{"pages", 2, 4096, 8192, false},
		{"zero ram", 0, 4096, 0, true},
		{"zero unit", 2, 0, 0, true},
		{"overflow", math.MaxUint64, 2, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := physicalMemoryBytes(tc.ram, tc.unit)
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("memory = %d, %v", got, err)
			}
		})
	}
}

func TestProbeFileBoundAndOwnership(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kernel-value")
	if err := os.WriteFile(path, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readProbeFile(t.Context(), path, 4)
	if err != nil || string(data) != "1234" {
		t.Fatalf("exact-bound input rejected: %q %v", data, err)
	}
	if _, err := readProbeFile(t.Context(), path, 3); err == nil {
		t.Fatal("oversized probe input admitted")
	}
	if _, err := readProbeFile(t.Context(), dir, 4); err == nil {
		t.Fatal("nonregular input admitted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProbeFile(t.Context(), link, 4); err == nil {
		t.Fatal("symlink probe input admitted")
	}
	if _, err := readProbeFile(t.Context(), filepath.Join(dir, "missing"), 4); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing observation error lost: %v", err)
	}
	for _, limit := range []int64{0, -1, math.MaxInt64, 65537} {
		if _, err := readProbeFile(t.Context(), path, limit); err == nil {
			t.Fatalf("invalid work bound admitted: %d", limit)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readProbeFile(ctx, path, 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observation admitted: %v", err)
	}
}

func TestContainerMarkersDoNotTreatOverlayAsProof(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"0::/user.slice", false}, {"overlay filesystem", false},
		{"0::/docker/abc", true}, {"0::/system.slice/docker-abc.scope", true},
		{"0::/kubepods/burstable", true}, {"0::/lxc/guest", true},
	} {
		if got := containerCgroup([]byte(tc.text)); got != tc.want {
			t.Fatalf("container evidence %q = %v", tc.text, got)
		}
	}
}

func TestFailedProbeHasNoOptimisticValue(t *testing.T) {
	result := failedProbe("denied test observer", os.ErrPermission)
	if result.State != StateUnknown || result.Value != 0 || result.Source != "denied test observer" || result.Reason != os.ErrPermission.Error() {
		t.Fatalf("failed observation became support: %+v", result)
	}
}
