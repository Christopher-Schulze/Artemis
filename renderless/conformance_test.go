package renderless

import (
	"testing"
)

// conformance_test.go (spec L4022: renderless/conformance_test.go -
// conformance tests).
//
// In-process no-render JS browser path: conformance tests that
// verify the renderless engine meets spec-mandated requirements.
// These tests are the spec-mandated conformance test file.

// TestConformanceEngineCreation verifies engine can be created
// (spec L4022: conformance test).
func TestConformanceEngineCreation(t *testing.T) {
	cfg := EngineConfig{MaxIsolates: 2}
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("conformance: engine creation: %v", err)
	}
	if e == nil {
		t.Fatal("conformance: engine should not be nil")
	}
	if e.Config().MaxIsolates != 2 {
		t.Error("conformance: max isolates should be 2")
	}
}

// TestConformanceEngineDefaults verifies default config
// (spec L4022: conformance test).
func TestConformanceEngineDefaults(t *testing.T) {
	e, _ := NewEngine(EngineConfig{})
	cfg := e.Config()
	if cfg.MaxIsolates != 4 {
		t.Error("conformance: default max isolates should be 4")
	}
	if cfg.UserAgent == "" {
		t.Error("conformance: default user agent should not be empty")
	}
}

// TestConformanceEngineClose verifies engine can be closed
// (spec L4022: conformance test).
func TestConformanceEngineClose(t *testing.T) {
	e, _ := NewEngine(EngineConfig{})
	if e.IsClosed() {
		t.Error("conformance: engine should not be closed initially")
	}
	e.Close()
	if !e.IsClosed() {
		t.Error("conformance: engine should be closed after Close()")
	}
}

// TestConformanceContextPool verifies context pool works
// (spec L4022: conformance test).
func TestConformanceContextPool(t *testing.T) {
	pool := NewContextPool(4)
	ctx := pool.Acquire()
	if ctx == nil {
		t.Fatal("conformance: Acquire should return a context")
	}
	pool.Release(ctx)
	if pool.Available() != 1 {
		t.Error("conformance: pool should have 1 available after release")
	}
}

// TestConformanceWebAPIRegistry verifies WebAPI registry
// (spec L4022: conformance test).
func TestConformanceWebAPIRegistry(t *testing.T) {
	r := NewWebAPIRegistry()
	if r.Count() == 0 {
		t.Error("conformance: registry should have standard globals")
	}
	if !r.IsImplemented("fetch") {
		t.Error("conformance: fetch should be implemented")
	}
	if !r.IsImplemented("document") {
		t.Error("conformance: document should be implemented")
	}
}

// TestConformanceScriptRouter verifies script router
// (spec L4022: conformance test).
func TestConformanceScriptRouter(t *testing.T) {
	r := NewScriptRouter()
	req := ScriptRequest{Type: ScriptTypeInline, Source: "console.log('hello')"}
	result := r.Execute(req)
	if !result.Success {
		t.Error("conformance: inline script should execute")
	}
}

// TestConformanceInterceptHandler verifies intercept handler
// (spec L4022: conformance test).
func TestConformanceInterceptHandler(t *testing.T) {
	h := NewInterceptHandler()
	h.AddRule(InterceptRule{
		Pattern: "example.com",
		Action:  InterceptActionBlock,
	})
	_, action, err := h.Intercept(InterceptedRequest{URL: "https://example.com/page"})
	if err == nil {
		t.Error("conformance: blocked request should return error")
	}
	if action != InterceptActionBlock {
		t.Error("conformance: action should be block")
	}
}

// TestConformanceCSSParser verifies CSS parser
// (spec L4022: conformance test).
func TestConformanceCSSParser(t *testing.T) {
	p := NewCSSParser()
	rules := p.Parse("div { color: red; } .class { font-size: 14px; }")
	if len(rules) != 2 {
		t.Errorf("conformance: rules: got %d, want 2", len(rules))
	}
}

// TestConformanceCapabilityProfile verifies capability profile
// (spec L4022: conformance test).
func TestConformanceCapabilityProfile(t *testing.T) {
	cfg := EngineConfig{EnableRobots: true, PrivateIPBlock: true}
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(cfg, registry)
	if profile.Version == "" {
		t.Error("conformance: profile version should not be empty")
	}
	if !profile.FetchSupport {
		t.Error("conformance: fetch should be supported")
	}
	if !profile.RobotsGuard {
		t.Error("conformance: robots guard should be enabled")
	}
}

// TestConformancePage verifies page creation
// (spec L4022: conformance test).
func TestConformancePage(t *testing.T) {
	p := NewPage("https://example.com", 200, []byte("<html></html>"))
	if p.URL != "https://example.com" {
		t.Error("conformance: URL mismatch")
	}
	if !p.IsSuccess() {
		t.Error("conformance: 200 should be success")
	}
}

// TestConformanceFullEngineFlow verifies full engine flow
// (spec L4022: conformance test).
func TestConformanceFullEngineFlow(t *testing.T) {
	// 1. Create engine
	e, _ := NewEngine(EngineConfig{})
	defer e.Close()

	// 2. Fetch a page
	page, err := e.Fetch(nil, "https://example.com")
	if err != nil {
		t.Fatalf("conformance: fetch: %v", err)
	}
	if page == nil {
		t.Fatal("conformance: page should not be nil")
	}

	// 3. Generate capability profile
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(e.Config(), registry)
	if !profile.IsCapable() {
		t.Error("conformance: profile should be capable")
	}
}
