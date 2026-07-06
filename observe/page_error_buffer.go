package observe

import (
	"sync"
	"time"
)

// page_error_buffer.go (spec L4304: Evidence Capture - page errors stream).
//
// Captures CDP Runtime.exceptionThrown events as page error entries with
// message/name/stack/timestamp. Thread-safe with RWMutex. Supports clear
// mode per spec. Part of the 3-stream evidence capture (console + network
// + page errors) for OCSF audit trail (ss6.1) and frontend dashboard.
//
// Reference: research/agents/openclaw-main/extensions/browser/src/browser/pw-tools-core.activity.ts:1-68

// PageError is a single captured page error entry
// (spec L4304: message/name/stack/timestamp with clear mode).
type PageError struct {
	Message   string    `json:"message"`
	Name      string    `json:"name,omitempty"`
	Stack     string    `json:"stack,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// DefaultPageErrorBufferSize is the ring buffer capacity for page errors.
const DefaultPageErrorBufferSize = 500

// PageErrorBuffer is a fixed-capacity ring buffer for page errors
// (spec L4304: message/name/stack/timestamp, clear mode, RWMutex).
type PageErrorBuffer struct {
	mu   sync.RWMutex
	cap  int
	buf  []PageError
	head int
	full bool
}

// NewPageErrorBuffer creates a new page error ring buffer with the
// given capacity. If capacity <= 0, DefaultPageErrorBufferSize is used.
func NewPageErrorBuffer(capacity int) *PageErrorBuffer {
	if capacity <= 0 {
		capacity = DefaultPageErrorBufferSize
	}
	return &PageErrorBuffer{
		cap: capacity,
		buf: make([]PageError, capacity),
	}
}

// Push appends a page error entry to the ring buffer (spec L4304).
// When the buffer is full, the oldest entry is overwritten.
// If Timestamp is zero, it is set to time.Now().
func (b *PageErrorBuffer) Push(entry PageError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	b.buf[b.head] = entry
	b.head++
	if b.head >= b.cap {
		b.head = 0
		b.full = true
	}
}

// PushError is a convenience method that creates a PageError from
// message/name/stack and pushes it.
func (b *PageErrorBuffer) PushError(message, name, stack string) {
	b.Push(PageError{
		Message:   message,
		Name:      name,
		Stack:     stack,
		Timestamp: time.Now(),
	})
}

// Snapshot returns a copy of all page errors in chronological order
// (oldest first, newest last) (spec L4304).
func (b *PageErrorBuffer) Snapshot() []PageError {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var out []PageError
	if !b.full {
		out = make([]PageError, b.head)
		copy(out, b.buf[:b.head])
	} else {
		out = make([]PageError, b.cap)
		n := copy(out, b.buf[b.head:])
		copy(out[n:], b.buf[:b.head])
	}
	return out
}

// Len returns the current number of entries in the buffer.
func (b *PageErrorBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.full {
		return b.cap
	}
	return b.head
}

// Cap returns the capacity of the ring buffer.
func (b *PageErrorBuffer) Cap() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cap
}

// IsFull returns true if the ring buffer has reached capacity.
func (b *PageErrorBuffer) IsFull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.full
}

// Clear removes all entries from the ring buffer (spec L4304: clear mode).
func (b *PageErrorBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.full = false
	b.buf = make([]PageError, b.cap)
}

// FindByMessage returns errors whose message contains the given substring
// (spec L4304: substring filter for diagnostics).
func (b *PageErrorBuffer) FindByMessage(substr string) []PageError {
	all := b.Snapshot()
	var out []PageError
	for _, e := range all {
		if containsString(e.Message, substr) {
			out = append(out, e)
		}
	}
	return out
}

// FindByName returns errors matching the given error name exactly.
func (b *PageErrorBuffer) FindByName(name string) []PageError {
	all := b.Snapshot()
	var out []PageError
	for _, e := range all {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

// Latest returns the most recent page error, or nil if the buffer is empty.
func (b *PageErrorBuffer) Latest() *PageError {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.head == 0 && !b.full {
		return nil
	}
	idx := b.head - 1
	if idx < 0 {
		idx = b.cap - 1
	}
	entry := b.buf[idx]
	return &entry
}

// containsString is a simple substring check.
func containsString(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
