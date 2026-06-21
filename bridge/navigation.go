package bridge

import (
	"context"
	"errors"
	"strings"
	"time"
)

// WaitMode is the page load wait strategy (spec L4557).
type WaitMode string

const (
	WaitDOMContentLoaded WaitMode = "domcontentloaded"
	WaitLoad             WaitMode = "load"
	WaitNetworkIdle      WaitMode = "networkidle"
	WaitCustom           WaitMode = "custom"
)

// PageLoadStrategy configures how navigation waits for page readiness
// (spec L4557).
type PageLoadStrategy struct {
	Mode            WaitMode
	CustomSelector  string        // for WaitCustom
	Timeout         time.Duration // hard cap, default 30s
	FallbackAfter   time.Duration // proceed-after fallback for networkidle, default 10s
	NetworkIdleWait time.Duration // no-requests window, default 500ms
	SPADetected     bool          // auto-detect SPA -> force networkidle
}

// DefaultPageLoadStrategy returns the spec-default strategy: networkidle,
// 30s timeout, 10s fallback, 500ms idle window.
func DefaultPageLoadStrategy() PageLoadStrategy {
	return PageLoadStrategy{
		Mode:            WaitNetworkIdle,
		Timeout:         30 * time.Second,
		FallbackAfter:   10 * time.Second,
		NetworkIdleWait: 500 * time.Millisecond,
	}
}

// Validate checks the strategy for spec compliance.
func (p PageLoadStrategy) Validate() error {
	switch p.Mode {
	case WaitDOMContentLoaded, WaitLoad, WaitNetworkIdle, WaitCustom:
	default:
		return errors.New("navigation: invalid wait mode")
	}
	if p.Timeout <= 0 {
		return errors.New("navigation: timeout must be positive")
	}
	if p.Mode == WaitCustom && strings.TrimSpace(p.CustomSelector) == "" {
		return errors.New("navigation: custom mode requires selector")
	}
	if p.FallbackAfter < 0 {
		return errors.New("navigation: fallback after must be non-negative")
	}
	if p.NetworkIdleWait <= 0 {
		return errors.New("navigation: network idle wait must be positive")
	}
	return nil
}

// ResolveMode applies SPA auto-detect: if SPADetected is true and mode is
// not explicitly custom, force networkidle (spec L4557).
func (p PageLoadStrategy) ResolveMode() WaitMode {
	if p.SPADetected && p.Mode != WaitCustom {
		return WaitNetworkIdle
	}
	return p.Mode
}

// EffectiveFallback returns the fallback duration, only meaningful for
// networkidle mode (spec L4557: infinite polling -> proceed after 10s).
func (p PageLoadStrategy) EffectiveFallback() time.Duration {
	if p.ResolveMode() != WaitNetworkIdle {
		return 0
	}
	if p.FallbackAfter > 0 {
		return p.FallbackAfter
	}
	return 10 * time.Second
}

// NavigationResult is the outcome of a Navigate call.
type NavigationResult struct {
	URL            string
	Mode           WaitMode
	FinalWait      time.Duration
	FallbackUsed   bool
	TimedOut       bool
	CustomSelector string
}

// Navigator is the abstract navigation surface. Real implementations call
// CDP Page.navigate + wait events; tests inject a fake.
type Navigator interface {
	Navigate(ctx context.Context, url string) error
	WaitDOMContentLoaded(ctx context.Context) error
	WaitLoad(ctx context.Context) error
	WaitNetworkIdle(ctx context.Context, idleWindow time.Duration) error
	WaitCustomSelector(ctx context.Context, selector string) error
	DetectSPA(ctx context.Context, url string) bool
}

// Navigate executes a navigation with the given strategy (spec L4557).
//
// Pipeline:
//  1. Validate strategy.
//  2. Resolve mode (SPA auto-detect -> networkidle).
//  3. Navigate.
//  4. Wait per mode, with hard timeout and networkidle fallback.
//
// Returns NavigationResult describing what happened.
func Navigate(ctx context.Context, nav Navigator, url string, strategy PageLoadStrategy) (NavigationResult, error) {
	if nav == nil {
		return NavigationResult{}, errors.New("navigation: nil navigator")
	}
	if err := strategy.Validate(); err != nil {
		return NavigationResult{}, err
	}
	mode := strategy.ResolveMode()

	navCtx, cancel := context.WithTimeout(ctx, strategy.Timeout)
	defer cancel()

	if err := nav.Navigate(navCtx, url); err != nil {
		return NavigationResult{URL: url, Mode: mode, TimedOut: errors.Is(err, context.DeadlineExceeded)}, err
	}

	start := time.Now()
	res := NavigationResult{URL: url, Mode: mode, CustomSelector: strategy.CustomSelector}

	switch mode {
	case WaitDOMContentLoaded:
		err := nav.WaitDOMContentLoaded(navCtx)
		res.FinalWait = time.Since(start)
		res.TimedOut = errors.Is(err, context.DeadlineExceeded)
		if err != nil && !res.TimedOut {
			return res, err
		}
	case WaitLoad:
		err := nav.WaitLoad(navCtx)
		res.FinalWait = time.Since(start)
		res.TimedOut = errors.Is(err, context.DeadlineExceeded)
		if err != nil && !res.TimedOut {
			return res, err
		}
	case WaitNetworkIdle:
		// Use a fallback context so we proceed after FallbackAfter even if
		// networkidle never settles (spec L4557: infinite polling -> proceed).
		fbCtx, fbCancel := context.WithTimeout(navCtx, strategy.EffectiveFallback())
		err := nav.WaitNetworkIdle(fbCtx, strategy.NetworkIdleWait)
		fbCancel()
		res.FinalWait = time.Since(start)
		if errors.Is(err, context.DeadlineExceeded) {
			res.FallbackUsed = true
		} else if err != nil {
			return res, err
		}
	case WaitCustom:
		err := nav.WaitCustomSelector(navCtx, strategy.CustomSelector)
		res.FinalWait = time.Since(start)
		res.TimedOut = errors.Is(err, context.DeadlineExceeded)
		if err != nil && !res.TimedOut {
			return res, err
		}
	}

	return res, nil
}

// ParseWaitMode parses a config string into a WaitMode with a safe default.
func ParseWaitMode(raw string) WaitMode {
	switch WaitMode(strings.ToLower(strings.TrimSpace(raw))) {
	case WaitDOMContentLoaded:
		return WaitDOMContentLoaded
	case WaitLoad:
		return WaitLoad
	case WaitCustom:
		return WaitCustom
	default:
		return WaitNetworkIdle
	}
}
