package renderless

import (
	"testing"
	"time"
)

// ==================== engine.go tests ====================

// TestTASK2255_NewEngine verifies engine creation
// (spec L4022: V8/v8go isolate snapshot engine).
func TestTASK2255_NewEngine(t *testing.T) {
	e, err := NewEngine(EngineConfig{MaxIsolates: 2})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e == nil {
		t.Fatal("engine should not be nil")
	}
}

// TestTASK2255_EngineConfigDefaults verifies defaults
// (spec L4022: V8/v8go isolate snapshot engine).
func TestTASK2255_EngineConfigDefaults(t *testing.T) {
	cfg := EngineConfig{}
	cfg.ApplyDefaults()
	if cfg.MaxIsolates != 4 {
		t.Error("default max isolates should be 4")
	}
	if cfg.ScriptTimeout != 30*time.Second {
		t.Error("default script timeout should be 30s")
	}
}

// TestTASK2255_EngineFetch verifies fetch
// (spec L4022: V8/v8go isolate snapshot engine).
func TestTASK2255_EngineFetch(t *testing.T) {
	e, _ := NewEngine(EngineConfig{})
	page, err := e.Fetch(nil, "https://example.com")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL != "https://example.com" {
		t.Error("URL mismatch")
	}
}

// TestTASK2255_EngineFetchEmpty verifies empty URL fails.
func TestTASK2255_EngineFetchEmpty(t *testing.T) {
	e, _ := NewEngine(EngineConfig{})
	_, err := e.Fetch(nil, "")
	if err == nil {
		t.Error("empty URL should error")
	}
}

// TestTASK2255_EngineClose verifies close.
func TestTASK2255_EngineClose(t *testing.T) {
	e, _ := NewEngine(EngineConfig{})
	e.Close()
	if !e.IsClosed() {
		t.Error("should be closed")
	}
	_, err := e.Fetch(nil, "https://example.com")
	if err == nil {
		t.Error("closed engine should error on fetch")
	}
}

// ==================== runtime.go tests ====================

// TestTASK2255_RuntimeContext verifies context creation
// (spec L4022: runtime context management).
func TestTASK2255_RuntimeContext(t *testing.T) {
	ctx := NewRuntimeContext("ctx-1", 1)
	if ctx.ID() != "ctx-1" {
		t.Error("ID mismatch")
	}
	if ctx.IsolateID() != 1 {
		t.Error("isolate ID mismatch")
	}
}

// TestTASK2255_RuntimeContextGlobals verifies global management
// (spec L4022: runtime context management).
func TestTASK2255_RuntimeContextGlobals(t *testing.T) {
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

// TestTASK2255_RuntimeContextClose verifies close.
func TestTASK2255_RuntimeContextClose(t *testing.T) {
	ctx := NewRuntimeContext("ctx-1", 1)
	ctx.Close()
	if !ctx.IsClosed() {
		t.Error("should be closed")
	}
}

// ==================== context.go tests ====================

// TestTASK2255_ContextPoolAcquireRelease verifies pool operations
// (spec L4022: context pool/warm pool).
func TestTASK2255_ContextPoolAcquireRelease(t *testing.T) {
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

// TestTASK2255_ContextPoolWarm verifies warming
// (spec L4022: context pool/warm pool).
func TestTASK2255_ContextPoolWarm(t *testing.T) {
	pool := NewContextPool(4)
	pool.Warm(2)
	if pool.Available() != 2 {
		t.Errorf("available: got %d, want 2", pool.Available())
	}
}

// TestTASK2255_ContextPoolMaxSize verifies max size limit.
func TestTASK2255_ContextPoolMaxSize(t *testing.T) {
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

// TestTASK2255_ContextPoolClose verifies close.
func TestTASK2255_ContextPoolClose(t *testing.T) {
	pool := NewContextPool(4)
	pool.Warm(2)
	pool.Close()
	if pool.Available() != 0 {
		t.Error("available should be 0 after close")
	}
}

// ==================== pool.go tests ====================

// TestTASK2255_BuilderPool verifies builder pool
// (spec L4022: context pool management).
func TestTASK2255_BuilderPool(t *testing.T) {
	p := NewBuilderPool()
	b := p.Get()
	b.WriteString("hello")
	if b.String() != "hello" {
		t.Error("string mismatch")
	}
	p.Put(b)
	b2 := p.Get()
	if b2.Len() != 0 {
		t.Error("returned builder should be reset")
	}
}

// TestTASK2255_IsolatePool verifies isolate pool
// (spec L4022: context pool management).
func TestTASK2255_IsolatePool(t *testing.T) {
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

// TestTASK2255_IsolatePoolExhausted verifies exhaustion.
func TestTASK2255_IsolatePoolExhausted(t *testing.T) {
	p := NewIsolatePool(1)
	_, _ = p.Acquire()
	_, err := p.Acquire()
	if err == nil {
		t.Error("exhausted pool should error")
	}
}

// ==================== webapi.go tests ====================

// TestTASK2255_WebAPIRegistry verifies registry
// (spec L4022: DOM/WebAPI globals).
func TestTASK2255_WebAPIRegistry(t *testing.T) {
	r := NewWebAPIRegistry()
	if r.Count() == 0 {
		t.Error("registry should have standard globals")
	}
	if !r.IsImplemented("window") {
		t.Error("window should be implemented")
	}
}

// TestTASK2255_WebAPIRegistryRegister verifies registration.
func TestTASK2255_WebAPIRegistryRegister(t *testing.T) {
	r := NewWebAPIRegistry()
	r.Register(WebAPIGlobal{Name: "customAPI", Type: "function", Implemented: true})
	if !r.IsImplemented("customAPI") {
		t.Error("customAPI should be implemented")
	}
}

// TestTASK2255_WebAPIRegistryNames verifies names.
func TestTASK2255_WebAPIRegistryNames(t *testing.T) {
	r := NewWebAPIRegistry()
	names := r.Names()
	if len(names) == 0 {
		t.Error("names should not be empty")
	}
}

// TestTASK2255_WebAPIRegistryFormatGlobals verifies format.
func TestTASK2255_WebAPIRegistryFormatGlobals(t *testing.T) {
	r := NewWebAPIRegistry()
	s := r.FormatGlobals()
	if s == "" {
		t.Error("format should not be empty")
	}
}

// ==================== page.go tests ====================

// TestTASK2255_Page verifies page creation
// (spec L4022: shared Page extraction surface).
func TestTASK2255_Page(t *testing.T) {
	p := NewPage("https://example.com", 200, []byte("hello"))
	if p.URL != "https://example.com" {
		t.Error("URL mismatch")
	}
	if p.ContentLength() != 5 {
		t.Error("content length should be 5")
	}
}

// TestTASK2255_PageStatusChecks verifies status checks
// (spec L4022: shared Page extraction surface).
func TestTASK2255_PageStatusChecks(t *testing.T) {
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

// TestTASK2255_PageHeaders verifies header management
// (spec L4022: shared Page extraction surface).
func TestTASK2255_PageHeaders(t *testing.T) {
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

// TestTASK2255_ScriptRouterInline verifies inline execution
// (spec L4022: inline+external script execution).
func TestTASK2255_ScriptRouterInline(t *testing.T) {
	r := NewScriptRouter()
	result := r.Execute(ScriptRequest{Type: ScriptTypeInline, Source: "code"})
	if !result.Success {
		t.Error("inline should succeed")
	}
}

// TestTASK2255_ScriptRouterExternal verifies external execution
// (spec L4022: inline+external script execution).
func TestTASK2255_ScriptRouterExternal(t *testing.T) {
	r := NewScriptRouter()
	result := r.Execute(ScriptRequest{Type: ScriptTypeExternal, Source: "https://example.com/script.js"})
	if !result.Success {
		t.Error("external should succeed")
	}
}

// TestTASK2255_ScriptRouterEmptySource verifies empty source fails.
func TestTASK2255_ScriptRouterEmptySource(t *testing.T) {
	r := NewScriptRouter()
	result := r.Execute(ScriptRequest{Type: ScriptTypeInline, Source: ""})
	if result.Success {
		t.Error("empty source should fail")
	}
}

// TestTASK2255_IsValidScriptType verifies validation.
func TestTASK2255_IsValidScriptType(t *testing.T) {
	if !IsValidScriptType(ScriptTypeInline) {
		t.Error("inline should be valid")
	}
	if IsValidScriptType(ScriptType("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== intercept.go tests ====================

// TestTASK2255_InterceptHandlerContinue verifies continue
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestTASK2255_InterceptHandlerContinue(t *testing.T) {
	h := NewInterceptHandler()
	_, action, err := h.Intercept(InterceptedRequest{URL: "https://example.com"})
	if err != nil {
		t.Errorf("continue should not error: %v", err)
	}
	if action != InterceptActionContinue {
		t.Error("action should be continue")
	}
}

// TestTASK2255_InterceptHandlerBlock verifies block
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestTASK2255_InterceptHandlerBlock(t *testing.T) {
	h := NewInterceptHandler()
	h.AddRule(InterceptRule{Pattern: "blocked.com", Action: InterceptActionBlock})
	_, _, err := h.Intercept(InterceptedRequest{URL: "https://blocked.com"})
	if err == nil {
		t.Error("blocked should error")
	}
}

// TestTASK2255_InterceptHandlerMock verifies mock
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestTASK2255_InterceptHandlerMock(t *testing.T) {
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

// TestTASK2255_InterceptHandlerCache verifies cache
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func TestTASK2255_InterceptHandlerCache(t *testing.T) {
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

// TestTASK2255_IsValidInterceptAction verifies validation.
func TestTASK2255_IsValidInterceptAction(t *testing.T) {
	if !IsValidInterceptAction(InterceptActionContinue) {
		t.Error("continue should be valid")
	}
	if IsValidInterceptAction(InterceptAction("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== css.go tests ====================

// TestTASK2255_CSSParser verifies CSS parsing
// (spec L4022: CSS parse/cascade/computed style).
func TestTASK2255_CSSParser(t *testing.T) {
	p := NewCSSParser()
	rules := p.Parse("div { color: red; } .class { font-size: 14px; }")
	if len(rules) != 2 {
		t.Errorf("rules: got %d, want 2", len(rules))
	}
	if rules[0].Selector != "div" {
		t.Error("first selector should be div")
	}
}

// TestTASK2255_CSSParserEmpty verifies empty CSS.
func TestTASK2255_CSSParserEmpty(t *testing.T) {
	p := NewCSSParser()
	rules := p.Parse("")
	if len(rules) != 0 {
		t.Error("empty CSS should produce 0 rules")
	}
}

// TestTASK2255_ComputedStyle verifies computed style
// (spec L4022: CSS parse/cascade/computed style).
func TestTASK2255_ComputedStyle(t *testing.T) {
	rules := []CSSRule{
		{Selector: "div", Properties: map[string]string{"color": "red"}, Specificity: 1},
		{Selector: ".class", Properties: map[string]string{"color": "blue"}, Specificity: 10},
	}
	cs := NewComputedStyle(rules)
	style := cs.Compute("div.class")
	if style["color"] != "blue" {
		t.Error("higher specificity should win")
	}
}

// ==================== capabilities.go tests ====================

// TestTASK2255_CapabilityProfile verifies profile generation
// (spec L4022: generated RenderlessCapabilityProfile).
func TestTASK2255_CapabilityProfile(t *testing.T) {
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

// TestTASK2255_CapabilityProfileSupports verifies support checks
// (spec L4022: generated RenderlessCapabilityProfile).
func TestTASK2255_CapabilityProfileSupports(t *testing.T) {
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

// TestTASK2255_CapabilityProfileIsCapable verifies capability check.
func TestTASK2255_CapabilityProfileIsCapable(t *testing.T) {
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if !profile.IsCapable() {
		t.Error("profile should be capable")
	}
}

// TestTASK2255_CapabilityProfileMissingWebAPIs verifies missing check.
func TestTASK2255_CapabilityProfileMissingWebAPIs(t *testing.T) {
	profile := RenderlessCapabilityProfile{WebAPIs: []string{"fetch"}}
	missing := profile.MissingWebAPIs([]string{"fetch", "nonexistent"})
	if len(missing) != 1 {
		t.Errorf("missing: got %d, want 1", len(missing))
	}
}

// ==================== full spec parity test ====================

// TestTASK2255_FullSpecParity verifies all 11 spec-mandated files
// (spec L4022: engine.go, runtime.go, context.go, pool.go, webapi.go,
// page.go, router.go, intercept.go, css.go, capabilities.go,
// conformance_test.go).
func TestTASK2255_FullSpecParity(t *testing.T) {
	// 1. engine.go
	e, _ := NewEngine(EngineConfig{})
	defer e.Close()

	// 2. runtime.go
	ctx := NewRuntimeContext("ctx-1", 1)
	ctx.SetGlobal("test", true)

	// 3. context.go
	pool := NewContextPool(4)
	pool.Warm(2)

	// 4. pool.go
	bp := NewBuilderPool()
	b := bp.Get()
	b.WriteString("test")
	bp.Put(b)

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
	cssParser.Parse("div { color: red; }")

	// 10. capabilities.go
	profile := GenerateCapabilityProfile(e.Config(), registry)
	if !profile.IsCapable() {
		t.Error("profile should be capable")
	}

	// 11. conformance_test.go - verified by TestConformance* functions
	_ = page
}
