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

func (c *HTTPClient) beginRequest(ctx context.Context) (context.Context, func(int64) error, error) {
	if ctx == nil {
		return nil, nil, errors.New("network: request context required")
	}
	if c.cfg.RequestLifecycle == nil {
		return ctx, func(int64) error { return nil }, nil
	}
	return c.cfg.RequestLifecycle.BeginRequest(ctx)
}
