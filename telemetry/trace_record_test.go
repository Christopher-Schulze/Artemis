package telemetry

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultTraceRecordConfig(t *testing.T) {
	cfg := DefaultTraceRecordConfig("/tmp/traces")
	if !cfg.Screenshots || !cfg.Snapshots || cfg.Sources {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
	if cfg.TraceDir != "/tmp/traces" {
		t.Fatalf("traceDir=%q", cfg.TraceDir)
	}
}

func TestTraceRecordStartStop(t *testing.T) {
	dir := t.TempDir()
	r := NewTraceRecorder(DefaultTraceRecordConfig(dir))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	if !r.IsActive() {
		t.Fatal("should be active")
	}
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if r.IsActive() {
		t.Fatal("should not be active after stop")
	}
	if path == "" {
		t.Fatal("path should not be empty")
	}
	if !strings.HasSuffix(path, ".zip") {
		t.Fatalf("path=%q", path)
	}
}

func TestTraceRecordStartAlreadyActive(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.Start(); err == nil {
		t.Fatal("expected error on double start")
	}
}

func TestTraceRecordStopWithoutStart(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if _, err := r.Stop(); err == nil {
		t.Fatal("expected error stopping idle recorder")
	}
}

func TestTraceRecordAddScreenshot(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.AddScreenshot([]byte("png data")); err != nil {
		t.Fatal(err)
	}
	if r.ScreenshotCount() != 1 {
		t.Fatalf("count=%d", r.ScreenshotCount())
	}
}

func TestTraceRecordAddScreenshotWhenStopped(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if err := r.AddScreenshot([]byte("data")); err == nil {
		t.Fatal("should error when not active")
	}
}

func TestTraceRecordAddSnapshot(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.AddSnapshot([]byte("<html>snapshot</html>")); err != nil {
		t.Fatal(err)
	}
	if r.SnapshotCount() != 1 {
		t.Fatalf("count=%d", r.SnapshotCount())
	}
}

func TestTraceRecordAddSource(t *testing.T) {
	cfg := DefaultTraceRecordConfig(t.TempDir())
	cfg.Sources = true
	r := NewTraceRecorder(cfg)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.AddSource([]byte("source code")); err != nil {
		t.Fatal(err)
	}
	if r.SourceCount() != 1 {
		t.Fatalf("count=%d", r.SourceCount())
	}
}

func TestTraceRecordAddSourceDisabled(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.AddSource([]byte("source")); err != nil {
		t.Fatal("should not error when sources disabled, just skip")
	}
	if r.SourceCount() != 0 {
		t.Fatalf("count=%d", r.SourceCount())
	}
}

func TestTraceRecordZipContents(t *testing.T) {
	dir := t.TempDir()
	r := NewTraceRecorder(DefaultTraceRecordConfig(dir))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	r.AddScreenshot([]byte("screenshot1"))
	r.AddScreenshot([]byte("screenshot2"))
	r.AddSnapshot([]byte("<html>snapshot1</html>"))
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}

	entries, err := ReadTraceZip(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 3 {
		t.Fatalf("entries=%d want at least 3", len(entries))
	}
	if _, ok := entries["screenshots/screenshot-0001.png"]; !ok {
		t.Fatal("missing screenshot-0001.png")
	}
	if _, ok := entries["screenshots/screenshot-0002.png"]; !ok {
		t.Fatal("missing screenshot-0002.png")
	}
	if _, ok := entries["snapshots/snapshot-0001.html"]; !ok {
		t.Fatal("missing snapshot-0001.html")
	}
	if _, ok := entries["trace.meta.json"]; !ok {
		t.Fatal("missing trace.meta.json")
	}
}

func TestTraceRecordZipAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	r := NewTraceRecorder(DefaultTraceRecordConfig(dir))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the .zip exists and is a valid zip.
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	zr.Close()

	// Verify no temp files remain.
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".part") {
			t.Fatalf("temp file remains: %s", f.Name())
		}
	}
}

func TestTraceRecordDuration(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if r.Duration() != 0 {
		t.Fatalf("idle duration=%v", r.Duration())
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	r.Stop()
	if r.Duration() <= 0 {
		t.Fatal("stopped duration should be positive")
	}
}

func TestTraceRecordTracePath(t *testing.T) {
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if r.TracePath() != "" {
		t.Fatal("path should be empty before stop")
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if r.TracePath() != path {
		t.Fatalf("path=%q tracePath=%q", path, r.TracePath())
	}
}

func TestTraceRecordDefaultTempDir(t *testing.T) {
	cfg := DefaultTraceRecordConfig("")
	r := NewTraceRecorder(cfg)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, os.TempDir()) {
		t.Fatalf("path=%q should be in temp dir", path)
	}
	os.Remove(path)
}

func TestReadTraceZipInvalidPath(t *testing.T) {
	if _, err := ReadTraceZip("/nonexistent/trace.zip"); err == nil {
		t.Fatal("expected error for nonexistent zip")
	}
}

func TestTraceRecordMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	r := NewTraceRecorder(DefaultTraceRecordConfig(dir))

	// First session.
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	r.AddScreenshot([]byte("first"))
	path1, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}

	// Second session.
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	r.AddScreenshot([]byte("second"))
	path2, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}

	if path1 == path2 {
		t.Fatal("paths should differ between sessions")
	}
}

func TestTraceRecordScreenshotsDisabled(t *testing.T) {
	cfg := DefaultTraceRecordConfig(t.TempDir())
	cfg.Screenshots = false
	r := NewTraceRecorder(cfg)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	r.AddScreenshot([]byte("data"))
	if r.ScreenshotCount() != 0 {
		t.Fatalf("count=%d should be 0 when disabled", r.ScreenshotCount())
	}
}

func TestTraceRecordSourcesInZip(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultTraceRecordConfig(dir)
	cfg.Sources = true
	r := NewTraceRecorder(cfg)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	r.AddSource([]byte("source code"))
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ReadTraceZip(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries["sources/source-0001.txt"]; !ok {
		t.Fatal("missing source-0001.txt")
	}
}

func TestTraceRecordZipFileExists(t *testing.T) {
	dir := t.TempDir()
	r := NewTraceRecorder(DefaultTraceRecordConfig(dir))
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	path, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("zip file should exist: %v", err)
	}
}

func TestBuildSiblingTempPath(t *testing.T) {
	target := filepath.Join("/tmp", "trace.zip")
	temp := buildSiblingTempPath(target)
	if filepath.Dir(temp) != "/tmp" {
		t.Fatalf("temp dir=%q want /tmp", filepath.Dir(temp))
	}
	if !strings.HasSuffix(temp, ".part") {
		t.Fatalf("temp=%q should end with .part", temp)
	}
}
