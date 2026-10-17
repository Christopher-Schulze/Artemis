package network

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
)

// ErrAdBlocked is returned when the configured request filter (ad/tracker
// blocking, spec L4206) drops a subresource. Callers treat it like a
// browser's blocked-by-client load failure, not as a policy violation.
var ErrAdBlocked = errors.New("network: request blocked by ad/tracker filter")

// RequestFilter decides whether an admitted subresource request is dropped.
// It runs before DNS resolution, so a blocked request costs no lookup.
type RequestFilter interface {
	BlockRequest(ctx context.Context, target *url.URL, kind TargetKind, sessionID string) bool
}

// AdBlockScope is the document context of a request: the top-level site,
// the request resource type (CDP names such as "script", "image",
// "stylesheet", "xhr", "document"), whether filtering is bypassed for a
// verification reload, and the per-page counter of blocked requests.
type AdBlockScope struct {
	SiteHost string
	Resource string
	Bypass   bool
	Blocked  *atomic.Int64
}

type adBlockScopeKey struct{}

// WithAdBlockScope attaches the request's document context.
func WithAdBlockScope(ctx context.Context, scope AdBlockScope) context.Context {
	return context.WithValue(ctx, adBlockScopeKey{}, scope)
}

// AdBlockScopeFrom returns the request's document context, if any.
func AdBlockScopeFrom(ctx context.Context) (AdBlockScope, bool) {
	scope, ok := ctx.Value(adBlockScopeKey{}).(AdBlockScope)
	return scope, ok
}

type requestFilterHolder struct {
	filter RequestFilter
}

// SetRequestFilter installs filter for every request this policy admits; nil
// removes it.
func (p *Policy) SetRequestFilter(filter RequestFilter) {
	if filter == nil {
		p.filter.Store(nil)
		return
	}
	p.filter.Store(&requestFilterHolder{filter: filter})
}

// filterRequest applies the request filter to subresource-like targets. A
// redirect is filtered only when its scope marks a non-document resource,
// so top-level navigation and its redirects are never blocked.
func (p *Policy) filterRequest(ctx context.Context, rawURL string, kind TargetKind, sessionID string) error {
	holder := p.filter.Load()
	if holder == nil {
		return nil
	}
	switch kind {
	case TargetSubresource, TargetSubframe, TargetWorker, TargetWebSocket:
	case TargetRedirect:
		scope, ok := AdBlockScopeFrom(ctx)
		if !ok || scope.Resource == "" || scope.Resource == "document" {
			return nil
		}
	default:
		return nil
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" {
		return nil
	}
	if holder.filter.BlockRequest(ctx, target, kind, sessionID) {
		return ErrAdBlocked
	}
	return nil
}
