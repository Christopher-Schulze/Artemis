package observe

import (
	"sync"
	"time"
)

// console_buffer.go (spec L4188: Console Log Capture).
//
// A ring buffer of 1000 console messages per tab with level
// (log/warn/error) + args + timestamp, thread-safe with RWMutex.
// Captures CDP Runtime.consoleAPICalled events for diagnostics and
// debugging.
//
// Reference: research/webstack/pinchtab-main/internal/bridge/observe/snapshot.go:1-352

// ConsoleLevel is the severity level of a console message
// (spec L4188: level log/warn/error).
type ConsoleLevel string

const (
	ConsoleLevelLog   ConsoleLevel = "log"
	ConsoleLevelWarn  ConsoleLevel = "warn"
	ConsoleLevelError ConsoleLevel = "error"
	ConsoleLevelInfo  ConsoleLevel = "info"
	ConsoleLevelDebug ConsoleLevel = "debug"
)

// IsValid checks if a console level is one of the recognized levels.
func (l ConsoleLevel) IsValid() bool {
	switch l {
	case ConsoleLevelLog, ConsoleLevelWarn, ConsoleLevelError,
		ConsoleLevelInfo, ConsoleLevelDebug:
		return true
	}
	return false
}

// NormalizeLevel converts a CDP Runtime.consoleAPICalled type string
// to a ConsoleLevel (spec L4188: level log/warn/error).
func NormalizeLevel(cdpType string) ConsoleLevel {
	switch cdpType {
	case "log":
		return ConsoleLevelLog
	case "warning":
		return ConsoleLevelWarn
	case "error":
		return ConsoleLevelError
	case "info":
		return ConsoleLevelInfo
	case "debug":
		return ConsoleLevelDebug
	default:
		return ConsoleLevelLog
	}
}

// ConsoleEntry is a single captured console message
// (spec L4188: level + args + timestamp).
type ConsoleEntry struct {
	Level     ConsoleLevel `json:"level"`
	Args      []string     `json:"args"`
	Timestamp time.Time    `json:"timestamp"`
	// StackTrace is an optional CDP stack trace string.
	StackTrace string `json:"stack_trace,omitempty"`
}

// DefaultConsoleBufferSize is the spec-mandated ring buffer capacity
// (spec L4188: 1000 messages per tab).
const DefaultConsoleBufferSize = 1000

// ConsoleRingBuffer is a fixed-capacity ring buffer for console log
// messages (spec L4188: 1000 cap, level/args/timestamp, RWMutex).
type ConsoleRingBuffer struct {
	mu   sync.RWMutex
	cap  int
	buf  []ConsoleEntry
	head int
	full bool
}

// NewConsoleRingBuffer creates a new console ring buffer with the
// given capacity. If capacity <= 0, DefaultConsoleBufferSize (1000)
// is used (spec L4188).
func NewConsoleRingBuffer(capacity int) *ConsoleRingBuffer {
	if capacity <= 0 {
		capacity = DefaultConsoleBufferSize
	}
	return &ConsoleRingBuffer{
		cap: capacity,
		buf: make([]ConsoleEntry, capacity),
	}
}

// Push appends a console entry to the ring buffer (spec L4188).
// When the buffer is full, the oldest entry is overwritten.
func (r *ConsoleRingBuffer) Push(entry ConsoleEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	r.buf[r.head] = entry
	r.head++
	if r.head >= r.cap {
		r.head = 0
		r.full = true
	}
}

// PushMessage is a convenience method that creates a ConsoleEntry
// from level + args and pushes it.
func (r *ConsoleRingBuffer) PushMessage(level ConsoleLevel, args []string) {
	r.Push(ConsoleEntry{
		Level:     level,
		Args:      args,
		Timestamp: time.Now(),
	})
}

// Snapshot returns a copy of all entries in chronological order
// (oldest first, newest last) (spec L4188). The Args slices are
// deep-copied so callers cannot mutate the buffer's internal state.
func (r *ConsoleRingBuffer) Snapshot() []ConsoleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []ConsoleEntry
	if !r.full {
		out = make([]ConsoleEntry, r.head)
		copy(out, r.buf[:r.head])
	} else {
		out = make([]ConsoleEntry, r.cap)
		n := copy(out, r.buf[r.head:])
		copy(out[n:], r.buf[:r.head])
	}

	// Deep-copy Args slices to prevent external mutation.
	for i := range out {
		if len(out[i].Args) > 0 {
			argsCopy := make([]string, len(out[i].Args))
			copy(argsCopy, out[i].Args)
			out[i].Args = argsCopy
		}
	}
	return out
}

// Len returns the current number of entries in the buffer.
func (r *ConsoleRingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.full {
		return r.cap
	}
	return r.head
}

// Cap returns the capacity of the ring buffer.
func (r *ConsoleRingBuffer) Cap() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cap
}

// IsFull returns true if the ring buffer has reached capacity.
func (r *ConsoleRingBuffer) IsFull() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.full
}

// Clear removes all entries from the ring buffer.
func (r *ConsoleRingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head = 0
	r.full = false
	r.buf = make([]ConsoleEntry, r.cap)
}

// FilterByLevel returns entries matching the given level
// (spec L4188: level filtering for diagnostics).
func (r *ConsoleRingBuffer) FilterByLevel(level ConsoleLevel) []ConsoleEntry {
	all := r.Snapshot()
	var out []ConsoleEntry
	for _, e := range all {
		if e.Level == level {
			out = append(out, e)
		}
	}
	return out
}

// CountByLevel returns a map of level -> count for all entries.
func (r *ConsoleRingBuffer) CountByLevel() map[ConsoleLevel]int {
	all := r.Snapshot()
	counts := make(map[ConsoleLevel]int)
	for _, e := range all {
		counts[e.Level]++
	}
	return counts
}

// Latest returns the most recent entry, or nil if the buffer is empty.
func (r *ConsoleRingBuffer) Latest() *ConsoleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.head == 0 && !r.full {
		return nil
	}
	idx := r.head - 1
	if idx < 0 {
		idx = r.cap - 1
	}
	entry := r.buf[idx]
	return &entry
}
