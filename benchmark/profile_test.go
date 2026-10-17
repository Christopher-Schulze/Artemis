package benchmark

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestProfilesFlushAndPreserveExistingFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := ProfileConfig{CPUProfilePath: filepath.Join(dir, "cpu.pprof"), MemProfilePath: filepath.Join(dir, "heap.pprof")}
	stop, err := StartProfile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cfg.CPUProfilePath, cfg.MemProfilePath} {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		compressed, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		payload, readErr := io.ReadAll(compressed)
		gzipErr, closeErr := compressed.Close(), file.Close()
		if readErr != nil || gzipErr != nil || closeErr != nil || len(payload) == 0 {
			t.Fatalf("invalid flushed profile %s: %v, %v, %v", path, readErr, gzipErr, closeErr)
		}
	}
	before, err := os.ReadFile(cfg.CPUProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartProfile(cfg); err == nil {
		t.Fatal("existing profile overwritten")
	}
	after, err := os.ReadFile(cfg.CPUProfilePath)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing profile changed")
	}
}
