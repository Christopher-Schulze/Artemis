package process

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"
)

const (
	DefaultMaxCPUPercent       = 400.0
	DefaultMaxMemoryBytes      = int64(2 * 1024 * 1024 * 1024)
	DefaultMaxProfileDiskBytes = int64(1024 * 1024 * 1024)
	DefaultSessionTimeout      = 30 * time.Minute
	DefaultResourceInterval    = time.Second
)

// ResourceBudget contains hard limits for an owned Chromium process group.
// Zero values receive platform defaults; negative values are invalid.
type ResourceBudget struct {
	MaxCPUPercent       float64
	MaxMemoryBytes      int64
	MaxProfileDiskBytes int64
	SessionTimeout      time.Duration
	SampleInterval      time.Duration
}

// ResourceUsage is one process-group resource sample.
type ResourceUsage struct {
	CPUPercent       float64
	MemoryBytes      int64
	ProfileDiskBytes int64
}

// ResourceSink persists one redacted process-group resource sample.
type ResourceSink func(ResourceUsage) error

func (b *ResourceBudget) applyDefaults() {
	defaults := platformResourceDefaults()
	if b.MaxCPUPercent == 0 {
		b.MaxCPUPercent = defaults.MaxCPUPercent
	}
	if b.MaxMemoryBytes == 0 {
		b.MaxMemoryBytes = defaults.MaxMemoryBytes
	}
	if b.MaxProfileDiskBytes == 0 {
		b.MaxProfileDiskBytes = defaults.MaxProfileDiskBytes
	}
	if b.SessionTimeout == 0 {
		b.SessionTimeout = DefaultSessionTimeout
	}
	if b.SampleInterval == 0 {
		b.SampleInterval = DefaultResourceInterval
	}
}

func (b ResourceBudget) validate() error {
	if b.MaxCPUPercent < 0 || b.MaxMemoryBytes < 0 || b.MaxProfileDiskBytes < 0 || b.SessionTimeout < time.Millisecond || b.SampleInterval < time.Millisecond {
		return errors.New("resource limits cannot be negative and time limits must be at least 1ms")
	}
	return nil
}

func (b ResourceBudget) exceeded(usage ResourceUsage) error {
	switch {
	case b.MaxCPUPercent > 0 && usage.CPUPercent > b.MaxCPUPercent:
		return fmt.Errorf("cpu percent %.2f exceeds %.2f", usage.CPUPercent, b.MaxCPUPercent)
	case b.MaxMemoryBytes > 0 && usage.MemoryBytes > b.MaxMemoryBytes:
		return fmt.Errorf("memory bytes %d exceeds %d", usage.MemoryBytes, b.MaxMemoryBytes)
	case b.MaxProfileDiskBytes > 0 && usage.ProfileDiskBytes > b.MaxProfileDiskBytes:
		return fmt.Errorf("profile disk bytes %d exceeds %d", usage.ProfileDiskBytes, b.MaxProfileDiskBytes)
	default:
		return nil
	}
}

type resourceSampler func(int, string) (ResourceUsage, error)

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
