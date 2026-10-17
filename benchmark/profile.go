package benchmark

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
)

// ProfileConfig controls pprof CPU and memory profiling.
type ProfileConfig struct {
	CPUProfilePath string
	MemProfilePath string
}

// StartProfile begins CPU profiling if configured. The returned stop
// function must be called to flush profiles.
func StartProfile(cfg ProfileConfig) (func() error, error) {
	if cfg.CPUProfilePath == "" && cfg.MemProfilePath == "" {
		return func() error { return nil }, nil
	}

	var cpuFile *os.File
	if cfg.CPUProfilePath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.CPUProfilePath), 0o750); cfg.CPUProfilePath != "" && filepath.Dir(cfg.CPUProfilePath) != "." && err != nil {
			return nil, fmt.Errorf("profile dir: %w", err)
		}
		f, err := os.OpenFile(cfg.CPUProfilePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("cpu profile: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			closeErr := f.Close()
			if closeErr != nil {
				return nil, errors.Join(fmt.Errorf("start cpu profile: %w", err), fmt.Errorf("close cpu profile: %w", closeErr))
			}
			return nil, fmt.Errorf("start cpu profile: %w", err)
		}
		cpuFile = f
	}

	return func() (stopErr error) {
		if cpuFile != nil {
			pprof.StopCPUProfile()
			if err := cpuFile.Close(); err != nil {
				stopErr = fmt.Errorf("close CPU profile: %w", err)
			}
			cpuFile = nil
		}
		if cfg.MemProfilePath != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.MemProfilePath), 0o750); cfg.MemProfilePath != "" && filepath.Dir(cfg.MemProfilePath) != "." && err != nil {
				return errors.Join(stopErr, fmt.Errorf("mem profile dir: %w", err))
			}
			f, err := os.OpenFile(cfg.MemProfilePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return errors.Join(stopErr, fmt.Errorf("mem profile: %w", err))
			}
			writeErr := pprof.WriteHeapProfile(f)
			closeErr := f.Close()
			if writeErr != nil {
				if closeErr != nil {
					return errors.Join(stopErr, fmt.Errorf("write heap profile: %w", writeErr), fmt.Errorf("close memory profile: %w", closeErr))
				}
				return errors.Join(stopErr, fmt.Errorf("write heap profile: %w", writeErr))
			}
			if closeErr != nil {
				return errors.Join(stopErr, fmt.Errorf("close memory profile: %w", closeErr))
			}
		}
		return stopErr
	}, nil
}
