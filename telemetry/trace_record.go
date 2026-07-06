package telemetry

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TraceRecordConfig configures a Playwright-style trace recording session
// (spec L4309). Screenshots and snapshots default to true; sources default
// to false per spec.
type TraceRecordConfig struct {
	Screenshots bool
	Snapshots   bool
	Sources     bool
	TraceDir    string
}

// DefaultTraceRecordConfig returns the spec-default trace config:
// screenshots=true, snapshots=true, sources=false.
func DefaultTraceRecordConfig(traceDir string) TraceRecordConfig {
	return TraceRecordConfig{
		Screenshots: true,
		Snapshots:   true,
		Sources:     false,
		TraceDir:    traceDir,
	}
}

// TraceRecorder manages Playwright-style trace recording sessions with
// atomic .zip output. It is thread-safe.
type TraceRecorder struct {
	mu          sync.Mutex
	config      TraceRecordConfig
	active      bool
	startedAt   time.Time
	stoppedAt   time.Time
	tracePath   string
	screenshots [][]byte
	snapshots   [][]byte
	sources     [][]byte
}

// NewTraceRecorder builds a TraceRecorder with the supplied config.
func NewTraceRecorder(config TraceRecordConfig) *TraceRecorder {
	return &TraceRecorder{
		config: config,
	}
}

// Start begins a trace recording session. Returns an error if a session is
// already active.
func (r *TraceRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return fmt.Errorf("trace already running; stop the current trace before starting a new one")
	}
	r.active = true
	r.startedAt = time.Now()
	r.stoppedAt = time.Time{}
	r.tracePath = ""
	r.screenshots = nil
	r.snapshots = nil
	r.sources = nil
	return nil
}

// Stop ends the active trace recording session and writes an atomic .zip
// archive to the configured trace directory. The .zip contains
// screenshots/, snapshots/, and sources/ subdirectories (when enabled).
// Returns the path to the written .zip file.
func (r *TraceRecorder) Stop() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return "", fmt.Errorf("no active trace; start a trace before stopping")
	}
	r.active = false
	r.stoppedAt = time.Now()

	traceDir := r.config.TraceDir
	if traceDir == "" {
		traceDir = os.TempDir()
	}
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		return "", fmt.Errorf("create trace dir: %w", err)
	}

	filename := fmt.Sprintf("browser-trace-%d-%03d.zip", r.startedAt.UnixMilli(), r.startedAt.Nanosecond()%1000000)
	finalPath := filepath.Join(traceDir, filename)

	// Atomic write: write to a sibling temp file then rename.
	tempPath := buildSiblingTempPath(finalPath)
	if err := r.writeZip(tempPath); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("write trace zip: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("rename trace zip: %w", err)
	}

	r.tracePath = finalPath
	return finalPath, nil
}

// AddScreenshot appends a screenshot capture to the active trace session.
func (r *TraceRecorder) AddScreenshot(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return fmt.Errorf("no active trace session")
	}
	if !r.config.Screenshots {
		return nil
	}
	r.screenshots = append(r.screenshots, data)
	return nil
}

// AddSnapshot appends a DOM snapshot to the active trace session.
func (r *TraceRecorder) AddSnapshot(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return fmt.Errorf("no active trace session")
	}
	if !r.config.Snapshots {
		return nil
	}
	r.snapshots = append(r.snapshots, data)
	return nil
}

// AddSource appends a source file to the active trace session.
func (r *TraceRecorder) AddSource(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return fmt.Errorf("no active trace session")
	}
	if !r.config.Sources {
		return nil
	}
	r.sources = append(r.sources, data)
	return nil
}

// IsActive reports whether a trace recording session is currently active.
func (r *TraceRecorder) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// TracePath returns the path of the last written .zip, or empty if none.
func (r *TraceRecorder) TracePath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tracePath
}

// ScreenshotCount returns the number of screenshots captured in the active
// or last session.
func (r *TraceRecorder) ScreenshotCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.screenshots)
}

// SnapshotCount returns the number of snapshots captured in the active or
// last session.
func (r *TraceRecorder) SnapshotCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.snapshots)
}

// SourceCount returns the number of sources captured in the active or last
// session.
func (r *TraceRecorder) SourceCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sources)
}

// Duration returns the elapsed time of the active or completed session.
func (r *TraceRecorder) Duration() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active && r.stoppedAt.IsZero() {
		return 0
	}
	if r.active {
		return time.Since(r.startedAt)
	}
	return r.stoppedAt.Sub(r.startedAt)
}

// writeZip writes the trace data as a .zip archive to the supplied path.
func (r *TraceRecorder) writeZip(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for i, data := range r.screenshots {
		entry := fmt.Sprintf("screenshots/screenshot-%04d.png", i+1)
		if err := writeZipEntry(zw, entry, data); err != nil {
			return err
		}
	}

	for i, data := range r.snapshots {
		entry := fmt.Sprintf("snapshots/snapshot-%04d.html", i+1)
		if err := writeZipEntry(zw, entry, data); err != nil {
			return err
		}
	}

	for i, data := range r.sources {
		entry := fmt.Sprintf("sources/source-%04d.txt", i+1)
		if err := writeZipEntry(zw, entry, data); err != nil {
			return err
		}
	}

	// Write a trace metadata file.
	meta := fmt.Sprintf(`{"startedAt":"%s","stoppedAt":"%s","screenshots":%d,"snapshots":%d,"sources":%d}`,
		r.startedAt.Format(time.RFC3339Nano),
		r.stoppedAt.Format(time.RFC3339Nano),
		len(r.screenshots), len(r.snapshots), len(r.sources))
	return writeZipEntry(zw, "trace.meta.json", []byte(meta))
}

// writeZipEntry writes a single entry to a zip.Writer.
func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// buildSiblingTempPath builds a temp file path in the same directory as the
// target, ensuring atomic rename on the same filesystem.
func buildSiblingTempPath(targetPath string) string {
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)
	return filepath.Join(dir, fmt.Sprintf(".artemis-trace-%d-%s.part", time.Now().UnixNano(), base))
}

// ReadTraceZip opens a trace .zip archive and returns the list of entry
// names and their contents.
func ReadTraceZip(path string) (map[string][]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open trace zip: %w", err)
	}
	defer r.Close()

	entries := make(map[string][]byte)
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", f.Name, err)
		}
		entries[f.Name] = data
	}
	return entries, nil
}
