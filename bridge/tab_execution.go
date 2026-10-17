package bridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

// PageLease holds the canonical execution admission for one target. It can be
// passed from a session's reference validation into its action runtime.
type PageLease struct {
	mu      sync.Mutex
	page    *Page
	ctx     context.Context
	release func()
}

type executionLeaseContextKey struct{}

// SetMaxParallelTabs configures cross-tab execution before pages are admitted.
func (c *BrowserContext) SetMaxParallelTabs(limit int) error {
	if limit < 1 {
		return fmt.Errorf("tab execution: positive parallelism required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || len(c.pages) != 0 {
		return fmt.Errorf("tab execution: configure before opening pages")
	}
	c.executor = tabs.NewTabExecutor(nil, limit)
	return nil
}

// AcquireExecution serializes one tab and bounds execution across its context.
// Cancellation of either the caller or target lifetime releases queued callers.
func (p *Page) AcquireExecution(ctx context.Context) (*PageLease, error) {
	if ctx == nil || p == nil || p.owner == nil {
		return nil, fmt.Errorf("tab execution: context and owned page required")
	}
	if err := p.ensureAttached(); err != nil {
		return nil, err
	}
	if p.lifecycle != nil && p.lifecycle.State() == tabs.TabLifecycleSuspended {
		return nil, fmt.Errorf("tab execution: target is suspended")
	}
	p.owner.mu.Lock()
	if p.owner.closed {
		p.owner.mu.Unlock()
		return nil, fmt.Errorf("tab execution: browser context is closed")
	}
	if p.owner.popupErr != nil {
		err := p.owner.popupErr
		p.owner.mu.Unlock()
		return nil, err
	}
	if p.owner.executor == nil {
		p.owner.executor = tabs.NewTabExecutor(nil, 4)
	}
	executor := p.owner.executor
	p.owner.mu.Unlock()
	operationCtx, cancel := context.WithCancel(ctx)
	p.mu.RLock()
	lifetime := p.lifetime
	p.mu.RUnlock()
	var stop func() bool
	if lifetime != nil {
		stop = context.AfterFunc(lifetime, cancel)
	}
	cleanup := func() {
		if stop != nil {
			stop()
		}
		cancel()
	}
	release, err := executor.Acquire(operationCtx, p.targetID)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := p.ensureAttached(); err != nil {
		release()
		cleanup()
		return nil, err
	}
	p.owner.mu.Lock()
	closed := p.owner.closed
	popupErr := p.owner.popupErr
	p.owner.mu.Unlock()
	if closed || popupErr != nil {
		release()
		cleanup()
		if popupErr != nil {
			return nil, popupErr
		}
		return nil, fmt.Errorf("tab execution: browser context closed while queued")
	}
	if p.lifecycle != nil && p.lifecycle.State() == tabs.TabLifecycleSuspended {
		release()
		cleanup()
		return nil, fmt.Errorf("tab execution: target suspended while queued")
	}
	if err := p.transitionLifecycle(tabs.TabLifecycleActive); err != nil {
		release()
		cleanup()
		return nil, err
	}
	return &PageLease{page: p, ctx: operationCtx, release: func() {
		if err := p.transitionLifecycle(tabs.TabLifecycleIdle); err != nil {
			p.mu.Lock()
			p.retirementErr = err
			p.mu.Unlock()
		}
		release()
		cleanup()
	}}, nil
}

// Context carries both caller and target cancellation for admitted I/O.
func (l *PageLease) Context() context.Context {
	return context.WithValue(l.ctx, executionLeaseContextKey{}, l.page)
}

// Run keeps a held lease valid until the callback returns. It rejects foreign
// or released leases and serializes accidental concurrent reuse of one lease.
func (l *PageLease) Run(page *Page, action func(context.Context)) error {
	if l == nil || action == nil {
		return fmt.Errorf("tab execution: lease and action required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.release == nil || l.page != page {
		return fmt.Errorf("tab execution: active lease for this page required")
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	action(l.Context())
	return nil
}

func (p *Page) drainExecution(ctx context.Context) error {
	if ctx.Value(executionLeaseContextKey{}) == p {
		return nil // Explicit self-close already owns this target's admission.
	}
	p.owner.mu.Lock()
	executor := p.owner.executor
	p.owner.mu.Unlock()
	if executor == nil {
		return nil
	}
	drainCtx, cancel := context.WithTimeout(ctx, defaultCDPDialTimeout)
	defer cancel()
	if err := executor.WaitTabIdle(drainCtx, p.targetID); err != nil {
		return fmt.Errorf("drain target execution: %w", err)
	}
	return nil
}

// Release is idempotent and waits for an in-progress Run callback to finish.
func (l *PageLease) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.release != nil {
		l.release()
		l.release = nil
	}
}
