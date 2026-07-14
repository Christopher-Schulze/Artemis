package benchmark

import (
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
		if err := os.MkdirAll(filepath.Dir(cfg.CPUProfilePath), 0o755); cfg.CPUProfilePath != "" && filepath.Dir(cfg.CPUProfilePath) != "." && err != nil {
			return nil, fmt.Errorf("profile dir: %w", err)
		}
		f, err := os.Create(cfg.CPUProfilePath)
		if err != nil {
			return nil, fmt.Errorf("cpu profile: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return nil, fmt.Errorf("start cpu profile: %w", err)
		}
	}

	return func() error {
		if cfg.CPUProfilePath != "" {
			pprof.StopCPUProfile()
		}
		if cfg.MemProfilePath != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.MemProfilePath), 0o755); cfg.MemProfilePath != "" && filepath.Dir(cfg.MemProfilePath) != "." && err != nil {
				return fmt.Errorf("mem profile dir: %w", err)
			}
			f, err := os.Create(cfg.MemProfilePath)
			if err != nil {
				return fmt.Errorf("mem profile: %w", err)
			}
			defer f.Close()
			if err := pprof.WriteHeapProfile(f); err != nil {
				return fmt.Errorf("write heap profile: %w", err)
			}
		}
		return nil
	}, nil
}
