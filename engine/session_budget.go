package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultSessionMaxTabs          = 8
	DefaultSessionMaxRequests      = 256
	DefaultSessionMaxResponseBytes = int64(256 * 1024 * 1024)
	DefaultSessionMaxConcurrency   = 16
	DefaultSessionTimeout          = 30 * time.Minute
)

// SessionBudget contains hard renderless-engine limits. Zero values receive
// secure defaults; negative values are invalid.
type SessionBudget struct {
	MaxTabs          int
	MaxRequests      int
	MaxResponseBytes int64
	MaxDiskBytes     int64
	MaxConcurrency   int
	Timeout          time.Duration
}

// SessionUsage is an atomic snapshot of admitted session work.
type SessionUsage struct {
	Requests      int
	ResponseBytes int64
	ActiveTabs    int
	Concurrent    int
	Cancelled     bool
	TerminalError string
}

// BudgetResource identifies the exhausted session resource.
type BudgetResource string

const (
	BudgetTabs          BudgetResource = "tabs"
	BudgetRequests      BudgetResource = "requests"
	BudgetResponseBytes BudgetResource = "response_bytes"
	BudgetConcurrency   BudgetResource = "concurrency"
	BudgetTimeout       BudgetResource = "timeout"
)

// BudgetError is returned when admitting or accounting work would exceed a
// hard session limit. The first breach cancels all remaining session work.
type BudgetError struct {
	Resource BudgetResource
	Limit    int64
	Observed int64
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("artemis session budget exceeded: %s limit=%d observed=%d", e.Resource, e.Limit, e.Observed)
}

// IsBudgetExceeded reports whether err contains a session budget failure.
func IsBudgetExceeded(err error) bool {
	var budgetErr *BudgetError
	return errors.As(err, &budgetErr)
}

func (b *SessionBudget) applyDefaults(defaultDiskBytes int64) {
	if b.MaxTabs == 0 {
		b.MaxTabs = DefaultSessionMaxTabs
	}
	if b.MaxRequests == 0 {
		b.MaxRequests = DefaultSessionMaxRequests
	}
	if b.MaxResponseBytes == 0 {
		b.MaxResponseBytes = DefaultSessionMaxResponseBytes
	}
	if b.MaxDiskBytes == 0 {
		b.MaxDiskBytes = defaultDiskBytes
	}
	if b.MaxConcurrency == 0 {
		b.MaxConcurrency = DefaultSessionMaxConcurrency
	}
	if b.Timeout == 0 {
		b.Timeout = DefaultSessionTimeout
	}
}

func (b SessionBudget) validate() error {
	if b.MaxTabs < 1 || b.MaxRequests < 1 || b.MaxResponseBytes < 1 || b.MaxDiskBytes < 1 || b.MaxConcurrency < 1 || b.Timeout < time.Millisecond {
		return errors.New("engine: session budget limits must be positive and timeout must be at least 1ms")
	}
	return nil
}

type sessionBudgetController struct {
	mu            sync.Mutex
	limits        SessionBudget
	ctx           context.Context
	cancel        context.CancelCauseFunc
	stopTimeout   func() bool
	requests      int
	responseBytes int64
	activeTabs    int
	concurrent    int
	terminal      error
}

func newSessionBudgetController(limits SessionBudget) *sessionBudgetController {
	ctx, cancel := context.WithCancelCause(context.Background())
	c := &sessionBudgetController{limits: limits, ctx: ctx, cancel: cancel}
	timer := time.AfterFunc(limits.Timeout, func() {
		c.fail(&BudgetError{Resource: BudgetTimeout, Limit: int64(limits.Timeout), Observed: int64(limits.Timeout) + 1})
	})
	c.stopTimeout = timer.Stop
	return c
}

func (c *sessionBudgetController) BeginRequest(ctx context.Context) (context.Context, func(int64) error, error) {
	if ctx == nil {
		return nil, nil, errors.New("engine: request context required")
	}
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return nil, nil, err
	}
	if c.requests >= c.limits.MaxRequests {
		err := &BudgetError{Resource: BudgetRequests, Limit: int64(c.limits.MaxRequests), Observed: int64(c.requests + 1)}
		c.mu.Unlock()
		c.fail(err)
		return nil, nil, err
	}
	if c.concurrent >= c.limits.MaxConcurrency {
		err := &BudgetError{Resource: BudgetConcurrency, Limit: int64(c.limits.MaxConcurrency), Observed: int64(c.concurrent + 1)}
		c.mu.Unlock()
		c.fail(err)
		return nil, nil, err
	}
	c.requests++
	c.concurrent++
	c.mu.Unlock()
	requestCtx, releaseContext := c.mergeContext(ctx)
	var once sync.Once
	finish := func(responseBytes int64) error {
		var result error
		once.Do(func() {
			releaseContext()
			result = c.finishRequest(responseBytes)
		})
		return result
	}
	return requestCtx, finish, nil
}

func (c *sessionBudgetController) finishRequest(responseBytes int64) error {
	if responseBytes < 0 {
		responseBytes = 0
	}
	c.mu.Lock()
	if c.concurrent > 0 {
		c.concurrent--
	}
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return err
	}
	observed := c.responseBytes + responseBytes
	if observed > c.limits.MaxResponseBytes {
		err := &BudgetError{Resource: BudgetResponseBytes, Limit: c.limits.MaxResponseBytes, Observed: observed}
		c.mu.Unlock()
		c.fail(err)
		return err
	}
	c.responseBytes = observed
	c.mu.Unlock()
	return nil
}

func (c *sessionBudgetController) admitSynthetic(responseBytes int64) error {
	_, finish, err := c.BeginRequest(context.Background())
	if err != nil {
		return err
	}
	return finish(responseBytes)
}

func (c *sessionBudgetController) acquireTab() error {
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return err
	}
	if c.activeTabs >= c.limits.MaxTabs {
		err := &BudgetError{Resource: BudgetTabs, Limit: int64(c.limits.MaxTabs), Observed: int64(c.activeTabs + 1)}
		c.mu.Unlock()
		c.fail(err)
		return err
	}
	c.activeTabs++
	c.mu.Unlock()
	return nil
}

func (c *sessionBudgetController) releaseTab() {
	c.mu.Lock()
	if c.activeTabs > 0 {
		c.activeTabs--
	}
	c.mu.Unlock()
}

func (c *sessionBudgetController) fail(err error) {
	c.mu.Lock()
	if c.terminal == nil {
		c.terminal = err
	}
	terminal := c.terminal
	c.mu.Unlock()
	c.cancel(terminal)
}

func (c *sessionBudgetController) mergeContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(c.ctx, func() { cancel(context.Cause(c.ctx)) })
	return ctx, func() {
		stop()
		cancel(context.Canceled)
	}
}

func (c *sessionBudgetController) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminal
}

func (c *sessionBudgetController) usage() SessionUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	usage := SessionUsage{
		Requests: c.requests, ResponseBytes: c.responseBytes, ActiveTabs: c.activeTabs,
		Concurrent: c.concurrent, Cancelled: c.terminal != nil,
	}
	if c.terminal != nil {
		usage.TerminalError = c.terminal.Error()
	}
	return usage
}

func (c *sessionBudgetController) close() {
	if c.stopTimeout != nil {
		c.stopTimeout()
	}
	c.fail(context.Canceled)
}
