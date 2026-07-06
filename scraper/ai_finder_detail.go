package scraper

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ai_finder_detail.go (spec L4398: AI Finder Stage 2 full detail).
//
// Stage 2 AI element finding sends a page snapshot (AX tree or HTML snippet)
// to the LLM via the Inference Hub (ss7). Privacy Routing (ss7.7) determines
// whether the request goes to LOCAL inference only (customer page with PII)
// or to an external hosted API (general page, policy/budget
// permitting).
//
// Max 3 LLM attempts with varied formulations. Vision mode: screenshot ->
// LLM -> coordinates. Text mode: HTML -> LLM -> CSS selector.

// FinderMode determines whether the AI Finder uses text (HTML snippet) or
// vision (screenshot) mode for LLM analysis.
type FinderMode string

const (
	// FinderModeText sends an HTML snippet to the LLM and expects a CSS/XPath
	// selector back (spec L4398: text mode).
	FinderModeText FinderMode = "text"
	// FinderModeVision sends a screenshot to the LLM and expects coordinates
	// back (spec L4398: vision mode).
	FinderModeVision FinderMode = "vision"
)

// PrivacyRoute determines where the LLM inference happens (ss7.7).
type PrivacyRoute string

const (
	// PrivacyRouteLocal forces LOCAL inference only (customer data, PII).
	PrivacyRouteLocal PrivacyRoute = "local_only"
	// PrivacyRouteExternal allows external hosted inference.
	PrivacyRouteExternal PrivacyRoute = "external_api"
)

// InferenceHubLLMRequest is the request sent to the Inference Hub for
// AI Finder LLM analysis (ss7 integration).
type InferenceHubLLMRequest struct {
	Mode      FinderMode `json:"mode"`
	Content   string     `json:"content"`    // HTML snippet (text mode) or screenshot base64 (vision mode)
	Intent    string     `json:"intent"`     // natural-language description of the target element
	Attempt   int        `json:"attempt"`    // 1-based attempt number for varied formulations
	LocalOnly bool       `json:"local_only"` // privacy routing: force local inference
}

// InferenceHubLLMResponse is the Inference Hub's response.
type InferenceHubLLMResponse struct {
	Selector    string  `json:"selector"`    // CSS/XPath selector (text mode)
	Coordinates string  `json:"coordinates"` // "x,y" coordinates (vision mode)
	Confidence  float64 `json:"confidence"`
	Model       string  `json:"model"`
	Local       bool    `json:"local"`
	Error       string  `json:"error,omitempty"`
}

// InferenceHubLLM is the interface for the Inference Hub LLM endpoint.
// The real implementation is provided by the embedding host.
type InferenceHubLLM interface {
	AnalyzePage(ctx context.Context, req InferenceHubLLMRequest) (InferenceHubLLMResponse, error)
}

// PrivacyRouter determines whether a URL/page should use local-only
// inference (ss7.7). The real implementation lives in artemis/bridge.
type PrivacyRouter interface {
	// ShouldUseLocalOnly returns true if the URL contains customer data
	// and inference must be LOCAL only.
	ShouldUseLocalOnly(url string) bool
}

// AIFinderStage2Config configures the Stage 2 AI Finder.
type AIFinderStage2Config struct {
	MaxAttempts     int           // default 3 (spec L4398: max 3 LLM attempts)
	Mode            FinderMode    // text or vision
	Timeout         time.Duration // per-attempt LLM timeout
	CacheConfidence float64       // minimum confidence to cache a selector
}

// DefaultAIFinderStage2Config returns spec-compliant defaults.
func DefaultAIFinderStage2Config() AIFinderStage2Config {
	return AIFinderStage2Config{
		MaxAttempts:     3,
		Mode:            FinderModeText,
		Timeout:         30 * time.Second,
		CacheConfidence: 0.70,
	}
}

// AIFinderStage2 is the full Stage 2 AI Finder with Inference Hub + Privacy
// Routing integration (spec L4398).
type AIFinderStage2 struct {
	mu     sync.Mutex
	hub    InferenceHubLLM
	router PrivacyRouter
	cache  *AdaptiveSelectorCache
	config AIFinderStage2Config
	stats  AIFinderStage2Stats
}

// AIFinderStage2Stats tracks Stage 2 AI Finder outcomes.
type AIFinderStage2Stats struct {
	TotalAttempts  int `json:"total_attempts"`
	Successful     int `json:"successful"`
	Failed         int `json:"failed"`
	CacheHits      int `json:"cache_hits"`
	LocalUsed      int `json:"local_used"`
	ExternalUsed   int `json:"external_used"`
	VisionModeUsed int `json:"vision_mode_used"`
	TextModeUsed   int `json:"text_mode_used"`
}

// NewAIFinderStage2 creates a Stage 2 AI Finder with the given Inference Hub
// and Privacy Router.
func NewAIFinderStage2(hub InferenceHubLLM, router PrivacyRouter, cache *AdaptiveSelectorCache, config AIFinderStage2Config) *AIFinderStage2 {
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 3
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.CacheConfidence <= 0 {
		config.CacheConfidence = 0.70
	}
	return &AIFinderStage2{
		hub:    hub,
		router: router,
		cache:  cache,
		config: config,
	}
}

// FindStage2 resolves an element intent via cache, then up to MaxAttempts
// LLM calls with varied formulations, applying privacy routing per call.
// domain and urlPattern are used for cache lookup/store. pageURL is used
// for privacy routing decisions. content is the HTML snippet (text mode)
// or screenshot base64 (vision mode).
func (a *AIFinderStage2) FindStage2(ctx context.Context, domain, urlPattern, pageURL, intent, content string) (AIFinderStage2Result, error) {
	if a == nil {
		return AIFinderStage2Result{}, fmt.Errorf("ai finder stage2: nil receiver")
	}
	if a.hub == nil {
		return AIFinderStage2Result{}, fmt.Errorf("ai finder stage2: no inference hub")
	}

	result := AIFinderStage2Result{
		Mode:  a.config.Mode,
		Route: PrivacyRouteExternal,
	}

	// Step 1: Check cache.
	if a.cache != nil {
		if entry, ok := a.cache.Get(domain, urlPattern); ok && entry.Selector != "" {
			a.mu.Lock()
			a.stats.CacheHits++
			a.mu.Unlock()
			result.Selector = entry.Selector
			result.Confidence = entry.Confidence
			result.FromCache = true
			result.AttemptsUsed = 0
			return result, nil
		}
	}

	// Step 2: Privacy routing — determine if local-only inference is required.
	localOnly := false
	if a.router != nil {
		localOnly = a.router.ShouldUseLocalOnly(pageURL)
	}
	if localOnly {
		result.Route = PrivacyRouteLocal
	}

	// Step 3: Up to MaxAttempts LLM calls with varied formulations.
	maxAttempts := a.config.MaxAttempts
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		a.mu.Lock()
		a.stats.TotalAttempts++
		if a.config.Mode == FinderModeVision {
			a.stats.VisionModeUsed++
		} else {
			a.stats.TextModeUsed++
		}
		if localOnly {
			a.stats.LocalUsed++
		} else {
			a.stats.ExternalUsed++
		}
		a.mu.Unlock()

		// Vary the formulation on each attempt (spec L4398: varied formulations).
		formulatedIntent := varyFormulation(intent, attempt)

		req := InferenceHubLLMRequest{
			Mode:      a.config.Mode,
			Content:   content,
			Intent:    formulatedIntent,
			Attempt:   attempt,
			LocalOnly: localOnly,
		}

		// Per-attempt timeout.
		attemptCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
		resp, err := a.hub.AnalyzePage(attemptCtx, req)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("ai finder stage2: attempt %d: %w", attempt, err)
			continue
		}
		if resp.Error != "" {
			lastErr = fmt.Errorf("ai finder stage2: attempt %d: hub error: %s", attempt, resp.Error)
			continue
		}

		selector := strings.TrimSpace(resp.Selector)
		if a.config.Mode == FinderModeVision {
			selector = strings.TrimSpace(resp.Coordinates)
		}
		if selector == "" {
			lastErr = fmt.Errorf("ai finder stage2: attempt %d: empty result", attempt)
			continue
		}

		result.Selector = selector
		result.Confidence = resp.Confidence
		result.AttemptsUsed = attempt
		result.Model = resp.Model

		// Cache if confidence meets threshold.
		if a.cache != nil && resp.Confidence >= a.config.CacheConfidence && a.config.Mode == FinderModeText {
			_ = a.cache.Put(AdaptiveEntry{
				Domain:     domain,
				URLPattern: urlPattern,
				Selector:   selector,
				Confidence: resp.Confidence,
				UpdatedAt:  time.Now(),
			})
		}

		a.mu.Lock()
		a.stats.Successful++
		a.mu.Unlock()
		return result, nil
	}

	a.mu.Lock()
	a.stats.Failed++
	a.mu.Unlock()

	if lastErr == nil {
		lastErr = fmt.Errorf("ai finder stage2: exhausted %d attempts", maxAttempts)
	}
	return result, lastErr
}

// AIFinderStage2Result is the outcome of a Stage 2 AI Finder call.
type AIFinderStage2Result struct {
	Selector     string       `json:"selector"`
	Confidence   float64      `json:"confidence"`
	AttemptsUsed int          `json:"attempts_used"`
	Mode         FinderMode   `json:"mode"`
	Route        PrivacyRoute `json:"route"`
	FromCache    bool         `json:"from_cache"`
	Model        string       `json:"model,omitempty"`
}

// Stats returns a copy of the current Stage 2 stats.
func (a *AIFinderStage2) Stats() AIFinderStage2Stats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stats
}

// SetMode changes the finder mode (text or vision).
func (a *AIFinderStage2) SetMode(mode FinderMode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config.Mode = mode
}

// varyFormulation produces a varied natural-language formulation for each
// attempt to help the LLM approach the problem from different angles
// (spec L4398: max 3 LLM attempts with varied formulations).
func varyFormulation(intent string, attempt int) string {
	switch attempt {
	case 1:
		return intent
	case 2:
		return fmt.Sprintf("Find the element that represents: %s. Consider alternative CSS selectors or XPath expressions.", intent)
	case 3:
		return fmt.Sprintf("Identify the DOM element matching this description: %s. Try a different selector strategy — look for nearby labels, aria attributes, or structural patterns.", intent)
	default:
		return fmt.Sprintf("%s (attempt %d)", intent, attempt)
	}
}
