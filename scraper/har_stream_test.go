package scraper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func helperTempHARPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.har")
}

// harFile holds the parsed shape of the on-disk HAR file.
type harFile struct {
	Log struct {
		Version string     `json:"version"`
		Creator HARCreator `json:"creator"`
		Entries []HAREntry `json:"entries"`
	} `json:"log"`
}

// parseHARFile reads and JSON-parses a finalized HAR file from disk.
func parseHARFile(t *testing.T, path string) harFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read har file: %v", err)
	}
	var hf harFile
	if err := json.Unmarshal(data, &hf); err != nil {
		t.Fatalf("unmarshal har: %v\nraw: %s", err, string(data))
	}
	return hf
}

func TestNewHARStreamCreatesFileAndHeader(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	defer s.Close()

	// File must exist immediately after construction.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("file is empty; header not written")
	}

	// Inspect raw bytes before Close to verify header prefix.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(raw), `{"log":{"version":"1.2","creator":`) {
		t.Fatalf("missing HAR header prefix, got: %s", string(raw[:min(80, len(raw))]))
	}
	if !strings.HasSuffix(string(raw), `"entries":[`) {
		t.Fatalf("entries array not left open, got tail: %s", string(raw[max(0, len(raw)-40):]))
	}
}

func TestNewHARStreamDefaultConfig(t *testing.T) {
	path := helperTempHARPath(t)
	s, err := NewHARStream(HARStreamConfig{Path: path})
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	defer s.Close()
	if s.config.MaxBodyBytes != DefaultHARMaxBodyBytes {
		t.Fatalf("expected default MaxBodyBytes %d, got %d", DefaultHARMaxBodyBytes, s.config.MaxBodyBytes)
	}
	if s.config.CreatorName != "artemis" {
		t.Fatalf("expected creator name artemis, got %s", s.config.CreatorName)
	}
	if s.config.CreatorVersion != "1.0" {
		t.Fatalf("expected creator version 1.0, got %s", s.config.CreatorVersion)
	}
}

func TestNewHARStreamEmptyPathErrors(t *testing.T) {
	_, err := NewHARStream(HARStreamConfig{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestAddEntryWritesEntry(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	entry := HAREntry{
		StartedDateTime: "2024-01-01T00:00:00.000Z",
		Time:            42,
		Request: HARRequest{
			Method:      "GET",
			URL:         "https://example.com/",
			HTTPVersion: "HTTP/1.1",
			Headers:     []HARHeader{{Name: "Host", Value: "example.com"}},
			HeadersSize: 30,
			BodySize:    0,
		},
		Response: HARResponse{
			Status:      200,
			StatusText:  "OK",
			HTTPVersion: "HTTP/1.1",
			Content: HARContent{
				Size:     11,
				MimeType: "text/html",
				Text:     "<html>hi</html>",
			},
			HeadersSize: 40,
			BodySize:    11,
		},
		Timings: HARTimings{Send: 1, Wait: 20, Receive: 21},
	}
	if err := s.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	hf := parseHARFile(t, path)
	if len(hf.Log.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(hf.Log.Entries))
	}
	e := hf.Log.Entries[0]
	if e.Request.Method != "GET" {
		t.Fatalf("expected GET, got %s", e.Request.Method)
	}
	if e.Response.Status != 200 {
		t.Fatalf("expected 200, got %d", e.Response.Status)
	}
	if e.Response.Content.Text != "<html>hi</html>" {
		t.Fatalf("expected body text preserved, got %q", e.Response.Content.Text)
	}
	if e.Timings.Wait != 20 {
		t.Fatalf("expected wait 20, got %d", e.Timings.Wait)
	}
}

func TestAddEntryMultipleEntries(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	for i := 0; i < 5; i++ {
		entry := HAREntry{
			StartedDateTime: "2024-01-01T00:00:00.000Z",
			Time:            int64(i * 10),
			Request: HARRequest{
				Method: "GET",
				URL:    "https://example.com/" + string(rune('a'+i)),
			},
			Response: HARResponse{
				Status: 200,
				Content: HARContent{
					Size:     5,
					MimeType: "text/plain",
					Text:     "hello",
				},
			},
		}
		if err := s.AddEntry(entry); err != nil {
			t.Fatalf("AddEntry %d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	hf := parseHARFile(t, path)
	if len(hf.Log.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(hf.Log.Entries))
	}
	for i, e := range hf.Log.Entries {
		if e.Time != int64(i*10) {
			t.Fatalf("entry %d: expected time %d, got %d", i, i*10, e.Time)
		}
	}
}

func TestCloseWritesFooter(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	if err := s.AddEntry(HAREntry{Request: HARRequest{Method: "GET", URL: "u"}, Response: HARResponse{Status: 200}}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(string(data), "]}}") {
		t.Fatalf("expected file to end with ]}}, got tail: %s", string(data[max(0, len(data)-20):]))
	}
	// Must be valid JSON.
	var hf harFile
	if err := json.Unmarshal(data, &hf); err != nil {
		t.Fatalf("final file not valid JSON: %v", err)
	}
}

func TestEntryCount(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	defer s.Close()

	if s.EntryCount() != 0 {
		t.Fatalf("expected 0 entries, got %d", s.EntryCount())
	}
	for i := 0; i < 3; i++ {
		if err := s.AddEntry(HAREntry{Request: HARRequest{Method: "GET"}, Response: HARResponse{Status: 200}}); err != nil {
			t.Fatalf("AddEntry: %v", err)
		}
	}
	if s.EntryCount() != 3 {
		t.Fatalf("expected 3 entries, got %d", s.EntryCount())
	}
}

func TestShouldCaptureBodyUnderLimit(t *testing.T) {
	if !ShouldCaptureBody(1024, "text/html") {
		t.Fatal("expected text/html under limit to be captured")
	}
	if !ShouldCaptureBody(50, "application/json") {
		t.Fatal("expected json under limit to be captured")
	}
	if !ShouldCaptureBody(50, "application/xml") {
		t.Fatal("expected xml under limit to be captured")
	}
}

func TestShouldCaptureBodyOverLimit(t *testing.T) {
	if ShouldCaptureBody(DefaultHARMaxBodyBytes, "text/html") {
		t.Fatal("expected text/html at limit to NOT be captured")
	}
	if ShouldCaptureBody(DefaultHARMaxBodyBytes+1, "text/html") {
		t.Fatal("expected text/html over limit to NOT be captured")
	}
}

func TestShouldCaptureBodyBinaryTypes(t *testing.T) {
	binaryTypes := []string{
		"image/png",
		"image/jpeg",
		"video/mp4",
		"audio/mpeg",
		"font/woff2",
		"application/octet-stream",
		"application/zip",
		"application/pdf",
		"application/wasm",
	}
	for _, mt := range binaryTypes {
		if ShouldCaptureBody(100, mt) {
			t.Fatalf("expected binary type %s to NOT be captured", mt)
		}
	}
}

func TestShouldCaptureBodyTextTypes(t *testing.T) {
	textTypes := []string{
		"text/html",
		"text/plain",
		"text/css",
		"application/json",
		"application/xml",
		"application/javascript",
		"application/xhtml+xml",
		"application/ld+json",
		"application/vnd.api+json",
	}
	for _, mt := range textTypes {
		if !ShouldCaptureBody(100, mt) {
			t.Fatalf("expected text type %s to be captured", mt)
		}
	}
}

func TestShouldCaptureBodyWithCharset(t *testing.T) {
	if !ShouldCaptureBody(100, "text/html; charset=utf-8") {
		t.Fatal("expected text/html with charset to be captured")
	}
	if ShouldCaptureBody(100, "image/png; charset=binary") {
		t.Fatal("expected image/png with charset to NOT be captured")
	}
}

func TestShouldCaptureBodyZeroOrNegative(t *testing.T) {
	if ShouldCaptureBody(0, "text/html") {
		t.Fatal("expected zero-size body to NOT be captured")
	}
	if ShouldCaptureBody(-1, "text/html") {
		t.Fatal("expected negative-size body to NOT be captured")
	}
}

func TestBodyOverLimitSkippedWithSizeRecorded(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	bigBody := strings.Repeat("x", DefaultHARMaxBodyBytes+100)
	entry := HAREntry{
		Request: HARRequest{Method: "GET", URL: "https://example.com/big"},
		Response: HARResponse{
			Status: 200,
			Content: HARContent{
				Size:     len(bigBody),
				MimeType: "text/html",
				Text:     bigBody,
			},
			BodySize: len(bigBody),
		},
	}
	if err := s.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	hf := parseHARFile(t, path)
	if len(hf.Log.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(hf.Log.Entries))
	}
	c := hf.Log.Entries[0].Response.Content
	if c.Size != len(bigBody) {
		t.Fatalf("expected size %d recorded, got %d", len(bigBody), c.Size)
	}
	if c.Text != "" {
		t.Fatalf("expected body text skipped for oversized body, got %d chars", len(c.Text))
	}
}

func TestBinaryBodySkippedWithSizeRecorded(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	entry := HAREntry{
		Request: HARRequest{Method: "GET", URL: "https://example.com/img.png"},
		Response: HARResponse{
			Status: 200,
			Content: HARContent{
				Size:     500,
				MimeType: "image/png",
				Text:     "fake-binary-data",
			},
			BodySize: 500,
		},
	}
	if err := s.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	hf := parseHARFile(t, path)
	c := hf.Log.Entries[0].Response.Content
	if c.Size != 500 {
		t.Fatalf("expected size 500, got %d", c.Size)
	}
	if c.Text != "" {
		t.Fatalf("expected binary body text skipped, got %q", c.Text)
	}
}

func TestStreamingFileGrowsIncrementally(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	info0, _ := os.Stat(path)
	baseSize := info0.Size()

	entry := HAREntry{
		Request:  HARRequest{Method: "GET", URL: "https://example.com/1"},
		Response: HARResponse{Status: 200, Content: HARContent{Size: 5, MimeType: "text/plain", Text: "hello"}},
	}
	if err := s.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry 1: %v", err)
	}
	info1, _ := os.Stat(path)
	if info1.Size() <= baseSize {
		t.Fatalf("file did not grow after first entry: base=%d now=%d", baseSize, info1.Size())
	}
	sizeAfter1 := info1.Size()

	entry.Request.URL = "https://example.com/2"
	if err := s.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry 2: %v", err)
	}
	info2, _ := os.Stat(path)
	if info2.Size() <= sizeAfter1 {
		t.Fatalf("file did not grow after second entry: after1=%d now=%d", sizeAfter1, info2.Size())
	}

	s.Close()
}

func TestConcurrentAddEntry(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	const goroutines = 10
	const perG = 20
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				entry := HAREntry{
					Request:  HARRequest{Method: "GET", URL: "https://example.com"},
					Response: HARResponse{Status: 200, Content: HARContent{Size: 4, MimeType: "text/plain", Text: "data"}},
				}
				if err := s.AddEntry(entry); err != nil {
					t.Errorf("AddEntry: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if s.EntryCount() != goroutines*perG {
		t.Fatalf("expected %d entries, got %d", goroutines*perG, s.EntryCount())
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	hf := parseHARFile(t, path)
	if len(hf.Log.Entries) != goroutines*perG {
		t.Fatalf("expected %d parsed entries, got %d", goroutines*perG, len(hf.Log.Entries))
	}
}

func TestAddEntryOnClosedStreamErrors(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = s.AddEntry(HAREntry{Request: HARRequest{Method: "GET"}, Response: HARResponse{Status: 200}})
	if err == nil {
		t.Fatal("expected error adding to closed stream")
	}
}

func TestCloseIdempotent(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close should be idempotent, got: %v", err)
	}
}

func TestHARVersionAndCreator(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	cfg.CreatorName = "test-creator"
	cfg.CreatorVersion = "2.5"
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	hf := parseHARFile(t, path)
	if hf.Log.Version != "1.2" {
		t.Fatalf("expected version 1.2, got %s", hf.Log.Version)
	}
	if hf.Log.Creator.Name != "test-creator" {
		t.Fatalf("expected creator name test-creator, got %s", hf.Log.Creator.Name)
	}
	if hf.Log.Creator.Version != "2.5" {
		t.Fatalf("expected creator version 2.5, got %s", hf.Log.Creator.Version)
	}
}

func TestAddEntryFillsStartedDateTime(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}
	entry := HAREntry{
		Request:  HARRequest{Method: "GET", URL: "u"},
		Response: HARResponse{Status: 200},
	}
	if err := s.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	hf := parseHARFile(t, path)
	if hf.Log.Entries[0].StartedDateTime == "" {
		t.Fatal("expected StartedDateTime to be auto-filled")
	}
}

func TestCustomMaxBodyBytes(t *testing.T) {
	path := helperTempHARPath(t)
	cfg := DefaultHARStreamConfig()
	cfg.Path = path
	cfg.MaxBodyBytes = 50
	s, err := NewHARStream(cfg)
	if err != nil {
		t.Fatalf("NewHARStream: %v", err)
	}

	// 40 bytes: under custom limit, text type → captured.
	small := HAREntry{
		Request:  HARRequest{Method: "GET", URL: "u"},
		Response: HARResponse{Status: 200, Content: HARContent{Size: 40, MimeType: "text/plain", Text: strings.Repeat("a", 40)}},
	}
	if err := s.AddEntry(small); err != nil {
		t.Fatalf("AddEntry small: %v", err)
	}
	// 60 bytes: over custom limit → text skipped.
	big := HAREntry{
		Request:  HARRequest{Method: "GET", URL: "u"},
		Response: HARResponse{Status: 200, Content: HARContent{Size: 60, MimeType: "text/plain", Text: strings.Repeat("b", 60)}},
	}
	if err := s.AddEntry(big); err != nil {
		t.Fatalf("AddEntry big: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	hf := parseHARFile(t, path)
	if hf.Log.Entries[0].Response.Content.Text == "" {
		t.Fatal("expected small body text captured under custom limit")
	}
	if hf.Log.Entries[1].Response.Content.Text != "" {
		t.Fatal("expected big body text skipped over custom limit")
	}
	if hf.Log.Entries[1].Response.Content.Size != 60 {
		t.Fatalf("expected size 60 recorded, got %d", hf.Log.Entries[1].Response.Content.Size)
	}
}
