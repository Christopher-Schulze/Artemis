//go:build !windows

package benchmark

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// rusage captures self process CPU and RSS usage.
type rusage struct {
	CPUMs    float64
	RSSBytes int64
}

// currentRusage returns the current process user+system CPU time in milliseconds
// and the lifetime peak RSS in bytes. Unavailable measurements return errors.
func currentRusage() (rusage, error) {
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
		return rusage{}, err
	}

	cpu := float64(ru.Utime.Sec)*1000.0 + float64(ru.Utime.Usec)/1000.0
	cpu += float64(ru.Stime.Sec)*1000.0 + float64(ru.Stime.Usec)/1000.0

	rss, err := rssBytes(ru.Maxrss, runtime.GOOS)
	if err != nil {
		return rusage{}, err
	}

	return rusage{CPUMs: cpu, RSSBytes: rss}, nil
}

func rssBytes(value int64, operatingSystem string) (int64, error) {
	if value < 0 {
		return 0, fmt.Errorf("negative peak RSS")
	}
	if operatingSystem == "darwin" {
		return value, nil
	}
	if value > math.MaxInt64/1024 {
		return 0, fmt.Errorf("peak RSS exceeds signed byte range")
	}
	return value * 1024, nil
}

// rusageDiff returns CPU consumed in the interval and lifetime peak process RSS.
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
	measureErr := f()
	wall := float64(time.Since(t0)) / float64(time.Millisecond)
	end, err := currentRusage()
	if err != nil {
		return MetricSet{WallMs: wall}, errors.Join(measureErr, err)
	}
	diff := rusageDiff(start, end)
	return MetricSet{
		WallMs:     wall,
		CPUMs:      diff.CPUMs,
		RSSBytes:   diff.RSSBytes,
		Throughput: MetricSet{WallMs: wall}.ThroughputForPages(1),
	}, measureErr
}
