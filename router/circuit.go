package router

import (
	"sync"
	"time"
)

type circuitBreaker struct {
	mu           sync.Mutex
	threshold    int
	openWindow   time.Duration
	failures     int
	openedAt     time.Time
	open         bool
	halfOpenUsed bool
}

func newCircuitBreaker(threshold int, openWindow time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = 1
	}
	if openWindow <= 0 {
		openWindow = time.Minute
	}
	return &circuitBreaker{threshold: threshold, openWindow: openWindow}
}

func (b *circuitBreaker) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return true
	}
	if now.Sub(b.openedAt) < b.openWindow {
		return false
	}
	if b.halfOpenUsed {
		return false
	}
	b.halfOpenUsed = true
	return true
}

func (b *circuitBreaker) success() {
	b.mu.Lock()
	b.failures = 0
	b.open = false
	b.halfOpenUsed = false
	b.mu.Unlock()
}

func (b *circuitBreaker) failure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.open = true
		b.openedAt = now
		b.halfOpenUsed = false
	}
}
