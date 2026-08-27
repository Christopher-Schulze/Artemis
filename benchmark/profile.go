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

	if cfg.CPUProfilePath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.CPUProfilePath), 0o750); cfg.CPUProfilePath != "" && filepath.Dir(cfg.CPUProfilePath) != "." && err != nil {
			return nil, fmt.Errorf("profile dir: %w", err)
		}
		f, err := os.Create(cfg.CPUProfilePath)
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
	}

	return func() error {
		if cfg.CPUProfilePath != "" {
			pprof.StopCPUProfile()
		}
		if cfg.MemProfilePath != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.MemProfilePath), 0o750); cfg.MemProfilePath != "" && filepath.Dir(cfg.MemProfilePath) != "." && err != nil {
				return fmt.Errorf("mem profile dir: %w", err)
			}
			f, err := os.Create(cfg.MemProfilePath)
			if err != nil {
				return fmt.Errorf("mem profile: %w", err)
			}
			writeErr := pprof.WriteHeapProfile(f)
			closeErr := f.Close()
			if writeErr != nil {
				if closeErr != nil {
					return errors.Join(fmt.Errorf("write heap profile: %w", writeErr), fmt.Errorf("close memory profile: %w", closeErr))
				}
				return fmt.Errorf("write heap profile: %w", writeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close memory profile: %w", closeErr)
			}
		}
		return nil
	}, nil
}
