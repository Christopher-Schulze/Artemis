package telemetry

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
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
	// DiscardArchive joins capture normally but creates no filesystem output.
	DiscardArchive bool
}

// TargetIdentity identifies the concrete browser target that produced a
// trace. It is intentionally protocol-level data, not a caller label.
type TargetIdentity struct {
	TargetID         string `json:"target_id"`
	SessionID        string `json:"session_id"`
	BrowserContextID string `json:"browser_context_id,omitempty"`
}

// Validate rejects an identity that cannot be tied to one attached target.
func (i TargetIdentity) Validate() error {
	if i.TargetID == "" {
		return fmt.Errorf("trace target: target ID is required")
	}
	if i.SessionID == "" {
		return fmt.Errorf("trace target: session ID is required")
	}
	if len(i.TargetID) > traceTextLimit || len(i.SessionID) > traceTextLimit || len(i.BrowserContextID) > traceTextLimit {
		return fmt.Errorf("trace target identity exceeds byte limit")
	}
	return nil
}

// TraceSource is one optional browser resource captured with a trace.
type TraceSource struct {
	URL  string
	Data []byte
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
	mu            sync.Mutex
	config        TraceRecordConfig
	target        *TargetIdentity
	active        bool
	startedAt     time.Time
	stoppedAt     time.Time
	tracePath     string
	screenshots   [][]byte
	snapshots     [][]byte
	sources       []TraceSource
	debug         TraceDebugEvidence
	retainedBytes int64
	debugBytes    int64
	artifactCount int
}

// NewTraceRecorder builds a TraceRecorder with the supplied config.
func NewTraceRecorder(config TraceRecordConfig) *TraceRecorder {
	return &TraceRecorder{
		config: config,
	}
}

// NewTraceRecorderWithTarget creates a recorder whose archive is bound to one
// concrete browser target and CDP session.
func NewTraceRecorderWithTarget(config TraceRecordConfig, target TargetIdentity) (*TraceRecorder, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	recorder := NewTraceRecorder(config)
	recorder.target = &target
	return recorder, nil
}

// SetDebugEvidence attaches bounded target-bound debug records before stop.
func (r *TraceRecorder) SetDebugEvidence(evidence TraceDebugEvidence) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return fmt.Errorf("no active trace session")
	}
	if r.target != nil && evidence.Target != *r.target {
		return fmt.Errorf("trace debug evidence belongs to another target")
	}
	bytes, err := measureTraceDebug(evidence)
	if err != nil {
		return err
	}
	if bytes > TraceTotalBytesLimit-(r.retainedBytes-r.debugBytes) {
		return fmt.Errorf("trace debug evidence exceeds aggregate byte limit")
	}
	r.debug = cloneTraceDebugEvidence(evidence)
	r.retainedBytes += bytes - r.debugBytes
	r.debugBytes = bytes
	return nil
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
	r.debug = TraceDebugEvidence{}
	r.retainedBytes, r.debugBytes, r.artifactCount = 0, 0, 0
	return nil
}

// Stop ends the active trace recording session and writes an atomic .zip
// archive to the configured trace directory. The .zip contains
// screenshots/, snapshots/, and sources/ subdirectories (when enabled).
// Returns the path to the written .zip file. DiscardArchive instead releases
// retained capture data and returns an empty path without filesystem output.
func (r *TraceRecorder) Stop() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return "", fmt.Errorf("no active trace; start a trace before stopping")
	}
	r.active = false
	r.stoppedAt = time.Now()
	if r.config.DiscardArchive {
		r.screenshots, r.snapshots, r.sources = nil, nil, nil
		r.debug = TraceDebugEvidence{}
		r.retainedBytes, r.debugBytes, r.artifactCount = 0, 0, 0
		return "", nil
	}

	traceDir, err := ensureTraceRoot(r.config.TraceDir)
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("browser-trace-%d-%03d.zip", r.startedAt.UnixMilli(), r.startedAt.Nanosecond()%1000000)
	finalPath := filepath.Join(traceDir, filename)

	// Publish a completed sibling file exclusively without replacing an archive.
	tempPath := buildSiblingTempPath(finalPath)
	if err := r.writeZip(tempPath); err != nil {
		return "", fmt.Errorf("write trace zip: %w", err)
	}
	if err := os.Link(tempPath, finalPath); err != nil {
		cleanupErr := os.Remove(tempPath)
		return "", errors.Join(fmt.Errorf("publish trace zip: %w", err), cleanupErr)
	}

	r.tracePath = finalPath
	return finalPath, os.Remove(tempPath)
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
	if err := r.reserveArtifact(len(data)); err != nil {
		return err
	}
	r.screenshots = append(r.screenshots, bytes.Clone(data))
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
	if err := r.reserveArtifact(len(data)); err != nil {
		return err
	}
	r.snapshots = append(r.snapshots, bytes.Clone(data))
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
	if err := r.reserveArtifact(len(data)); err != nil {
		return err
	}
	r.sources = append(r.sources, TraceSource{Data: bytes.Clone(data)})
	return nil
}

// AddNamedSource appends a resource source while retaining its browser URL.
func (r *TraceRecorder) AddNamedSource(source TraceSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return fmt.Errorf("no active trace session")
	}
	if !r.config.Sources {
		return nil
	}
	if source.URL == "" || len(source.URL) > traceTextLimit {
		return fmt.Errorf("trace source URL is required")
	}
	if err := r.reserveArtifact(len(source.Data)); err != nil {
		return err
	}
	r.sources = append(r.sources, TraceSource{URL: source.URL, Data: bytes.Clone(source.Data)})
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
func (r *TraceRecorder) writeZip(path string) (writeErr error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		writeErr = errors.Join(writeErr, f.Sync(), f.Close())
		if writeErr != nil {
			writeErr = errors.Join(writeErr, os.Remove(path))
		}
	}()

	zw := zip.NewWriter(&traceArchiveWriter{writer: f, remaining: TraceTotalBytesLimit})
	defer func() {
		writeErr = errors.Join(writeErr, zw.Close())
	}()
	remaining := TraceTotalBytesLimit
	writeEntry := func(name string, data []byte) error {
		if len(data) > remaining {
			return fmt.Errorf("trace ZIP decoded aggregate exceeds byte limit")
		}
		if err := writeZipEntry(zw, name, data); err != nil {
			return err
		}
		remaining -= len(data)
		return nil
	}

	for i, data := range r.screenshots {
		entry := fmt.Sprintf("screenshots/screenshot-%04d.png", i+1)
		if entryErr := writeEntry(entry, data); entryErr != nil {
			return entryErr
		}
	}

	for i, data := range r.snapshots {
		entry := fmt.Sprintf("snapshots/snapshot-%04d.html", i+1)
		if entryErr := writeEntry(entry, data); entryErr != nil {
			return entryErr
		}
	}

	for i, source := range r.sources {
		entry := fmt.Sprintf("sources/source-%04d.txt", i+1)
		if source.URL != "" {
			entry = fmt.Sprintf("sources/source-%04d-%s.txt", i+1, safeTraceName(source.URL))
		}
		if entryErr := writeEntry(entry, source.Data); entryErr != nil {
			return entryErr
		}
	}

	meta := traceMetadata{
		StartedAt:   r.startedAt,
		StoppedAt:   r.stoppedAt,
		Screenshots: len(r.screenshots),
		Snapshots:   len(r.snapshots),
		Sources:     len(r.sources),
		Debug:       newTraceDebugSummary(r.debug),
	}
	if r.target != nil {
		meta.Target = r.target
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal trace metadata: %w", err)
	}
	if err := writeEntry("trace.meta.json", metaData); err != nil {
		return err
	}
	debugEntries := []struct {
		name string
		data any
	}{
		{name: "debug/console.json", data: r.debug.Console},
		{name: "debug/page-errors.json", data: r.debug.PageErrors},
		{name: "debug/network.json", data: r.debug.Network},
	}
	for _, entry := range debugEntries {
		data, err := json.Marshal(entry.data)
		if err != nil {
			return fmt.Errorf("marshal trace debug %s: %w", entry.name, err)
		}
		if err := writeEntry(entry.name, data); err != nil {
			return err
		}
	}
	return nil
}

// writeZipEntry writes a single entry to a zip.Writer.
func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	if len(data) > TraceEntryBytesLimit {
		return fmt.Errorf("trace ZIP entry %s exceeds byte limit", name)
	}
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

// buildSiblingTempPath builds a temp file path in the same directory as the
// target, allowing exclusive atomic publication on the same filesystem.
func buildSiblingTempPath(targetPath string) string {
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)
	return filepath.Join(dir, fmt.Sprintf(".artemis-trace-%d-%s.part", time.Now().UnixNano(), base))
}

func ensureTraceRoot(raw string) (string, error) {
	traceDir := raw
	if traceDir == "" {
		traceDir = os.TempDir()
	}
	absolute, err := filepath.Abs(traceDir)
	if err != nil {
		return "", fmt.Errorf("resolve trace dir: %w", err)
	}
	if mkdirErr := os.MkdirAll(absolute, 0o750); mkdirErr != nil {
		return "", fmt.Errorf("create trace dir: %w", mkdirErr)
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve trace root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat trace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("trace root is not a directory: %s", root)
	}
	return root, nil
}

// ReadTraceZip opens a trace .zip archive and returns the list of entry
// names and their contents.
func ReadTraceZip(path string) (entries map[string][]byte, returnErr error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat trace zip: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > TraceTotalBytesLimit {
		return nil, fmt.Errorf("trace ZIP must be a regular file within %d bytes", TraceTotalBytesLimit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open trace zip: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			entries = nil
			returnErr = errors.Join(returnErr, fmt.Errorf("close trace zip: %w", closeErr))
		}
	}()
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat opened trace zip: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > TraceTotalBytesLimit {
		return nil, fmt.Errorf("trace ZIP must be a regular file within %d bytes", TraceTotalBytesLimit)
	}
	r, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("decode trace zip: %w", err)
	}
	if len(r.File) > TraceArtifactLimit+traceFixedEntries {
		return nil, fmt.Errorf("trace ZIP entry count exceeds limit")
	}

	entries = make(map[string][]byte)
	total := 0
	for _, f := range r.File {
		limit := min(TraceEntryBytesLimit, TraceTotalBytesLimit-total)
		if f.UncompressedSize64 > uint64(limit) {
			return nil, fmt.Errorf("trace ZIP entry %s exceeds byte limit", f.Name)
		}
		if _, exists := entries[f.Name]; exists {
			return nil, fmt.Errorf("duplicate trace ZIP entry %s", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
		closeErr := rc.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, fmt.Errorf("read/close zip entry %s: %w", f.Name, err)
		}
		if len(data) > limit {
			return nil, fmt.Errorf("trace ZIP entry %s expands beyond byte limit", f.Name)
		}
		total += len(data)
		entries[f.Name] = data
	}
	return entries, nil
}
