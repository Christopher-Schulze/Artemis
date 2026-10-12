package renderless

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// context.go (spec L4022: renderless/context.go - context pool/warm
// pool).
//
// In-process no-render JS browser path: context pool for managing
// warm V8/v8go contexts. Pre-warmed contexts reduce initialization
// overhead for script execution.

// ContextPool manages a pool of warm runtime contexts
// (spec L4022: context pool/warm pool).
type ContextPool struct {
	mu        sync.Mutex
	available []*RuntimeContext
	inUse     map[string]*RuntimeContext
	maxSize   int
	nextID    atomic.Int64
	createAt  time.Time
}

// NewContextPool creates a new context pool
// (spec L4022: context pool/warm pool).
func NewContextPool(maxSize int) *ContextPool {
	if maxSize <= 0 {
		maxSize = 4
	}
	return &ContextPool{
		available: make([]*RuntimeContext, 0, maxSize),
		inUse:     make(map[string]*RuntimeContext),
		maxSize:   maxSize,
		createAt:  time.Now(),
	}
}

// Acquire gets a context from the pool or creates a new one
// (spec L4022: context pool/warm pool).
func (p *ContextPool) Acquire() *RuntimeContext {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.available) > 0 {
		ctx := p.available[len(p.available)-1]
		p.available = p.available[:len(p.available)-1]
		p.inUse[ctx.ID()] = ctx
		ctx.Touch()
		return ctx
	}
	if len(p.inUse) >= p.maxSize {
		return nil // pool exhausted
	}
	id := fmt.Sprintf("ctx-%d", p.nextID.Add(1))
	ctx := NewRuntimeContext(id, int(p.nextID.Load()))
	p.inUse[id] = ctx
	return ctx
}

// Release returns a context to the pool
// (spec L4022: context pool/warm pool).
func (p *ContextPool) Release(ctx *RuntimeContext) {
	if ctx == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inUse, ctx.ID())
	if !ctx.IsClosed() && len(p.available) < p.maxSize {
		p.available = append(p.available, ctx)
	}
}

// Available returns the number of available contexts
// (spec L4022: context pool/warm pool).
func (p *ContextPool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.available)
}

// InUse returns the number of contexts currently in use
// (spec L4022: context pool/warm pool).
func (p *ContextPool) InUse() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inUse)
}

// MaxSize returns the maximum pool size
// (spec L4022: context pool/warm pool).
func (p *ContextPool) MaxSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxSize
}

// Close closes all contexts in the pool
// (spec L4022: context pool/warm pool).
func (p *ContextPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ctx := range p.available {
		ctx.Close()
	}
	for _, ctx := range p.inUse {
		ctx.Close()
	}
	p.available = nil
	p.inUse = make(map[string]*RuntimeContext)
}

// Warm pre-warms the pool with the specified number of contexts
// (spec L4022: context pool/warm pool).
func (p *ContextPool) Warm(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := 0; i < count && len(p.available) < p.maxSize; i++ {
		id := fmt.Sprintf("ctx-%d", p.nextID.Add(1))
		ctx := NewRuntimeContext(id, int(p.nextID.Load()))
		p.available = append(p.available, ctx)
	}
}

// String returns a diagnostic summary.
func (p *ContextPool) String() string {
	return fmt.Sprintf("ContextPool{available:%d inUse:%d max:%d}",
		p.Available(), p.InUse(), p.MaxSize())
}
