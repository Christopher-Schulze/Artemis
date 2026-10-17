package bridge

import (
	"context"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/network"
)

// SetRequestFilter replaces the browser-wide subresource filter (spec L4206);
// nil turns ad/tracker blocking off for every page of this browser.
func (b *ChromiumBrowser) SetRequestFilter(filter network.RequestFilter) {
	b.policy.SetRequestFilter(filter)
}

// AdBlockedRequests returns how many requests of the current top-level
// document the ad/tracker filter dropped (spec L4208 Case B signal).
func (p *Page) AdBlockedRequests() int64 {
	return p.adBlocked.Load()
}

// SetAdBlockBypass loads subsequent requests without ad/tracker filtering;
// Layer 2 uses it for the verification reload.
func (p *Page) SetAdBlockBypass(bypass bool) {
	p.adBypass.Store(bypass)
}

// adBlockContext records the site on a top-level navigation (resetting the
// per-document counter) or its redirect and tags ctx with the request's
// document context.
func (p *Page) adBlockContext(ctx context.Context, kind network.TargetKind, sessionID string, request fetchRequestPaused) context.Context {
	mainDocument := strings.EqualFold(request.ResourceType, "document") && (kind == network.TargetNavigation || kind == network.TargetRedirect && sessionID == p.sessionID)
	if mainDocument {
		site := ""
		if parsed, err := url.Parse(request.Request.URL); err == nil {
			site = strings.ToLower(parsed.Hostname())
		}
		p.adSite.Store(&site)
		if kind == network.TargetNavigation {
			p.adBlocked.Store(0)
		}
	}
	site := ""
	if current := p.adSite.Load(); current != nil {
		site = *current
	}
	return network.WithAdBlockScope(ctx, network.AdBlockScope{
		SiteHost: site,
		Resource: strings.ToLower(request.ResourceType),
		Bypass:   p.adBypass.Load(),
		Blocked:  &p.adBlocked,
	})
}
