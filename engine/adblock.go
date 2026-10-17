package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/security"
)

// AdBlocker returns the engine's ad/tracker blocker (spec L4206), or nil when
// Config.DisableAdBlock is set.
func (e *Engine) AdBlocker() *security.AdBlocker {
	return e.adBlock
}

// newAdBlockScope is the document context of one page: its site, the
// verification bypass and the page's blocked-request counter.
func newAdBlockScope(pageURL string, opts FetchOpts) network.AdBlockScope {
	return network.AdBlockScope{SiteHost: hostOf(pageURL), Bypass: opts.AdBlockBypass, Blocked: new(atomic.Int64)}
}

// withAdBlockResource tags ctx with scope and the request's resource type.
func withAdBlockResource(ctx context.Context, scope network.AdBlockScope, resource string) context.Context {
	scope.Resource = resource
	return network.WithAdBlockScope(ctx, scope)
}

// AdBlockedRequests returns how many of this page's subresource requests the
// ad/tracker blocker dropped.
func (p *Page) AdBlockedRequests() int64 {
	if p == nil || p.adBlocked == nil {
		return 0
	}
	return p.adBlocked.Load()
}

// AdBlockFinding returns the Layer 2 finding recorded when the page loaded
// (spec L4208), or nil.
func (p *Page) AdBlockFinding() *security.AdBlockFinding {
	if p == nil {
		return nil
	}
	return p.adFinding.Clone()
}

// applyAdBlockLayer2 runs Layer 2 for a page whose requests were blocked and
// returns the page to keep. Case B repeats the GET with blocking bypassed;
// a better load whitelists the site and replaces the broken page.
func (e *Engine) applyAdBlockLayer2(ctx context.Context, method string, opts FetchOpts, page *Page) *Page {
	blocked := page.AdBlockedRequests()
	if e.adBlock == nil || opts.AdBlockBypass || blocked == 0 {
		return page
	}
	site := hostOf(page.URL())
	probe, err := renderlessAdBlockProbe(ctx, page)
	if err != nil {
		page.adFinding = &security.AdBlockFinding{Site: site, BlockedRequests: blocked, Signals: []string{err.Error()}, Decision: security.AdBlockDecisionUnavailable}
		return page
	}
	finding := security.ClassifyAdBlock(probe, blocked, 0, site)
	page.adFinding = finding
	if finding == nil || finding.Case != security.AdBlockCaseBreakage {
		return page
	}
	if method != http.MethodGet {
		finding.Signals = append(finding.Signals, "verification_skipped: non-GET request")
		return page
	}
	retryOpts := opts
	retryOpts.AdBlockBypass = true
	retry, err := e.Fetch(ctx, page.URL(), retryOpts)
	if err != nil {
		finding.Signals = append(finding.Signals, "verification_failed: "+err.Error())
		return page
	}
	retryProbe, err := renderlessAdBlockProbe(ctx, retry)
	if err == nil && security.ImprovedWithoutBlocking(probe, retryProbe) {
		err = e.adBlock.AcceptBreakage(site)
		if err == nil {
			finding.Decision = security.AdBlockDecisionWhitelisted
			finding.Signals = append(finding.Signals, fmt.Sprintf("verification_text=%d", retryProbe.TextLength))
			retry.adFinding = finding
			if closeErr := page.Close(); closeErr != nil {
				finding.Signals = append(finding.Signals, "blocked_page_close: "+closeErr.Error())
			}
			return retry
		}
	}
	if err != nil {
		finding.Signals = append(finding.Signals, "verification_failed: "+err.Error())
	} else {
		finding.Signals = append(finding.Signals, fmt.Sprintf("verification_text=%d", retryProbe.TextLength))
	}
	if closeErr := retry.Close(); closeErr != nil {
		finding.Signals = append(finding.Signals, "verification_close: "+closeErr.Error())
	}
	return page
}

func renderlessAdBlockProbe(ctx context.Context, page *Page) (security.AdBlockProbe, error) {
	raw, err := page.EvalString(ctx, security.AdBlockProbeScript(false))
	if err != nil {
		return security.AdBlockProbe{}, fmt.Errorf("adblock probe: %w", err)
	}
	return security.ParseAdBlockProbe(raw)
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
