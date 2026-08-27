//go:build !windows

package benchmark

import (
	"time"

	"golang.org/x/sys/unix"
)

// rusage captures self process CPU and RSS usage.
type rusage struct {
	CPUMs    float64
	RSSBytes int64
}

// currentRusage returns the current process user+system CPU time in milliseconds
// and the peak RSS in bytes. On platforms where getrusage is not available
// it returns zeroes.
func currentRusage() (rusage, error) {
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
		return rusage{}, err
	}

	cpu := float64(ru.Utime.Sec)*1000.0 + float64(ru.Utime.Usec)/1000.0
	cpu += float64(ru.Stime.Sec)*1000.0 + float64(ru.Stime.Usec)/1000.0

	// Maxrss units differ by platform: Linux reports kilobytes, Darwin bytes.
	// Use a conservative heuristic: values larger than 1 TiB are treated as bytes,
	// otherwise kilobytes. This is sufficient for benchmark processes and
	// avoids adding a build tag per OS.
	rss := ru.Maxrss
	const tebibyte = 1024 * 1024 * 1024 * 1024
	if rss < tebibyte {
		rss *= 1024
	}

	return rusage{CPUMs: cpu, RSSBytes: rss}, nil
}

// rusageDiff returns the CPU time and RSS delta between a start and end rusage.
func rusageDiff(start, end rusage) rusage {
	cpu := end.CPUMs - start.CPUMs
	if cpu < 0 {
		cpu = 0
	}
	rss := end.RSSBytes
	if rss < 0 {
		rss = 0
	}
	return rusage{CPUMs: cpu, RSSBytes: rss}
}

// measureFunc records CPU/RSS before and after f and returns the measured work.
func measureFunc(f func() error) (MetricSet, error) {
	start, err := currentRusage()
	if err != nil {
		return MetricSet{}, err
	}
	t0 := time.Now()
	if measureErr := f(); measureErr != nil {
		return MetricSet{}, measureErr
	}
	wall := float64(time.Since(t0).Microseconds()) / 1000.0
	end, err := currentRusage()
	if err != nil {
		return MetricSet{}, err
	}
	diff := rusageDiff(start, end)
	return MetricSet{
		WallMs:     wall,
		CPUMs:      diff.CPUMs,
		RSSBytes:   diff.RSSBytes,
		Throughput: MetricSet{WallMs: wall}.ThroughputForPages(1),
	}, nil
}
