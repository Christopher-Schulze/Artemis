package renderless

import (
	"fmt"
	"sync"
)

// pool.go (spec L4022: renderless/pool.go - context pool management).
//
// In-process no-render JS browser path: context pool management
// utilities. This file provides higher-level pool management on top
// of context.go's ContextPool, including builder pools and string
// buffer pools for efficient memory reuse.
//
// Ref: research/artemis/internal/pool/pool.go:1-26

// BuilderPool is a sync.Pool for strings.Builder reuse
// (spec L4022: context pool management).
type BuilderPool struct {
	pool sync.Pool
}

// NewBuilderPool creates a new BuilderPool
// (spec L4022: context pool management).
func NewBuilderPool() *BuilderPool {
	return &BuilderPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &stringsBuilder{}
			},
		},
	}
}

// Get retrieves a strings.Builder from the pool
// (spec L4022: context pool management).
func (p *BuilderPool) Get() *stringsBuilder {
	return p.pool.Get().(*stringsBuilder)
}

// Put returns a strings.Builder to the pool
// (spec L4022: context pool management).
func (p *BuilderPool) Put(b *stringsBuilder) {
	b.Reset()
	p.pool.Put(b)
}

// stringsBuilder wraps strings.Builder for pool compatibility
type stringsBuilder struct {
	data []byte
}

// Write writes bytes to the builder
func (b *stringsBuilder) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// WriteString writes a string to the builder
func (b *stringsBuilder) WriteString(s string) (int, error) {
	b.data = append(b.data, s...)
	return len(s), nil
}

// String returns the builder content as a string
func (b *stringsBuilder) String() string {
	return string(b.data)
}

// Reset resets the builder
func (b *stringsBuilder) Reset() {
	b.data = b.data[:0]
}

// Len returns the builder length
func (b *stringsBuilder) Len() int {
	return len(b.data)
}

// Bytes returns the builder content as bytes
func (b *stringsBuilder) Bytes() []byte {
	return b.data
}

// IsolatePool manages V8 isolate allocation
// (spec L4022: context pool management).
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
