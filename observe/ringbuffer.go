package observe

import (
	"strings"
	"sync"
	"time"
)

// Spec-mandated defaults (spec L4183: network ring buffer).
const (
	// DefaultNetworkRingCapacity is the default per-tab ring buffer size.
	DefaultNetworkRingCapacity = 100
	// MaxNetworkRingCapacity is the upper clamp for the ring buffer size.
	MaxNetworkRingCapacity = 10000

	// Sanitization limits (spec L4184).
	MaxURLLen         = 8 * 1024  // 8KB
	MaxPostDataLen    = 64 * 1024 // 64KB
	MaxHeaderValLen   = 4 * 1024  // 4KB per header value
	MaxHeaderTotalLen = 32 * 1024 // 32KB total headers
)

// NetworkEvent is one captured network observation.
// Normalized per spec L4183: URL, method, status, resourceType,
// mimeType, headers (truncated 4KB each), postData (max 64KB),
// timing (start/end/duration_ms).
type NetworkEvent struct {
	RequestID    string            `json:"requestId"`
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	Status       int               `json:"status"`
	ResourceType string            `json:"resourceType,omitempty"`
	MimeType     string            `json:"mimeType,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	PostData     string            `json:"postData,omitempty"`
	Bytes        int               `json:"bytes"`
	Start        time.Time         `json:"start,omitempty"`
	End          time.Time         `json:"end,omitempty"`
	DurationMS   float64           `json:"durationMs,omitempty"`
}

// NetworkRingBuffer stores the last N network events with O(1)
// lookup by requestId (spec L4183: circular buffer default 100/tab,
// clamped at 10000, O(1) lookup by requestId).
type NetworkRingBuffer struct {
	mu   sync.Mutex
	cap  int
	buf  []NetworkEvent
	head int
	full bool
	byID map[string]int // requestId -> buffer index for O(1) lookup
}

// NewNetworkRingBuffer creates a ring buffer with the given capacity.
// Default is 100, clamped at 10000 (spec L4183).
func NewNetworkRingBuffer(capacity int) *NetworkRingBuffer {
	if capacity <= 0 {
		capacity = DefaultNetworkRingCapacity
	}
	if capacity > MaxNetworkRingCapacity {
		capacity = MaxNetworkRingCapacity
	}
	return &NetworkRingBuffer{
		cap:  capacity,
		buf:  make([]NetworkEvent, capacity),
		byID: make(map[string]int, capacity),
	}
}

// Push adds a network event to the ring buffer, overwriting the
// oldest entry if full. Maintains the requestId index for O(1)
// lookup (spec L4183).
func (r *NetworkRingBuffer) Push(ev NetworkEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Sanitize the event before storing.
	ev = sanitizeEvent(ev)
	// If overwriting an existing entry, remove its requestId mapping.
	if r.full && r.buf[r.head].RequestID != "" {
		delete(r.byID, r.buf[r.head].RequestID)
	}
	r.buf[r.head] = ev
	if ev.RequestID != "" {
		r.byID[ev.RequestID] = r.head
	}
	r.head++
	if r.head >= r.cap {
		r.head = 0
		r.full = true
	}
}

// Snapshot returns a copy of all events in chronological order.
func (r *NetworkRingBuffer) Snapshot() []NetworkEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]NetworkEvent, r.head)
		copy(out, r.buf[:r.head])
		return out
	}
	out := make([]NetworkEvent, r.cap)
	copy(out, r.buf[r.head:])
	copy(out[r.cap-r.head:], r.buf[:r.head])
	return out
}

// Len returns the number of events currently in the buffer.
func (r *NetworkRingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return r.cap
	}
	return r.head
}

// Lookup retrieves an event by requestId in O(1)
// (spec L4183: O(1) lookup by requestId).
func (r *NetworkRingBuffer) Lookup(requestID string) (NetworkEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx, ok := r.byID[requestID]
	if !ok {
		return NetworkEvent{}, false
	}
	return r.buf[idx], true
}

// Capacity returns the configured capacity of the ring buffer.
func (r *NetworkRingBuffer) Capacity() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cap
}

// sanitizeEvent applies the spec-mandated sanitization limits
// (spec L4184: URL 8KB, postData 64KB, header value 4KB, header total 32KB).
func sanitizeEvent(ev NetworkEvent) NetworkEvent {
	if len(ev.URL) > MaxURLLen {
		ev.URL = ev.URL[:MaxURLLen]
	}
	if len(ev.PostData) > MaxPostDataLen {
		ev.PostData = ev.PostData[:MaxPostDataLen]
	}
	if len(ev.Headers) > 0 {
		totalHeaderLen := 0
		sanitized := make(map[string]string, len(ev.Headers))
		for k, v := range ev.Headers {
			if len(v) > MaxHeaderValLen {
				v = v[:MaxHeaderValLen]
			}
			entryLen := len(k) + len(v) + 4 // key + ": " + value + "\r\n"
			if totalHeaderLen+entryLen > MaxHeaderTotalLen {
				break // stop adding headers once total exceeds 32KB
			}
			totalHeaderLen += entryLen
			sanitized[k] = v
		}
		ev.Headers = sanitized
	}
	return ev
}

// truncateString truncates a string to maxLen bytes.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// normalizeHeaders lowercases header keys and truncates values.
func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	totalLen := 0
	for k, v := range headers {
		k = strings.ToLower(strings.TrimSpace(k))
		v = truncateString(v, MaxHeaderValLen)
		entryLen := len(k) + len(v) + 4
		if totalLen+entryLen > MaxHeaderTotalLen {
			break
		}
		totalLen += entryLen
		out[k] = v
	}
	return out
}
