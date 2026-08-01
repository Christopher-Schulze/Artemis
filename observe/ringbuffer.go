package observe

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Spec-mandated defaults (spec L4186: network ring buffer).
const (
	// DefaultNetworkRingCapacity is the default per-tab ring buffer size.
	DefaultNetworkRingCapacity = 100
	// MaxNetworkRingCapacity is the upper clamp for the ring buffer size.
	MaxNetworkRingCapacity = 10000

	// Sanitization limits (spec L4186).
	MaxURLLen         = 8 * 1024  // 8KB
	MaxPostDataLen    = 64 * 1024 // 64KB
	MaxHeaderValLen   = 4 * 1024  // 4KB per header value
	MaxHeaderTotalLen = 32 * 1024 // 32KB total headers
)

// NetworkEvent is one captured network observation.
// Normalized per spec L4186: URL, method, status, resourceType,
// mimeType, headers (truncated 4KB each), postData (max 64KB),
// timing (start/end/duration_ms).
// Headers carries the request headers; ResponseHeaders carries the
// response headers. They are separate fields because a single map
// forced the response side to overwrite the request side, which lost
// half the header evidence for every completed request (research floor
// research/webstack/pinchtab-main/internal/bridge/observe/network.go:43-44
// keeps the same split).
type NetworkEvent struct {
	RequestID       string            `json:"requestId"`
	URL             string            `json:"url"`
	Method          string            `json:"method"`
	Status          int               `json:"status"`
	ResourceType    string            `json:"resourceType,omitempty"`
	MimeType        string            `json:"mimeType,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	PostData        string            `json:"postData,omitempty"`
	Bytes           int               `json:"bytes"`
	Start           time.Time         `json:"start,omitempty"`
	End             time.Time         `json:"end,omitempty"`
	DurationMS      float64           `json:"durationMs,omitempty"`
}

// NetworkRingBuffer stores the last N network events with O(1)
// lookup by requestId (spec L4186: circular buffer default 100/tab,
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
// Default is 100, clamped at 10000 (spec L4186).
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
// lookup (spec L4186).
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
// (spec L4186: O(1) lookup by requestId).
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
// (spec L4186: URL 8KB, postData 64KB, header value 4KB, header total 32KB).
// Both header maps are budgeted independently, matching the research floor
// which normalizes request and response headers through the same limits.
func sanitizeEvent(ev NetworkEvent) NetworkEvent {
	ev.URL = truncateUTF8(ev.URL, MaxURLLen)
	ev.PostData = truncateUTF8(ev.PostData, MaxPostDataLen)
	ev.Headers = sanitizeHeaders(ev.Headers)
	ev.ResponseHeaders = sanitizeHeaders(ev.ResponseHeaders)
	return ev
}

// sanitizeHeaders truncates each header value to MaxHeaderValLen and drops
// headers once the set reaches MaxHeaderTotalLen. Keys are visited in sorted
// order so the surviving set is identical across runs: ranging over the map
// made the dropped headers depend on Go's randomized map iteration, which
// turned every oversized header set into nondeterministic evidence.
func sanitizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	total := 0
	sanitized := make(map[string]string, len(headers))
	for _, key := range keys {
		value := truncateUTF8(headers[key], MaxHeaderValLen)
		entryLen := len(key) + len(value) + 4 // key + ": " + value + "\r\n"
		if total+entryLen > MaxHeaderTotalLen {
			break
		}
		total += entryLen
		sanitized[key] = value
	}
	return sanitized
}

// truncateUTF8 truncates s to at most maxBytes bytes without splitting a
// rune, so truncated evidence stays valid UTF-8 and survives JSON encoding
// (research floor: sanitize.TruncateUTF8Bytes).
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// truncateString truncates a string to maxLen bytes on a rune boundary.
func truncateString(s string, maxLen int) string {
	return truncateUTF8(s, maxLen)
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
