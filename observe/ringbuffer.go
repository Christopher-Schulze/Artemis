package observe

import (
	"sync"
)

// NetworkEvent is one captured network observation.
type NetworkEvent struct {
	URL      string
	Method   string
	Status   int
	Bytes    int
}

// NetworkRingBuffer stores the last N network events with optional mmap persistence hook.
type NetworkRingBuffer struct {
	mu   sync.Mutex
	cap  int
	buf  []NetworkEvent
	head int
	full bool
}

func NewNetworkRingBuffer(capacity int) *NetworkRingBuffer {
	if capacity <= 0 {
		capacity = 256
	}
	return &NetworkRingBuffer{cap: capacity, buf: make([]NetworkEvent, capacity)}
}

func (r *NetworkRingBuffer) Push(ev NetworkEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = ev
	r.head++
	if r.head >= r.cap {
		r.head = 0
		r.full = true
	}
}

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

func (r *NetworkRingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return r.cap
	}
	return r.head
}
