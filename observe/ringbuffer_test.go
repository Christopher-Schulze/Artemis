package observe

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNetworkRingBufferDefaultCapacity(t *testing.T) {
	rb := NewNetworkRingBuffer(0)
	if rb.Capacity() != DefaultNetworkRingCapacity {
		t.Errorf("default capacity = %d, want %d", rb.Capacity(), DefaultNetworkRingCapacity)
	}
	if DefaultNetworkRingCapacity != 100 {
		t.Errorf("DefaultNetworkRingCapacity = %d, want 100", DefaultNetworkRingCapacity)
	}
}

func TestNetworkRingBufferClampAtMax(t *testing.T) {
	rb := NewNetworkRingBuffer(99999)
	if rb.Capacity() != MaxNetworkRingCapacity {
		t.Errorf("clamped capacity = %d, want %d", rb.Capacity(), MaxNetworkRingCapacity)
	}
	if MaxNetworkRingCapacity != 10000 {
		t.Errorf("MaxNetworkRingCapacity = %d, want 10000", MaxNetworkRingCapacity)
	}
}

func TestNetworkRingBufferPushAndSnapshot(t *testing.T) {
	rb := NewNetworkRingBuffer(5)
	for i := 0; i < 3; i++ {
		rb.Push(NetworkEvent{
			RequestID: "req-1",
			URL:       "https://example.com",
			Method:    "GET",
			Status:    200,
		})
	}
	if rb.Len() != 3 {
		t.Errorf("Len = %d, want 3", rb.Len())
	}
	snap := rb.Snapshot()
	if len(snap) != 3 {
		t.Errorf("snapshot len = %d, want 3", len(snap))
	}
}

func TestNetworkRingBufferLookup(t *testing.T) {
	rb := NewNetworkRingBuffer(10)
	rb.Push(NetworkEvent{RequestID: "req-1", URL: "https://a.com"})
	rb.Push(NetworkEvent{RequestID: "req-2", URL: "https://b.com"})

	ev, ok := rb.Lookup("req-1")
	if !ok {
		t.Fatal("Lookup(req-1) not found")
	}
	if ev.URL != "https://a.com" {
		t.Errorf("Lookup URL = %s, want https://a.com", ev.URL)
	}

	_, ok = rb.Lookup("nonexistent")
	if ok {
		t.Error("Lookup(nonexistent) should return false")
	}
}

func TestNetworkRingBufferOverwrite(t *testing.T) {
	rb := NewNetworkRingBuffer(2)
	rb.Push(NetworkEvent{RequestID: "req-1", URL: "https://a.com"})
	rb.Push(NetworkEvent{RequestID: "req-2", URL: "https://b.com"})
	rb.Push(NetworkEvent{RequestID: "req-3", URL: "https://c.com"})

	// req-1 should be overwritten
	_, ok := rb.Lookup("req-1")
	if ok {
		t.Error("req-1 should have been overwritten")
	}
	// req-3 should be present
	ev, ok := rb.Lookup("req-3")
	if !ok {
		t.Fatal("req-3 not found")
	}
	if ev.URL != "https://c.com" {
		t.Errorf("req-3 URL = %s, want https://c.com", ev.URL)
	}
}

func TestSanitizeEventURLLimit(t *testing.T) {
	longURL := makeURL(10000) // 10KB, exceeds 8KB limit
	ev := NetworkEvent{URL: longURL}
	sanitized := sanitizeEvent(ev)
	if len(sanitized.URL) > MaxURLLen {
		t.Errorf("URL len = %d, max %d", len(sanitized.URL), MaxURLLen)
	}
}

func TestSanitizeEventPostDataLimit(t *testing.T) {
	longData := makeURL(100000) // 100KB, exceeds 64KB limit
	ev := NetworkEvent{PostData: longData}
	sanitized := sanitizeEvent(ev)
	if len(sanitized.PostData) > MaxPostDataLen {
		t.Errorf("PostData len = %d, max %d", len(sanitized.PostData), MaxPostDataLen)
	}
}

func TestSanitizeEventHeaderLimit(t *testing.T) {
	headers := make(map[string]string)
	// Add headers that exceed 32KB total
	for i := 0; i < 100; i++ {
		headers["X-Custom-"+string(rune('A'+i))] = makeURL(500) // 500 bytes each
	}
	ev := NetworkEvent{Headers: headers}
	sanitized := sanitizeEvent(ev)
	totalLen := 0
	for k, v := range sanitized.Headers {
		totalLen += len(k) + len(v) + 4
	}
	if totalLen > MaxHeaderTotalLen {
		t.Errorf("total header len = %d, max %d", totalLen, MaxHeaderTotalLen)
	}
}

func TestSanitizeEventHeaderValueLimit(t *testing.T) {
	headers := map[string]string{
		"X-Big": makeURL(10000), // 10KB, exceeds 4KB limit
	}
	ev := NetworkEvent{Headers: headers}
	sanitized := sanitizeEvent(ev)
	if len(sanitized.Headers["X-Big"]) > MaxHeaderValLen {
		t.Errorf("header value len = %d, max %d", len(sanitized.Headers["X-Big"]), MaxHeaderValLen)
	}
}

// TestSanitizeHeadersIsDeterministic is the mutation proof for the header
// budget: ranging over the map made the surviving header set depend on Go's
// randomized map iteration, so the same capture produced different evidence
// on every run.
func TestSanitizeHeadersIsDeterministic(t *testing.T) {
	headers := make(map[string]string, 200)
	for i := 0; i < 200; i++ {
		headers[fmt.Sprintf("X-Custom-%03d", i)] = makeURL(500)
	}

	first := sanitizeHeaders(headers)
	if len(first) == 0 || len(first) >= len(headers) {
		t.Fatalf("kept %d of %d headers, want a partial set inside the budget", len(first), len(headers))
	}
	firstKeys := sortedKeys(first)
	for run := 0; run < 64; run++ {
		again := sanitizeHeaders(headers)
		againKeys := sortedKeys(again)
		if len(againKeys) != len(firstKeys) {
			t.Fatalf("run %d kept %d headers, want %d", run, len(againKeys), len(firstKeys))
		}
		for i := range firstKeys {
			if againKeys[i] != firstKeys[i] {
				t.Fatalf("run %d header %d = %q, want %q", run, i, againKeys[i], firstKeys[i])
			}
		}
	}
	// Sorted-key order means the budget always keeps the lowest-named headers.
	if firstKeys[0] != "X-Custom-000" {
		t.Fatalf("first kept header = %q, want X-Custom-000", firstKeys[0])
	}
}

// TestSanitizeEventKeepsValidUTF8 proves truncation never splits a rune, so
// truncated evidence still survives JSON encoding.
func TestSanitizeEventKeepsValidUTF8(t *testing.T) {
	// Each rune is 3 bytes, so every limit lands mid-rune without a
	// boundary-aware cut.
	multibyte := strings.Repeat("あ", MaxPostDataLen)
	event := sanitizeEvent(NetworkEvent{
		URL:             multibyte,
		PostData:        multibyte,
		Headers:         map[string]string{"X-Note": multibyte},
		ResponseHeaders: map[string]string{"X-Echo": multibyte},
	})

	for name, value := range map[string]string{
		"url":             event.URL,
		"postData":        event.PostData,
		"requestHeader":   event.Headers["X-Note"],
		"responseHeader":  event.ResponseHeaders["X-Echo"],
		"truncateString":  truncateString(multibyte, MaxHeaderValLen),
		"truncateUTF8Raw": truncateUTF8(multibyte, 4),
	} {
		if !utf8.ValidString(value) {
			t.Fatalf("%s is not valid UTF-8 after truncation", name)
		}
	}
	if len(event.URL) > MaxURLLen || len(event.PostData) > MaxPostDataLen {
		t.Fatalf("limits exceeded: url=%d postData=%d", len(event.URL), len(event.PostData))
	}
	if len(event.Headers["X-Note"]) > MaxHeaderValLen {
		t.Fatalf("header value len = %d, max %d", len(event.Headers["X-Note"]), MaxHeaderValLen)
	}
}

// TestSanitizeEventBudgetsResponseHeaders proves the response side is bounded
// by the same limits as the request side.
func TestSanitizeEventBudgetsResponseHeaders(t *testing.T) {
	headers := make(map[string]string, 100)
	for i := 0; i < 100; i++ {
		headers[fmt.Sprintf("X-Resp-%03d", i)] = makeURL(500)
	}

	event := sanitizeEvent(NetworkEvent{ResponseHeaders: headers})

	total := 0
	for key, value := range event.ResponseHeaders {
		if len(value) > MaxHeaderValLen {
			t.Fatalf("response header %q len = %d, max %d", key, len(value), MaxHeaderValLen)
		}
		total += len(key) + len(value) + 4
	}
	if total > MaxHeaderTotalLen {
		t.Fatalf("total response header len = %d, max %d", total, MaxHeaderTotalLen)
	}
	if len(event.ResponseHeaders) == 0 {
		t.Fatal("response headers dropped entirely")
	}
}

// TestTruncateUTF8Bounds pins the boundary behavior of the shared truncator.
func TestTruncateUTF8Bounds(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{"empty budget", "hello", 0, ""},
		{"negative budget", "hello", -1, ""},
		{"fits exactly", "hello", 5, "hello"},
		{"ascii cut", "hello world", 5, "hello"},
		{"rune boundary cut", "あああ", 4, "あ"},
		{"rune fits exactly", "あああ", 6, "ああ"},
		{"shorter than budget", "あ", 64, "あ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateUTF8(tc.input, tc.maxBytes); got != tc.want {
				t.Fatalf("truncateUTF8(%q, %d) = %q, want %q", tc.input, tc.maxBytes, got, tc.want)
			}
		})
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func makeURL(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}
