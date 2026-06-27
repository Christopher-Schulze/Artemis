package observe

import (
	"fmt"
	"sync"
)

// network.go (spec L4024: observe/network.go - network ring buffer +
// subscriber pattern).
//
// This file is the spec-mandated facade for the network ring buffer
// and subscriber pattern. The ring buffer implementation lives in
// ringbuffer.go; this file adds the subscriber pattern on top.
//
// Page observation: network ring buffer + subscriber pattern.

// NetworkSubscriber is a callback that receives network events
// (spec L4024: network.go - subscriber pattern).
type NetworkSubscriber func(ev NetworkEvent)

// NetworkMonitor combines a ring buffer with a subscriber pattern
// (spec L4024: network.go - network ring buffer + subscriber pattern).
type NetworkMonitor struct {
	mu          sync.RWMutex
	buffer      *NetworkRingBuffer
	subscribers []NetworkSubscriber
}

// NewNetworkMonitor creates a new NetworkMonitor with the given
// ring buffer capacity (spec L4024: network.go).
func NewNetworkMonitor(capacity int) *NetworkMonitor {
	return &NetworkMonitor{
		buffer: NewNetworkRingBuffer(capacity),
	}
}

// Push adds a network event to the ring buffer and notifies all
// subscribers (spec L4024: network.go - network ring buffer +
// subscriber pattern).
func (m *NetworkMonitor) Push(ev NetworkEvent) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.buffer.Push(ev)
	subs := make([]NetworkSubscriber, len(m.subscribers))
	copy(subs, m.subscribers)
	m.mu.Unlock()
	// Notify subscribers outside the lock.
	for _, sub := range subs {
		sub(ev)
	}
}

// Subscribe adds a subscriber that receives all future network events
// (spec L4024: network.go - subscriber pattern).
func (m *NetworkMonitor) Subscribe(sub NetworkSubscriber) {
	if m == nil || sub == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = append(m.subscribers, sub)
}

// Snapshot returns a copy of the current ring buffer contents
// (spec L4024: network.go - network ring buffer).
func (m *NetworkMonitor) Snapshot() []NetworkEvent {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buffer.Snapshot()
}

// Len returns the number of events in the ring buffer
// (spec L4024).
func (m *NetworkMonitor) Len() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buffer.Len()
}

// SubscriberCount returns the number of active subscribers
// (spec L4024: subscriber pattern).
func (m *NetworkMonitor) SubscriberCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subscribers)
}

// String returns a diagnostic summary.
func (m *NetworkMonitor) String() string {
	if m == nil {
		return "NetworkMonitor(nil)"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fmt.Sprintf("NetworkMonitor{events:%d subscribers:%d}", m.buffer.Len(), len(m.subscribers))
}
