package network

import (
	"context"
	"errors"
)

// RequestLifecycle owns admission, cancellation, concurrency release, and
// response-byte accounting for one HTTP request.
type RequestLifecycle interface {
	BeginRequest(context.Context) (context.Context, func(int64) error, error)
}

// MemoryAdmission reserves transient input/image memory before allocation.
// Release follows joined consumption and actual unmapping/native cleanup.
// The embedding application's canonical resource owner implements this port.
type MemoryAdmission interface {
	ReserveMemory(context.Context, int64) (func() error, error)
}

func (c *HTTPClient) beginRequest(ctx context.Context) (context.Context, func(int64) error, error) {
	if ctx == nil {
		return nil, nil, errors.New("network: request context required")
	}
	c.sessionMu.Lock()
	closed := c.closed
	c.sessionMu.Unlock()
	if closed {
		return nil, nil, errors.New("network: request on closed client")
	}
	if c.cfg.RequestLifecycle == nil {
		return ctx, func(int64) error { return nil }, nil
	}
	return c.cfg.RequestLifecycle.BeginRequest(ctx)
}
