package renderless

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func closeRenderlessTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("close %s: %v", label, err)
	}
}

func renderlessFixture(t *testing.T, body string) (*Engine, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Errorf("write renderless fixture response: %v", err)
		}
	}))
	parsed, err := url.Parse(server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("parse fixture URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		server.Close()
		t.Fatalf("fixture port: %v", err)
	}
	engine, err := NewEngine(EngineConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}, FetchTimeout: time.Second})
	if err != nil {
		server.Close()
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close fixture engine: %v", err)
		}
		server.Close()
	})
	return engine, server
}

// ==================== engine.go tests ====================

// TestNewEngine verifies engine creation
// (V8/v8go isolate snapshot engine).
func TestNewEngine(t *testing.T) {
	e, err := NewEngine(EngineConfig{MaxIsolates: 2})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e == nil {
		t.Fatal("engine should not be nil")
	}
	defer closeRenderlessTestResource(t, "engine", e.Close)
}

// TestEngineConfigDefaults verifies defaults
// (V8/v8go isolate snapshot engine).
func TestEngineConfigDefaults(t *testing.T) {
	cfg := EngineConfig{}
	cfg.ApplyDefaults()
	if cfg.MaxIsolates != 4 {
		t.Error("default max isolates should be 4")
	}
	if cfg.ScriptTimeout != 30*time.Second {
		t.Error("default script timeout should be 30s")
	}
}

// TestEngineFetch verifies fetch
// (V8/v8go isolate snapshot engine).
func TestEngineFetch(t *testing.T) {
	e, server := renderlessFixture(t, "<!doctype html><title>fixture</title><p>real</p>")
	page, err := e.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeRenderlessTestResource(t, "page", page.Close)
	if page.URL != server.URL {
		t.Error("URL mismatch")
	}
	if !strings.Contains(string(page.RawBody), "real") {
		t.Error("fetch did not retain the real response body")
	}
}

func TestEngineFetchExecutesAgainstProductionPage(t *testing.T) {
	e, server := renderlessFixture(t, "<!doctype html><body><p id='value'>before</p><script>document.getElementById('value').textContent = 'after'</script></body>")
	page, err := e.FetchWithOptions(context.Background(), server.URL, FetchOptions{RunScripts: true})
	if err != nil {
		t.Fatalf("FetchWithOptions: %v", err)
	}
	defer closeRenderlessTestResource(t, "page", page.Close)
	if !strings.Contains(page.RealPage().Text(), "after") {
		t.Fatalf("production page did not reflect script mutation: %q", page.RealPage().Text())
	}
}

// TestEngineFetchEmpty verifies empty URL fails.
func TestEngineFetchEmpty(t *testing.T) {
	e, err := NewEngine(EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer closeRenderlessTestResource(t, "engine", e.Close)
	_, err = e.Fetch(context.Background(), "")
	if err == nil {
		t.Error("empty URL should error")
	}
}

// TestEngineClose verifies close.
func TestEngineClose(t *testing.T) {
	e, err := NewEngine(EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if closeErr := e.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	if !e.IsClosed() {
		t.Error("should be closed")
	}
	_, err = e.Fetch(context.Background(), "https://example.com")
	if err == nil {
		t.Error("closed engine should error on fetch")
	}
}

// ==================== runtime.go tests ====================

// TestRuntimeContext verifies context creation
// (runtime context management).
func TestRuntimeContext(t *testing.T) {
	ctx := NewRuntimeContext("ctx-1", 1)
	if ctx.ID() != "ctx-1" {
		t.Error("ID mismatch")
	}
	if ctx.IsolateID() != 1 {
		t.Error("isolate ID mismatch")
	}
}

// TestRuntimeContextGlobals verifies global management
// (runtime context management).
func TestRuntimeContextGlobals(t *testing.T) {
	ctx := NewRuntimeContext("ctx-1", 1)
	ctx.SetGlobal("foo", "bar")
	val, ok := ctx.GetGlobal("foo")
	if !ok || val != "bar" {
		t.Error("global retrieval failed")
	}
	_, ok = ctx.GetGlobal("nonexistent")
	if ok {
		t.Error("nonexistent should not be found")
	}
}

// TestRuntimeContextClose verifies close.
func TestRuntimeContextClose(t *testing.T) {
	ctx := NewRuntimeContext("ctx-1", 1)
	ctx.Close()
	if !ctx.IsClosed() {
		t.Error("should be closed")
	}
}

// ==================== context.go tests ====================

// TestContextPoolAcquireRelease verifies pool operations
// (context pool/warm pool).
func TestContextPoolAcquireRelease(t *testing.T) {
	pool := NewContextPool(4)
	ctx := pool.Acquire()
	if ctx == nil {
		t.Fatal("Acquire should return a context")
	}
	if pool.InUse() != 1 {
		t.Error("inUse should be 1")
	}
	pool.Release(ctx)
	if pool.Available() != 1 {
		t.Error("available should be 1")
	}
}

// TestContextPoolWarm verifies warming
// (context pool/warm pool).
func TestContextPoolWarm(t *testing.T) {
	pool := NewContextPool(4)
	pool.Warm(2)
	if pool.Available() != 2 {
		t.Errorf("available: got %d, want 2", pool.Available())
	}
}

// TestContextPoolMaxSize verifies max size limit.
func TestContextPoolMaxSize(t *testing.T) {
	pool := NewContextPool(2)
	pool.Warm(2)
	ctx := pool.Acquire()
	if ctx == nil {
		t.Fatal("should acquire from warm pool")
	}
	// Pool now has 1 available, 1 in use. Acquire another.
	ctx2 := pool.Acquire()
	if ctx2 == nil {
		t.Fatal("should acquire second context")
	}
	// Pool is now exhausted.
	ctx3 := pool.Acquire()
	if ctx3 != nil {
		t.Error("exhausted pool should return nil")
	}
	pool.Release(ctx)
	pool.Release(ctx2)
}

// TestContextPoolClose verifies close.
func TestContextPoolClose(t *testing.T) {
	pool := NewContextPool(4)
	pool.Warm(2)
	pool.Close()
	if pool.Available() != 0 {
		t.Error("available should be 0 after close")
	}
}

// ==================== pool.go tests ====================

// TestIsolatePool verifies isolate pool
// (context pool management).
func TestIsolatePool(t *testing.T) {
	p := NewIsolatePool(2)
	id1, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if p.InUseCount() != 1 {
		t.Error("inUse should be 1")
	}
	p.Release(id1)
	if p.AvailableCount() != 1 {
		t.Error("available should be 1")
	}
}

// TestIsolatePoolExhausted verifies exhaustion.
func TestIsolatePoolExhausted(t *testing.T) {
	p := NewIsolatePool(1)
	if _, err := p.Acquire(); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	_, err := p.Acquire()
	if err == nil {
		t.Error("exhausted pool should error")
	}
}

// ==================== webapi.go tests ====================

// TestWebAPIRegistry verifies registry
// (DOM/WebAPI globals).
func TestWebAPIRegistry(t *testing.T) {
	r := NewWebAPIRegistry()
	if r.Count() == 0 {
		t.Error("registry should have standard globals")
	}
	if !r.IsImplemented("window") {
		t.Error("window should be implemented")
	}
}

// TestWebAPIRegistryRegister verifies registration.
func TestWebAPIRegistryRegister(t *testing.T) {
	r := NewWebAPIRegistry()
	r.Register(WebAPIGlobal{Name: "fetch", Type: "function", Implemented: true})
	if !r.IsImplemented("fetch") {
		t.Error("actual fetch binding should be implemented")
	}
}

func TestWebAPIRegistryPreservesGlobalKinds(t *testing.T) {
	r := NewWebAPIRegistry()
	for _, test := range []struct {
		name string
		kind string
	}{
		{name: "fetch", kind: "function"},
		{name: "URL", kind: "constructor"},
		{name: "window", kind: "object"},
	} {
		global, ok := r.Get(test.name)
		if !ok || global.Type != test.kind {
			t.Fatalf("global %q = %+v, want kind %q", test.name, global, test.kind)
		}
	}
}

// TestWebAPIRegistryNames verifies names.
func TestWebAPIRegistryNames(t *testing.T) {
	r := NewWebAPIRegistry()
	names := r.Names()
	if len(names) == 0 {
		t.Error("names should not be empty")
	}
}

// TestWebAPIRegistryFormatGlobals verifies format.
func TestWebAPIRegistryFormatGlobals(t *testing.T) {
	r := NewWebAPIRegistry()
	s := r.FormatGlobals()
	if s == "" {
		t.Error("format should not be empty")
	}
}

// ==================== page.go tests ====================

// TestPage verifies page creation
// (shared Page extraction surface).
func TestPage(t *testing.T) {
	p := NewPage("https://example.com", 200, []byte("hello"))
	if p.URL != "https://example.com" {
		t.Error("URL mismatch")
	}
	if p.ContentLength() != 5 {
		t.Error("content length should be 5")
	}
}

// TestPageStatusChecks verifies status checks
// (shared Page extraction surface).
func TestPageStatusChecks(t *testing.T) {
	p := NewPage("url", 200, nil)
	if !p.IsSuccess() {
		t.Error("200 should be success")
	}
	p.StatusCode = 301
	if !p.IsRedirect() {
		t.Error("301 should be redirect")
	}
	p.StatusCode = 404
	if !p.IsError() {
		t.Error("404 should be error")
	}
}

// TestPageHeaders verifies header management
// (shared Page extraction surface).
func TestPageHeaders(t *testing.T) {
	p := NewPage("url", 200, nil)
	p.SetHeader("Content-Type", "text/html")
	if p.GetHeader("Content-Type") != "text/html" {
		t.Error("header mismatch")
	}
	if !p.IsHTML() {
		t.Error("should be HTML")
	}
}

// ==================== router.go tests ====================

// TestScriptRouterInline verifies inline execution
// (inline+external script execution).
func TestScriptRouterInline(t *testing.T) {
	r := NewScriptRouter()
	result := r.Execute(ScriptRequest{Type: ScriptTypeInline, Source: "code"})
	if result.Success || result.Error == "" {
		t.Error("inline without a production page executor must fail closed")
	}
}

// TestScriptRouterExternal verifies external execution
// (inline+external script execution).
func TestScriptRouterExternal(t *testing.T) {
	r := NewScriptRouter()
	result := r.Execute(ScriptRequest{Type: ScriptTypeExternal, Source: "https://example.com/script.js"})
	if result.Success || result.Error == "" {
		t.Error("external without a production page executor must fail closed")
	}
}

func TestScriptRouterAllowsCallerBackedHandler(t *testing.T) {
	r := NewScriptRouter()
	r.RegisterHandler(ScriptTypeInline, func(req ScriptRequest) ScriptResult {
		return ScriptResult{Success: true, Output: req.Source}
	})
	result := r.Execute(ScriptRequest{Type: ScriptTypeInline, Source: "real"})
	if !result.Success || result.Output != "real" {
		t.Fatalf("caller-backed handler result=%+v", result)
	}
}

func TestScriptRouterNilHandlerFailsClosed(t *testing.T) {
	r := NewScriptRouter()
	r.RegisterHandler(ScriptTypeInline, nil)
	result := r.Execute(ScriptRequest{Type: ScriptTypeInline, Source: "real"})
	if result.Success || result.Error == "" {
		t.Fatalf("nil handler must fail closed: %+v", result)
	}
}

// TestScriptRouterEmptySource verifies empty source fails.
func TestScriptRouterEmptySource(t *testing.T) {
	r := NewScriptRouter()
	result := r.Execute(ScriptRequest{Type: ScriptTypeInline, Source: ""})
	if result.Success {
		t.Error("empty source should fail")
	}
}

// TestIsValidScriptType verifies validation.
func TestIsValidScriptType(t *testing.T) {
	if !IsValidScriptType(ScriptTypeInline) {
		t.Error("inline should be valid")
	}
	if IsValidScriptType(ScriptType("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== intercept.go tests ====================

// TestInterceptHandlerContinue verifies continue
// (fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestInterceptHandlerContinue(t *testing.T) {
	h := NewInterceptHandler()
	_, action, err := h.Intercept(InterceptedRequest{URL: "https://example.com"})
	if err != nil {
		t.Errorf("continue should not error: %v", err)
	}
	if action != InterceptActionContinue {
		t.Error("action should be continue")
	}
}

// TestInterceptHandlerBlock verifies block
// (fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestInterceptHandlerBlock(t *testing.T) {
	h := NewInterceptHandler()
	h.AddRule(InterceptRule{Pattern: "blocked.com", Action: InterceptActionBlock})
	_, _, err := h.Intercept(InterceptedRequest{URL: "https://blocked.com"})
	if err == nil {
		t.Error("blocked should error")
	}
}

// TestInterceptHandlerMock verifies mock
// (fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestInterceptHandlerMock(t *testing.T) {
	h := NewInterceptHandler()
	h.AddRule(InterceptRule{
		Pattern:  "api.example.com",
		Action:   InterceptActionMock,
		Response: NewMockResponse(200, `{"ok":true}`),
	})
	resp, action, err := h.Intercept(InterceptedRequest{URL: "https://api.example.com/data"})
	if err != nil {
		t.Errorf("mock should not error: %v", err)
	}
	if action != InterceptActionMock {
		t.Error("action should be mock")
	}
	if resp == nil || resp.StatusCode != 200 {
		t.Error("mock response should be 200")
	}
}

// TestInterceptHandlerCache verifies cache
// (fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestInterceptHandlerCache(t *testing.T) {
	h := NewInterceptHandler()
	h.StoreCache("https://example.com", NewMockResponse(200, "cached"), 1*time.Hour)
	if h.CacheCount() != 1 {
		t.Error("cache count should be 1")
	}
	h.ClearCache()
	if h.CacheCount() != 0 {
		t.Error("cache should be cleared")
	}
}

// TestIsValidInterceptAction verifies validation.
func TestIsValidInterceptAction(t *testing.T) {
	if !IsValidInterceptAction(InterceptActionContinue) {
		t.Error("continue should be valid")
	}
	if IsValidInterceptAction(InterceptAction("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== css.go tests ====================

// TestCSSParser verifies CSS parsing
// (CSS parse/cascade/computed style).
func TestCSSParser(t *testing.T) {
	p := NewCSSParser()
	rules, err := p.Parse("div { color: red; } .class { font-size: 14px; }")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Errorf("rules: got %d, want 2", len(rules))
	}
	if rules[0].SelectorRaw != "div" {
		t.Error("first selector should be div")
	}
}

// TestCSSParserEmpty verifies empty CSS.
func TestCSSParserEmpty(t *testing.T) {
	p := NewCSSParser()
	rules, err := p.Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Error("empty CSS should produce 0 rules")
	}
}

// TestComputedStyle verifies computed style
// (CSS parse/cascade/computed style).
func TestComputedStyle(t *testing.T) {
	rules, err := NewCSSParser().Parse(`div{color:red}.class{color:blue}.class-other{color:green}`)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parser.ParseHTML(strings.NewReader(`<div class="class" id="target"></div>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	node, err := doc.QuerySelector("#target")
	if err != nil || node == nil {
		t.Fatalf("actual DOM node: %v", err)
	}
	cs := NewComputedStyle(rules)
	value, err := cs.Compute(node.Raw(), "", "color")
	if err != nil || value != "rgb(0, 0, 255)" {
		t.Fatalf("actual DOM specificity must win without substring matches: %q %v", value, err)
	}
}

// ==================== capabilities.go tests ====================

// TestCapabilityProfile verifies profile generation
// (generated RenderlessCapabilityProfile).
func TestCapabilityProfile(t *testing.T) {
	cfg := EngineConfig{EnableRobots: true}
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(cfg, registry)
	if profile.Version == "" {
		t.Error("version should not be empty")
	}
	if !profile.FetchSupport {
		t.Error("fetch should be supported")
	}
}

// TestCapabilityProfileSupports verifies support checks
// (generated RenderlessCapabilityProfile).
func TestCapabilityProfileSupports(t *testing.T) {
	profile := RenderlessCapabilityProfile{
		WebAPIs:     []string{"fetch", "document"},
		CSSSupport:  []string{"selectors"},
		ScriptTypes: []string{"inline"},
	}
	if !profile.SupportsWebAPI("fetch") {
		t.Error("should support fetch")
	}
	if !profile.SupportsCSS("selectors") {
		t.Error("should support selectors")
	}
	if !profile.SupportsScriptType("inline") {
		t.Error("should support inline")
	}
}

// TestCapabilityProfileIsCapable verifies capability check.
func TestCapabilityProfileIsCapable(t *testing.T) {
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if !profile.IsCapable() {
		t.Error("profile should be capable")
	}
}

// TestCapabilityProfileMissingWebAPIs verifies missing check.
func TestCapabilityProfileMissingWebAPIs(t *testing.T) {
	profile := RenderlessCapabilityProfile{WebAPIs: []string{"fetch"}}
	missing := profile.MissingWebAPIs([]string{"fetch", "nonexistent"})
	if len(missing) != 1 {
		t.Errorf("missing: got %d, want 1", len(missing))
	}
}

// ==================== parity test ====================

// TestRenderlessParity verifies all 11 spec-mandated files
// (engine.go, runtime.go, context.go, pool.go, webapi.go,
// page.go, router.go, intercept.go, css.go, capabilities.go,
// conformance_test.go).
func TestRenderlessParity(t *testing.T) {
	// 1. engine.go
	e, err := NewEngine(EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer closeRenderlessTestResource(t, "engine", e.Close)

	// 2. runtime.go
	ctx := NewRuntimeContext("ctx-1", 1)
	ctx.SetGlobal("test", true)

	// 3. context.go
	pool := NewContextPool(4)
	pool.Warm(2)

	// 4. pool.go
	isolates := NewIsolatePool(1)
	if _, err := isolates.Acquire(); err != nil {
		t.Fatalf("acquire isolate slot: %v", err)
	}

	// 5. webapi.go
	registry := NewWebAPIRegistry()

	// 6. page.go
	page := NewPage("https://example.com", 200, []byte("hello"))

	// 7. router.go
	router := NewScriptRouter()
	router.Execute(ScriptRequest{Type: ScriptTypeInline, Source: "code"})

	// 8. intercept.go
	ih := NewInterceptHandler()
	ih.AddRule(InterceptRule{Pattern: "test.com", Action: InterceptActionBlock})

	// 9. css.go
	cssParser := NewCSSParser()
	if _, err := cssParser.Parse("div { color: red; }"); err != nil {
		t.Fatal(err)
	}

	// 10. capabilities.go
	profile := GenerateCapabilityProfile(e.Config(), registry)
	if !profile.IsCapable() {
		t.Error("profile should be capable")
	}

	// 11. conformance_test.go - verified by TestConformance* functions
	_ = page
}
