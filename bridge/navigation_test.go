package bridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeNavigator struct {
	mu               sync.Mutex
	navigateCalls    int
	lastURL          string
	domReadyErr      error
	loadErr          error
	networkIdleErr   error
	customErr        error
	networkIdleCalls int
	customSelector   string
	spaDetected      bool
	navigateErr      error
}

func (f *fakeNavigator) Navigate(ctx context.Context, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.navigateCalls++
	f.lastURL = url
	return f.navigateErr
}
func (f *fakeNavigator) WaitDOMContentLoaded(ctx context.Context) error {
	return f.domReadyErr
}
func (f *fakeNavigator) WaitLoad(ctx context.Context) error {
	return f.loadErr
}
func (f *fakeNavigator) WaitNetworkIdle(ctx context.Context, idle time.Duration) error {
	f.mu.Lock()
	f.networkIdleCalls++
	f.mu.Unlock()
	return f.networkIdleErr
}
func (f *fakeNavigator) WaitCustomSelector(ctx context.Context, selector string) error {
	f.mu.Lock()
	f.customSelector = selector
	f.mu.Unlock()
	return f.customErr
}
func (f *fakeNavigator) DetectSPA(ctx context.Context, url string) bool {
	return f.spaDetected
}

func TestDefaultPageLoadStrategy(t *testing.T) {
	s := DefaultPageLoadStrategy()
	if s.Mode != WaitNetworkIdle {
		t.Fatalf("expected networkidle, got %s", s.Mode)
	}
	if s.Timeout != 30*time.Second {
		t.Fatalf("expected 30s timeout, got %v", s.Timeout)
	}
	if s.FallbackAfter != 10*time.Second {
		t.Fatalf("expected 10s fallback, got %v", s.FallbackAfter)
	}
	if s.NetworkIdleWait != 500*time.Millisecond {
		t.Fatalf("expected 500ms idle, got %v", s.NetworkIdleWait)
	}
}

func TestParseWaitModeDefaultsNetworkIdle(t *testing.T) {
	if m := ParseWaitMode(""); m != WaitNetworkIdle {
		t.Fatalf("empty must default networkidle, got %s", m)
	}
	if m := ParseWaitMode("garbage"); m != WaitNetworkIdle {
		t.Fatalf("unknown must default networkidle, got %s", m)
	}
	if m := ParseWaitMode("domcontentloaded"); m != WaitDOMContentLoaded {
		t.Fatalf("domcontentloaded must parse, got %s", m)
	}
	if m := ParseWaitMode("LOAD"); m != WaitLoad {
		t.Fatalf("load must parse case-insensitive, got %s", m)
	}
	if m := ParseWaitMode("custom"); m != WaitCustom {
		t.Fatalf("custom must parse, got %s", m)
	}
}

func TestPageLoadStrategyValidateInvalidMode(t *testing.T) {
	s := DefaultPageLoadStrategy()
	s.Mode = WaitMode("bogus")
	if err := s.Validate(); err == nil {
		t.Fatal("invalid mode must error")
	}
}

func TestPageLoadStrategyValidateCustomRequiresSelector(t *testing.T) {
	s := DefaultPageLoadStrategy()
	s.Mode = WaitCustom
	if err := s.Validate(); err == nil {
		t.Fatal("custom without selector must error")
	}
	s.CustomSelector = "#ready"
	if err := s.Validate(); err != nil {
		t.Fatalf("custom with selector must pass, got %v", err)
	}
}

func TestPageLoadStrategyValidateTimeoutPositive(t *testing.T) {
	s := DefaultPageLoadStrategy()
	s.Timeout = 0
	if err := s.Validate(); err == nil {
		t.Fatal("zero timeout must error")
	}
}

func TestPageLoadStrategyResolveModeSPAForcesNetworkIdle(t *testing.T) {
	s := DefaultPageLoadStrategy()
	s.Mode = WaitLoad
	s.SPADetected = true
	if m := s.ResolveMode(); m != WaitNetworkIdle {
		t.Fatalf("SPA must force networkidle, got %s", m)
	}
}

func TestPageLoadStrategyResolveModeSPADoesNotOverrideCustom(t *testing.T) {
	s := DefaultPageLoadStrategy()
	s.Mode = WaitCustom
	s.CustomSelector = "#x"
	s.SPADetected = true
	if m := s.ResolveMode(); m != WaitCustom {
		t.Fatalf("SPA must not override explicit custom, got %s", m)
	}
}

func TestPageLoadStrategyEffectiveFallbackOnlyForNetworkIdle(t *testing.T) {
	s := DefaultPageLoadStrategy()
	s.Mode = WaitLoad
	if fb := s.EffectiveFallback(); fb != 0 {
		t.Fatalf("non-networkidle must have 0 fallback, got %v", fb)
	}
	s.Mode = WaitNetworkIdle
	if fb := s.EffectiveFallback(); fb != 10*time.Second {
		t.Fatalf("networkidle must have 10s fallback, got %v", fb)
	}
}

func TestNavigateDOMContentLoadedSuccess(t *testing.T) {
	nav := &fakeNavigator{}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitDOMContentLoaded
	res, err := Navigate(context.Background(), nav, "https://example.com", s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != WaitDOMContentLoaded {
		t.Fatalf("expected domcontentloaded, got %s", res.Mode)
	}
	if res.TimedOut {
		t.Fatal("must not time out")
	}
	if res.FallbackUsed {
		t.Fatal("must not use fallback for domcontentloaded")
	}
}

func TestNavigateLoadSuccess(t *testing.T) {
	nav := &fakeNavigator{}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitLoad
	res, err := Navigate(context.Background(), nav, "https://example.com", s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != WaitLoad {
		t.Fatalf("expected load, got %s", res.Mode)
	}
}

func TestNavigateNetworkIdleFallbackUsed(t *testing.T) {
	nav := &fakeNavigator{networkIdleErr: context.DeadlineExceeded}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitNetworkIdle
	s.FallbackAfter = 50 * time.Millisecond
	res, err := Navigate(context.Background(), nav, "https://example.com", s)
	if err != nil {
		t.Fatalf("fallback must not return hard error, got %v", err)
	}
	if !res.FallbackUsed {
		t.Fatal("expected fallback used when networkidle times out")
	}
}

func TestNavigateNetworkIdleSuccess(t *testing.T) {
	nav := &fakeNavigator{}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitNetworkIdle
	s.FallbackAfter = 5 * time.Second
	res, err := Navigate(context.Background(), nav, "https://example.com", s)
	if err != nil {
		t.Fatal(err)
	}
	if res.FallbackUsed {
		t.Fatal("must not use fallback on success")
	}
}

func TestNavigateCustomSelector(t *testing.T) {
	nav := &fakeNavigator{}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitCustom
	s.CustomSelector = "#ready-marker"
	res, err := Navigate(context.Background(), nav, "https://example.com", s)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != WaitCustom {
		t.Fatalf("expected custom, got %s", res.Mode)
	}
	if res.CustomSelector != "#ready-marker" {
		t.Fatalf("expected selector recorded, got %s", res.CustomSelector)
	}
	nav.mu.Lock()
	got := nav.customSelector
	nav.mu.Unlock()
	if got != "#ready-marker" {
		t.Fatalf("navigator must receive selector, got %s", got)
	}
}

func TestNavigateNilNavigatorErrors(t *testing.T) {
	_, err := Navigate(context.Background(), nil, "https://example.com", DefaultPageLoadStrategy())
	if err == nil {
		t.Fatal("nil navigator must error")
	}
}

func TestNavigateInvalidStrategyErrors(t *testing.T) {
	nav := &fakeNavigator{}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitMode("bogus")
	_, err := Navigate(context.Background(), nav, "https://example.com", s)
	if err == nil {
		t.Fatal("invalid strategy must error")
	}
}

func TestNavigateNavigateErrorPropagates(t *testing.T) {
	nav := &fakeNavigator{navigateErr: errors.New("network down")}
	_, err := Navigate(context.Background(), nav, "https://example.com", DefaultPageLoadStrategy())
	if err == nil {
		t.Fatal("navigate error must propagate")
	}
}

func TestNavigateSPADetectedForcesNetworkIdle(t *testing.T) {
	nav := &fakeNavigator{}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitLoad
	s.SPADetected = true
	res, _ := Navigate(context.Background(), nav, "https://example.com", s)
	if res.Mode != WaitNetworkIdle {
		t.Fatalf("SPA must force networkidle, got %s", res.Mode)
	}
}

func TestNavigateTimeoutContextDeadlineExceeded(t *testing.T) {
	nav := &fakeNavigator{domReadyErr: context.DeadlineExceeded}
	s := DefaultPageLoadStrategy()
	s.Mode = WaitDOMContentLoaded
	s.Timeout = 30 * time.Millisecond
	res, err := Navigate(context.Background(), nav, "https://example.com", s)
	// DeadlineExceeded on wait is treated as timeout, not hard error.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded or nil, got %v", err)
	}
	if !res.TimedOut {
		t.Fatal("expected timed out flag")
	}
}
