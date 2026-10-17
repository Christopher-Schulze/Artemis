//go:build !windows

package benchmark

import (
	"errors"
	"math"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRSSUnits(t *testing.T) {
	cases := []struct {
		name      string
		platform  string
		input     int64
		want      int64
		wantError bool
	}{
		{"Darwin bytes", "darwin", 256 * 1024 * 1024, 256 * 1024 * 1024, false},
		{"Linux KiB", "linux", 256 * 1024, 256 * 1024 * 1024, false},
		{"large Darwin bytes", "darwin", math.MaxInt64, math.MaxInt64, false},
		{"Linux overflow", "linux", math.MaxInt64, 0, true},
		{"negative", "darwin", -1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rssBytes(tc.input, tc.platform)
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("RSS = %d, %v; want %d, error=%v", got, err, tc.want, tc.wantError)
			}
		})
	}
}

func TestCurrentRSSMatchesOperatingSystem(t *testing.T) {
	var raw unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &raw); err != nil {
		t.Fatal(err)
	}
	want, err := rssBytes(raw.Maxrss, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	got, err := currentRusage()
	if err != nil {
		t.Fatal(err)
	}
	if got.RSSBytes < want || got.RSSBytes > want+64*1024*1024 {
		t.Fatalf("peak RSS = %d, recent kernel result = %d", got.RSSBytes, want)
	}
}

func TestFailedMeasurementRetainsTimingAndError(t *testing.T) {
	want := errors.New("operation failed")
	metrics, err := measureFunc(func() error { time.Sleep(time.Millisecond); return want })
	if !errors.Is(err, want) || metrics.WallMs <= 0 || metrics.RSSBytes <= 0 {
		t.Fatalf("failed measurement lost evidence: %+v, %v", metrics, err)
	}
}
