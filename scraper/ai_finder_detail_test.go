package scraper

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// mockInferenceHubLLM is a test InferenceHubLLM implementation.
type mockInferenceHubLLM struct {
	mu          sync.Mutex
	responses   []InferenceHubLLMResponse
	errors      []error
	calls       int
	lastRequest InferenceHubLLMRequest
}

func (m *mockInferenceHubLLM) AnalyzePage(ctx context.Context, req InferenceHubLLMRequest) (InferenceHubLLMResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastRequest = req
	idx := m.calls - 1
	if idx < len(m.errors) && m.errors[idx] != nil {
		return InferenceHubLLMResponse{}, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return InferenceHubLLMResponse{Selector: ".fallback", Confidence: 0.5}, nil
}

func (m *mockInferenceHubLLM) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockInferenceHubLLM) LastRequest() InferenceHubLLMRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// mockPrivacyRouter is a test PrivacyRouter implementation.
type mockPrivacyRouter struct {
	localOnlyURLs map[string]bool
}

func (m *mockPrivacyRouter) ShouldUseLocalOnly(url string) bool {
	return m.localOnlyURLs[url]
}

func TestAIFinderStage2_DefaultConfig(t *testing.T) {
	cfg := DefaultAIFinderStage2Config()
	if cfg.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.Mode != FinderModeText {
		t.Fatalf("Mode = %s, want text", cfg.Mode)
	}
	if cfg.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.CacheConfidence != 0.70 {
		t.Fatalf("CacheConfidence = %f, want 0.70", cfg.CacheConfidence)
	}
}

func TestAIFinderStage2_NewWithDefaults(t *testing.T) {
	hub := &mockInferenceHubLLM{}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, AIFinderStage2Config{})
	if f == nil {
		t.Fatal("NewAIFinderStage2 returned nil")
	}
	if f.config.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", f.config.MaxAttempts)
	}
	if f.config.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", f.config.Timeout)
	}
}

func TestAIFinderStage2_SuccessFirstAttempt(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".product-name", Confidence: 0.92, Model: "gpt-mini", Local: false},
		},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	result, err := f.FindStage2(context.Background(), "example.com", "/products", "https://example.com/products", "product name field", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	if result.Selector != ".product-name" {
		t.Fatalf("Selector = %q, want .product-name", result.Selector)
	}
	if result.AttemptsUsed != 1 {
		t.Fatalf("AttemptsUsed = %d, want 1", result.AttemptsUsed)
	}
	if result.Confidence != 0.92 {
		t.Fatalf("Confidence = %f, want 0.92", result.Confidence)
	}
	if result.FromCache {
		t.Fatal("FromCache = true, want false")
	}
	if result.Route != PrivacyRouteExternal {
		t.Fatalf("Route = %s, want external_api", result.Route)
	}
	if hub.CallCount() != 1 {
		t.Fatalf("CallCount = %d, want 1", hub.CallCount())
	}
}

func TestAIFinderStage2_SuccessSecondAttempt(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: "", Confidence: 0.1, Error: "low confidence"},
			{Selector: "div.price", Confidence: 0.85, Model: "gpt-mini"},
		},
		errors: []error{nil, nil},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	result, err := f.FindStage2(context.Background(), "shop.com", "/items", "https://shop.com/items", "price element", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	if result.Selector != "div.price" {
		t.Fatalf("Selector = %q, want div.price", result.Selector)
	}
	if result.AttemptsUsed != 2 {
		t.Fatalf("AttemptsUsed = %d, want 2", result.AttemptsUsed)
	}
}

func TestAIFinderStage2_ExhaustedAttempts(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: "", Error: "not found"},
			{Selector: "", Error: "not found"},
			{Selector: "", Error: "not found"},
		},
		errors: []error{nil, nil, nil},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	_, err := f.FindStage2(context.Background(), "site.com", "/page", "https://site.com/page", "target", "<div>...</div>")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if hub.CallCount() != 3 {
		t.Fatalf("CallCount = %d, want 3", hub.CallCount())
	}
}

func TestAIFinderStage2_PrivacyRoutingLocalOnly(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".customer-data", Confidence: 0.88, Local: true},
		},
	}
	router := &mockPrivacyRouter{
		localOnlyURLs: map[string]bool{"https://portal.internal/customer": true},
	}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	result, err := f.FindStage2(context.Background(), "portal.internal", "/customer", "https://portal.internal/customer", "customer info", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	if result.Route != PrivacyRouteLocal {
		t.Fatalf("Route = %s, want local_only", result.Route)
	}
	req := hub.LastRequest()
	if !req.LocalOnly {
		t.Fatal("LocalOnly = false, want true")
	}
}

func TestAIFinderStage2_PrivacyRoutingExternal(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".article", Confidence: 0.90, Local: false},
		},
	}
	router := &mockPrivacyRouter{
		localOnlyURLs: map[string]bool{},
	}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	result, err := f.FindStage2(context.Background(), "news.com", "/article", "https://news.com/article", "article body", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	if result.Route != PrivacyRouteExternal {
		t.Fatalf("Route = %s, want external_api", result.Route)
	}
}

func TestAIFinderStage2_VisionMode(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Coordinates: "420,180", Confidence: 0.75},
		},
	}
	router := &mockPrivacyRouter{}
	cfg := DefaultAIFinderStage2Config()
	cfg.Mode = FinderModeVision
	f := NewAIFinderStage2(hub, router, nil, cfg)

	result, err := f.FindStage2(context.Background(), "shop.com", "/checkout", "https://shop.com/checkout", "checkout button", "base64screenshot")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	if result.Selector != "420,180" {
		t.Fatalf("Selector = %q, want 420,180", result.Selector)
	}
	if result.Mode != FinderModeVision {
		t.Fatalf("Mode = %s, want vision", result.Mode)
	}
	req := hub.LastRequest()
	if req.Mode != FinderModeVision {
		t.Fatalf("request Mode = %s, want vision", req.Mode)
	}
}

func TestAIFinderStage2_CacheHit(t *testing.T) {
	cache, err := OpenAdaptiveCache(filepath.Join(t.TempDir(), "adaptive.db"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	_ = cache.Put(AdaptiveEntry{
		Domain:     "cached.com",
		URLPattern: "/cached",
		Selector:   ".cached-selector",
		Confidence: 0.95,
		UpdatedAt:  time.Now(),
	})
	hub := &mockInferenceHubLLM{}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, cache, DefaultAIFinderStage2Config())

	result, err := f.FindStage2(context.Background(), "cached.com", "/cached", "https://cached.com/cached", "target", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	if !result.FromCache {
		t.Fatal("FromCache = false, want true")
	}
	if result.Selector != ".cached-selector" {
		t.Fatalf("Selector = %q, want .cached-selector", result.Selector)
	}
	if hub.CallCount() != 0 {
		t.Fatalf("CallCount = %d, want 0 (cache hit)", hub.CallCount())
	}
}

func TestAIFinderStage2_CachesHighConfidence(t *testing.T) {
	cache, err := OpenAdaptiveCache(filepath.Join(t.TempDir(), "adaptive.db"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".high-conf", Confidence: 0.95},
		},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, cache, DefaultAIFinderStage2Config())

	_, err = f.FindStage2(context.Background(), "conf.com", "/page", "https://conf.com/page", "target", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	entry, ok := cache.Get("conf.com", "/page")
	if !ok {
		t.Fatal("selector not cached after high-confidence result")
	}
	if entry.Selector != ".high-conf" {
		t.Fatalf("cached Selector = %q, want .high-conf", entry.Selector)
	}
}

func TestAIFinderStage2_DoesNotCacheLowConfidence(t *testing.T) {
	cache, err := OpenAdaptiveCache(filepath.Join(t.TempDir(), "adaptive.db"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".low-conf", Confidence: 0.40},
		},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, cache, DefaultAIFinderStage2Config())

	_, err = f.FindStage2(context.Background(), "low.com", "/page", "https://low.com/page", "target", "<div>...</div>")
	if err != nil {
		t.Fatalf("FindStage2 error: %v", err)
	}
	_, ok := cache.Get("low.com", "/page")
	if ok {
		t.Fatal("selector should not be cached with low confidence")
	}
}

func TestAIFinderStage2_VaryFormulation(t *testing.T) {
	intent := "find the login button"
	if varyFormulation(intent, 1) != intent {
		t.Fatal("attempt 1 should return original intent")
	}
	f2 := varyFormulation(intent, 2)
	if f2 == intent {
		t.Fatal("attempt 2 should vary the formulation")
	}
	f3 := varyFormulation(intent, 3)
	if f3 == intent {
		t.Fatal("attempt 3 should vary the formulation")
	}
	if f2 == f3 {
		t.Fatal("attempts 2 and 3 should produce different formulations")
	}
}

func TestAIFinderStage2_Stats(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".ok", Confidence: 0.90},
		},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	_, _ = f.FindStage2(context.Background(), "s.com", "/p", "https://s.com/p", "target", "<div>...</div>")
	stats := f.Stats()
	if stats.TotalAttempts != 1 {
		t.Fatalf("TotalAttempts = %d, want 1", stats.TotalAttempts)
	}
	if stats.Successful != 1 {
		t.Fatalf("Successful = %d, want 1", stats.Successful)
	}
	if stats.TextModeUsed != 1 {
		t.Fatalf("TextModeUsed = %d, want 1", stats.TextModeUsed)
	}
	if stats.ExternalUsed != 1 {
		t.Fatalf("ExternalUsed = %d, want 1", stats.ExternalUsed)
	}
}

func TestAIFinderStage2_NilReceiver(t *testing.T) {
	var f *AIFinderStage2
	_, err := f.FindStage2(context.Background(), "x", "/y", "https://x/y", "z", "<div>...</div>")
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

func TestAIFinderStage2_NoInferenceHub(t *testing.T) {
	f := NewAIFinderStage2(nil, nil, nil, DefaultAIFinderStage2Config())
	_, err := f.FindStage2(context.Background(), "x", "/y", "https://x/y", "z", "<div>...</div>")
	if err == nil {
		t.Fatal("expected error for nil inference hub")
	}
}

func TestAIFinderStage2_ContextCancelled(t *testing.T) {
	hub := &mockInferenceHubLLM{
		responses: []InferenceHubLLMResponse{
			{Selector: ".ok", Confidence: 0.90},
		},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.FindStage2(ctx, "x", "/y", "https://x/y", "z", "<div>...</div>")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestAIFinderStage2_SetMode(t *testing.T) {
	hub := &mockInferenceHubLLM{}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())
	f.SetMode(FinderModeVision)
	if f.config.Mode != FinderModeVision {
		t.Fatalf("Mode = %s, want vision", f.config.Mode)
	}
}

func TestAIFinderStage2_HubError(t *testing.T) {
	hub := &mockInferenceHubLLM{
		errors: []error{fmt.Errorf("network error"), fmt.Errorf("timeout"), fmt.Errorf("server error")},
	}
	router := &mockPrivacyRouter{}
	f := NewAIFinderStage2(hub, router, nil, DefaultAIFinderStage2Config())

	_, err := f.FindStage2(context.Background(), "e.com", "/p", "https://e.com/p", "target", "<div>...</div>")
	if err == nil {
		t.Fatal("expected error after all attempts fail")
	}
	stats := f.Stats()
	if stats.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", stats.Failed)
	}
	if stats.TotalAttempts != 3 {
		t.Fatalf("TotalAttempts = %d, want 3", stats.TotalAttempts)
	}
}
