package network

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
)

type hostFilter struct {
	host  string
	scope AdBlockScope
	calls atomic.Int64
}

func (f *hostFilter) BlockRequest(ctx context.Context, target *url.URL, _ TargetKind, _ string) bool {
	f.calls.Add(1)
	f.scope, _ = AdBlockScopeFrom(ctx)
	return target.Hostname() == f.host
}

func TestRequestFilterBlocksSubresourcesBeforeDNSAndNeverNavigation(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}}
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	filter := &hostFilter{host: "ads.test"}
	policy.SetRequestFilter(filter)
	ctx := WithAdBlockScope(context.Background(), AdBlockScope{SiteHost: "news.test", Resource: "script"})
	for _, kind := range []TargetKind{TargetSubresource, TargetSubframe, TargetWorker, TargetWebSocket} {
		if err := policy.ValidateRequest(ctx, "https://ads.test/a.js", "GET", "", 0, kind, "s"); !errors.Is(err, ErrAdBlocked) {
			t.Fatalf("%s to a filtered host was admitted: %v", kind, err)
		}
		if err := policy.ValidateProxiedRequest(ctx, "https://ads.test/a.js", "GET", "", 0, kind, "s"); !errors.Is(err, ErrAdBlocked) {
			t.Fatalf("proxied %s to a filtered host was admitted: %v", kind, err)
		}
	}
	if resolver.calls != 0 {
		t.Fatalf("blocked requests still resolved DNS: %d lookups", resolver.calls)
	}
	if filter.scope.SiteHost != "news.test" || filter.scope.Resource != "script" {
		t.Fatalf("filter did not receive the document scope: %+v", filter.scope)
	}
	before := filter.calls.Load()
	if err := policy.ValidateRequest(context.Background(), "https://ads.test/", "GET", "", 0, TargetNavigation, "s"); err != nil {
		t.Fatalf("top-level navigation was filtered: %v", err)
	}
	if err := policy.ValidateRequest(WithAdBlockScope(context.Background(), AdBlockScope{Resource: "document"}), "https://ads.test/", "GET", "", 0, TargetRedirect, "s"); err != nil {
		t.Fatalf("document redirect was filtered: %v", err)
	}
	if filter.calls.Load() != before {
		t.Fatal("filter consulted for navigation or document redirect")
	}
	if err := policy.ValidateRequest(ctx, "https://ads.test/pixel", "GET", "", 0, TargetRedirect, "s"); !errors.Is(err, ErrAdBlocked) {
		t.Fatalf("subresource redirect into a filtered host was admitted: %v", err)
	}
	policy.SetRequestFilter(nil)
	resolver.results = []resolverResult{{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}
	if err := policy.ValidateRequest(ctx, "https://ads.test/a.js", "GET", "", 0, TargetSubresource, "s"); err != nil {
		t.Fatalf("removed filter still blocks: %v", err)
	}
}
