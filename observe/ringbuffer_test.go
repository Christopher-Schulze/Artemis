package observe

import "testing"

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

func makeURL(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}
