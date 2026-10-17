package renderless

import (
	"fmt"
	"sync"
)

// pool.go (spec L4022: renderless/pool.go - context pool management).
//
// In-process no-render JS browser path: isolate slot accounting on top of
// context.go's ContextPool. Reusable dump buffers are owned by
// internal/pool.
//
// Ref: research/artemis/internal/pool/pool.go:1-26

// IsolatePool accounts bounded isolate slots by integer ID; it holds no V8
// isolates and no memory (spec L4022: context pool management).
type IsolatePool struct {
	mu       sync.Mutex
	isolates []int
	inUse    map[int]bool
	nextID   int
	maxSize  int
}

// NewIsolatePool creates a new IsolatePool
// (spec L4022: context pool management).
func NewIsolatePool(maxSize int) *IsolatePool {
	if maxSize <= 0 {
		maxSize = 4
	}
	return &IsolatePool{
		isolates: make([]int, 0, maxSize),
		inUse:    make(map[int]bool),
		maxSize:  maxSize,
	}
}

// Acquire gets an isolate ID from the pool
// (spec L4022: context pool management).
func (p *IsolatePool) Acquire() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.isolates) > 0 {
		id := p.isolates[len(p.isolates)-1]
		p.isolates = p.isolates[:len(p.isolates)-1]
		p.inUse[id] = true
		return id, nil
	}
	if len(p.inUse) >= p.maxSize {
		return 0, fmt.Errorf("isolate pool: exhausted")
	}
	p.nextID++
	id := p.nextID
	p.inUse[id] = true
	return id, nil
}

// Release returns an isolate ID to the pool
// (spec L4022: context pool management).
func (p *IsolatePool) Release(id int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inUse, id)
	p.isolates = append(p.isolates, id)
}

// InUseCount returns the number of isolates in use
// (spec L4022: context pool management).
func (p *IsolatePool) InUseCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inUse)
}

// AvailableCount returns the number of available isolates
// (spec L4022: context pool management).
func (p *IsolatePool) AvailableCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.isolates)
}

// String returns a diagnostic summary.
func (p *IsolatePool) String() string {
	return fmt.Sprintf("IsolatePool{available:%d inUse:%d max:%d}",
		p.AvailableCount(), p.InUseCount(), p.maxSize)
}
