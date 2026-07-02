// Package scraper HAR streaming writer (spec L4478: P0b.2 HAR Streaming).
//
// P0b.2 HAR Streaming: write HAR entries to an io.Writer (file) as responses
// arrive rather than buffering the entire archive in memory. Response bodies
// are only captured when smaller than MaxBodyBytes (default 100KB); larger
// bodies are skipped with their size still recorded. Memory stays constant
// (~1MB) because each entry is JSON-encoded incrementally via a streaming
// json.Encoder that writes directly to the underlying file.
//
// The HAR format follows the HTTP Archive 1.2 spec:
//
//	{ "log": { "version": "1.2", "creator": {...}, "entries": [ ... ] } }
//
// The writer opens the file on construction, emits the header
// (`{"log":{"version":"1.2","creator":{...},"entries":[`), appends each entry
// as a comma-prefixed JSON object on AddEntry, and finalizes with `]}}` on
// Close. No entry is held in memory after it is flushed.
package scraper

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultHARMaxBodyBytes is the default upper bound on response body capture.
// Bodies larger than this are recorded by size only (text omitted).
const DefaultHARMaxBodyBytes = 100 * 1024 // 100KB

// HARStreamConfig configures a HARStream. Zero-value fields are replaced with
// sensible defaults by NewHARStream.
type HARStreamConfig struct {
	// Path is the filesystem path of the HAR file to write.
	Path string
	// MaxBodyBytes is the maximum response body size to capture as text.
	// Bodies at or above this size have their text skipped (size recorded).
	MaxBodyBytes int
	// CreatorName is the HAR creator.name field (default "artemis").
	CreatorName string
	// CreatorVersion is the HAR creator.version field (default "1.0").
	CreatorVersion string
}

// DefaultHARStreamConfig returns a config with safe defaults. The caller must
// set Path before use.
func DefaultHARStreamConfig() HARStreamConfig {
	return HARStreamConfig{
		MaxBodyBytes:   DefaultHARMaxBodyBytes,
		CreatorName:    "artemis",
		CreatorVersion: "1.0",
	}
}

// HARHeader is a single HTTP request/response header (HAR 1.2).
type HARHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARQuery is a single URL query string parameter (HAR 1.2).
type HARQuery struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARTimings records per-phase request timing in milliseconds (HAR 1.2).
type HARTimings struct {
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
}

// HARContent is the response content payload (HAR 1.2). Text is only
// populated when the body is under MaxBodyBytes and the MIME type is a
// text-like type; binary payloads record Size only.
type HARContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

// HARRequest is the request side of a HAR entry (HAR 1.2).
type HARRequest struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []HARHeader `json:"headers"`
	QueryString []HARQuery  `json:"queryString"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

// HARResponse is the response side of a HAR entry (HAR 1.2).
type HARResponse struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []HARHeader `json:"headers"`
	Content     HARContent  `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

// HAREntry is a single HAR log entry (HAR 1.2).
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Timings         HARTimings  `json:"timings"`
}

// HARCreator is the creator block of the HAR log.
type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HARStream writes HAR entries incrementally to a file-backed io.Writer.
// Each AddEntry call flushes a single JSON object to disk; no entries are
// retained in memory, so memory use stays constant regardless of entry count.
type HARStream struct {
	config     HARStreamConfig
	writer     io.Writer
	closer     io.Closer
	encoder    *json.Encoder
	entryCount int
	started    bool
	closed     bool
	mu         sync.Mutex
}

// NewHARStream opens the file at config.Path, writes the HAR header
// (`{"log":{"version":"1.2","creator":{...},"entries":[`), and returns a
// ready-to-use stream. Zero-value config fields are replaced with defaults.
func NewHARStream(config HARStreamConfig) (*HARStream, error) {
	if config.Path == "" {
		return nil, fmt.Errorf("har stream: path required")
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = DefaultHARMaxBodyBytes
	}
	if config.CreatorName == "" {
		config.CreatorName = "artemis"
	}
	if config.CreatorVersion == "" {
		config.CreatorVersion = "1.0"
	}

	f, err := os.OpenFile(config.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("har stream: open %s: %w", config.Path, err)
	}

	s := &HARStream{
		config:  config,
		writer:  f,
		closer:  f,
		encoder: json.NewEncoder(f),
		started: true,
	}

	// Write the static header by hand so the entries array is left open for
	// incremental appends. The creator block is JSON-encoded to stay robust
	// against special characters in the creator name/version.
	creatorBytes, err := json.Marshal(HARCreator{
		Name:    config.CreatorName,
		Version: config.CreatorVersion,
	})
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("har stream: marshal creator: %w", err)
	}
	header := `{"log":{"version":"1.2","creator":` + string(creatorBytes) + `,"entries":[`
	if _, err := f.Write([]byte(header)); err != nil {
		f.Close()
		return nil, fmt.Errorf("har stream: write header: %w", err)
	}

	return s, nil
}

// AddEntry writes a single HAR entry to the stream as JSON and increments the
// entry count. The entry's response body is filtered against MaxBodyBytes and
// MIME type via ShouldCaptureBody; oversized or binary bodies have their text
// dropped while their size is preserved. Safe for concurrent use.
func (s *HARStream) AddEntry(entry HAREntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil {
		return fmt.Errorf("har stream: nil stream")
	}
	if s.closed {
		return fmt.Errorf("har stream: closed")
	}
	if !s.started {
		return fmt.Errorf("har stream: not started")
	}

	// Ensure StartedDateTime is a valid ISO 8601 timestamp.
	if entry.StartedDateTime == "" {
		entry.StartedDateTime = time.Now().UTC().Format(time.RFC3339)
	}

	// Apply body capture policy to the response content. A body is captured
	// only when it is under the configured byte limit AND ShouldCaptureBody
	// approves the MIME type. Otherwise the text payload is dropped while
	// size and mime type are preserved for the record.
	content := &entry.Response.Content
	if content.Size >= s.config.MaxBodyBytes || !ShouldCaptureBody(content.Size, content.MimeType) {
		content.Text = ""
		content.Encoding = ""
	}

	// Emit a leading comma before every entry after the first so the entries
	// array stays valid JSON as it grows.
	if s.entryCount > 0 {
		if _, err := s.writer.Write([]byte(",")); err != nil {
			return fmt.Errorf("har stream: write separator: %w", err)
		}
	}

	if err := s.encoder.Encode(entry); err != nil {
		return fmt.Errorf("har stream: encode entry: %w", err)
	}

	s.entryCount++
	return nil
}

// Close finalizes the HAR file by writing the closing `]}}` and closing the
// underlying file. After Close, further AddEntry calls return an error.
func (s *HARStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil {
		return fmt.Errorf("har stream: nil stream")
	}
	if s.closed {
		return nil
	}
	s.closed = true

	if _, err := s.writer.Write([]byte("]}}")); err != nil {
		s.closer.Close()
		return fmt.Errorf("har stream: write footer: %w", err)
	}
	return s.closer.Close()
}

// EntryCount returns the number of entries successfully written.
func (s *HARStream) EntryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entryCount
}

// ShouldCaptureBody reports whether a response body of the given byte size
// and MIME type should have its text captured into the HAR archive.
//
// Bodies at or above MaxBodyBytes are never captured (size is still recorded).
// Text-like MIME types (text/*, application/json, application/xml,
// application/javascript, application/xhtml, +xml/+json suffixes) are captured
// when under the limit; binary types (images, video, audio, fonts, octet-stream,
// zip, pdf, etc.) never capture body text regardless of size.
func ShouldCaptureBody(bodySize int, mimeType string) bool {
	if bodySize <= 0 {
		return false
	}
	if bodySize >= DefaultHARMaxBodyBytes {
		return false
	}
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	if mt == "" {
		// Unknown type: treat as text-capturable when small (best-effort).
		return true
	}
	// Strip parameters (e.g. "text/html; charset=utf-8").
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}

	// Explicitly binary types: never capture text.
	binaryPrefixes := []string{
		"image/", "video/", "audio/", "font/",
		"application/octet-stream",
		"application/zip", "application/gzip",
		"application/x-gzip", "application/x-tar",
		"application/x-bzip", "application/x-bzip2",
		"application/x-7z-compressed", "application/x-rar-compressed",
		"application/pdf", "application/x-shockwave-flash",
		"application/wasm",
	}
	for _, p := range binaryPrefixes {
		if strings.HasPrefix(mt, p) {
			return false
		}
	}

	// Text-like types: always capture when under the limit.
	textPrefixes := []string{
		"text/",
		"application/json",
		"application/xml",
		"application/javascript",
		"application/ecmascript",
		"application/xhtml",
		"application/x-www-form-urlencoded",
		"application/ld+json",
	}
	for _, p := range textPrefixes {
		if strings.HasPrefix(mt, p) {
			return true
		}
	}

	// Structured suffixes: +xml, +json, +javascript are text-like.
	if strings.HasSuffix(mt, "+xml") || strings.HasSuffix(mt, "+json") {
		return true
	}

	// Default: do not capture unknown application/* types (likely binary).
	if strings.HasPrefix(mt, "application/") {
		return false
	}
	// Unknown but non-binary top-level: capture when small.
	return true
}
