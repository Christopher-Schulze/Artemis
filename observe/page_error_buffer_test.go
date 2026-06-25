package observe

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPageErrorBuffer_PushAndSnapshot(t *testing.T) {
	buf := NewPageErrorBuffer(3)
	buf.PushError("TypeError: x is undefined", "TypeError", "at line 5")
	buf.PushError("ReferenceError: foo not defined", "ReferenceError", "at line 10")
	buf.PushError("SyntaxError: unexpected token", "SyntaxError", "at line 1")

	entries := buf.Snapshot()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Message != "TypeError: x is undefined" {
		t.Errorf("entry 0 message mismatch: %q", entries[0].Message)
	}
	if entries[0].Name != "TypeError" {
		t.Errorf("entry 0 name mismatch: %q", entries[0].Name)
	}
	if entries[0].Stack != "at line 5" {
		t.Errorf("entry 0 stack mismatch: %q", entries[0].Stack)
	}
	if entries[2].Name != "SyntaxError" {
		t.Errorf("entry 2 name mismatch: %q", entries[2].Name)
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("entry 0 timestamp should not be zero")
	}
}

func TestPageErrorBuffer_RingOverflow(t *testing.T) {
	buf := NewPageErrorBuffer(2)
	buf.PushError("error1", "E1", "s1")
	buf.PushError("error2", "E2", "s2")
	buf.PushError("error3", "E3", "s3") // overwrites error1

	if !buf.IsFull() {
		t.Error("buffer should be full")
	}
	entries := buf.Snapshot()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Oldest (error1) should be gone; error2 and error3 remain
	if entries[0].Message != "error2" {
		t.Errorf("expected error2 first, got %q", entries[0].Message)
	}
	if entries[1].Message != "error3" {
		t.Errorf("expected error3 second, got %q", entries[1].Message)
	}
}

func TestPageErrorBuffer_DefaultCapacity(t *testing.T) {
	buf := NewPageErrorBuffer(0)
	if buf.Cap() != DefaultPageErrorBufferSize {
		t.Errorf("expected default cap %d, got %d", DefaultPageErrorBufferSize, buf.Cap())
	}
}

func TestPageErrorBuffer_Clear(t *testing.T) {
	buf := NewPageErrorBuffer(10)
	buf.PushError("err1", "E1", "s1")
	buf.PushError("err2", "E2", "s2")
	if buf.Len() != 2 {
		t.Fatalf("expected len 2, got %d", buf.Len())
	}
	buf.Clear()
	if buf.Len() != 0 {
		t.Errorf("expected len 0 after clear, got %d", buf.Len())
	}
	if buf.IsFull() {
		t.Error("buffer should not be full after clear")
	}
	entries := buf.Snapshot()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(entries))
	}
}

func TestPageErrorBuffer_FindByMessage(t *testing.T) {
	buf := NewPageErrorBuffer(10)
	buf.PushError("TypeError: cannot read property", "TypeError", "s1")
	buf.PushError("NetworkError: fetch failed", "NetworkError", "s2")
	buf.PushError("TypeError: undefined variable", "TypeError", "s3")

	matches := buf.FindByMessage("TypeError")
	if len(matches) != 2 {
		t.Fatalf("expected 2 TypeError matches, got %d", len(matches))
	}
	for _, m := range matches {
		if !strings.Contains(m.Message, "TypeError") {
			t.Errorf("match message does not contain TypeError: %q", m.Message)
		}
	}
}

func TestPageErrorBuffer_FindByName(t *testing.T) {
	buf := NewPageErrorBuffer(10)
	buf.PushError("err1", "TypeError", "s1")
	buf.PushError("err2", "RangeError", "s2")
	buf.PushError("err3", "TypeError", "s3")

	matches := buf.FindByName("TypeError")
	if len(matches) != 2 {
		t.Fatalf("expected 2 TypeError by name, got %d", len(matches))
	}
}

func TestPageErrorBuffer_Latest(t *testing.T) {
	buf := NewPageErrorBuffer(10)
	if latest := buf.Latest(); latest != nil {
		t.Error("expected nil Latest on empty buffer")
	}
	buf.PushError("err1", "E1", "s1")
	buf.PushError("err2", "E2", "s2")
	latest := buf.Latest()
	if latest == nil {
		t.Fatal("expected non-nil Latest")
	}
	if latest.Message != "err2" {
		t.Errorf("expected latest message err2, got %q", latest.Message)
	}
}

func TestPageErrorBuffer_LatestOnFullBuffer(t *testing.T) {
	buf := NewPageErrorBuffer(2)
	buf.PushError("err1", "E1", "s1")
	buf.PushError("err2", "E2", "s2")
	buf.PushError("err3", "E3", "s3") // wraps around
	latest := buf.Latest()
	if latest == nil {
		t.Fatal("expected non-nil Latest")
	}
	if latest.Message != "err3" {
		t.Errorf("expected latest err3, got %q", latest.Message)
	}
}

func TestPageErrorBuffer_PreservesTimestamp(t *testing.T) {
	buf := NewPageErrorBuffer(5)
	custom := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	buf.Push(PageError{
		Message:   "custom",
		Name:      "E",
		Stack:     "s",
		Timestamp: custom,
	})
	entries := buf.Snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].Timestamp.Equal(custom) {
		t.Errorf("expected custom timestamp %v, got %v", custom, entries[0].Timestamp)
	}
}

func TestPageErrorBuffer_Concurrent(t *testing.T) {
	buf := NewPageErrorBuffer(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.PushError("concurrent-error", "E", "stack")
		}(i)
	}
	wg.Wait()
	if buf.Len() != 50 {
		t.Errorf("expected 50 entries, got %d", buf.Len())
	}
}
