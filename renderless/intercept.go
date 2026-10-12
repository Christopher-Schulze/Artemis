package renderless

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// intercept.go (spec L4022: renderless/intercept.go - fetch/XHR
// bridge + OnRequest mock/intercept/cache hook).
//
// In-process no-render JS browser path: fetch/XHR bridge with
// OnRequest mock/intercept/cache hook. Intercepts network requests
// from the JS runtime and routes them through configurable handlers.

// InterceptAction enumerates actions for intercepted requests
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
type InterceptAction string

const (
	InterceptActionContinue InterceptAction = "continue"
	InterceptActionMock     InterceptAction = "mock"
	InterceptActionBlock    InterceptAction = "block"
	InterceptActionCache    InterceptAction = "cache"
)

// InterceptedRequest represents an intercepted network request
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
type InterceptedRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body,omitempty"`
}

// InterceptedResponse represents a mocked response
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
type InterceptedResponse struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
}

// InterceptRule defines a rule for intercepting requests
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
type InterceptRule struct {
	Pattern  string               `json:"pattern"` // URL pattern to match
	Action   InterceptAction      `json:"action"`
	Response *InterceptedResponse `json:"response,omitempty"` // for mock
	MaxAge   time.Duration        `json:"maxAge,omitempty"`   // for cache
}

// InterceptHandler manages request interception
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
type InterceptHandler struct {
	mu            sync.RWMutex
	rules         []InterceptRule
	cache         map[string]*cacheEntry
	mockResponses map[string]*InterceptedResponse
}

type cacheEntry struct {
	response *InterceptedResponse
	cachedAt time.Time
	maxAge   time.Duration
}

// NewInterceptHandler creates a new InterceptHandler
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func NewInterceptHandler() *InterceptHandler {
	return &InterceptHandler{
		cache:         make(map[string]*cacheEntry),
		mockResponses: make(map[string]*InterceptedResponse),
	}
}

// AddRule adds an interception rule
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (h *InterceptHandler) AddRule(rule InterceptRule) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rules = append(h.rules, rule)
	if rule.Action == InterceptActionMock && rule.Response != nil {
		h.mockResponses[rule.Pattern] = rule.Response
	}
}

// Intercept processes an intercepted request
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (h *InterceptHandler) Intercept(req InterceptedRequest) (*InterceptedResponse, InterceptAction, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, rule := range h.rules {
		if matchURL(req.URL, rule.Pattern) {
			switch rule.Action {
			case InterceptActionBlock:
				return nil, InterceptActionBlock, fmt.Errorf("intercept: blocked %s", req.URL)
			case InterceptActionMock:
				if resp, ok := h.mockResponses[rule.Pattern]; ok {
					return resp, InterceptActionMock, nil
				}
			case InterceptActionCache:
				if entry, ok := h.cache[req.URL]; ok {
					if time.Since(entry.cachedAt) < entry.maxAge {
						return entry.response, InterceptActionCache, nil
					}
				}
			case InterceptActionContinue:
				return nil, InterceptActionContinue, nil
			}
		}
	}
	return nil, InterceptActionContinue, nil
}

// StoreCache stores a response in the cache
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (h *InterceptHandler) StoreCache(url string, resp *InterceptedResponse, maxAge time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache[url] = &cacheEntry{
		response: resp,
		cachedAt: time.Now(),
		maxAge:   maxAge,
	}
}

// ClearCache clears the response cache
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (h *InterceptHandler) ClearCache() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := len(h.cache)
	h.cache = make(map[string]*cacheEntry)
	return count
}

// RuleCount returns the number of interception rules
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (h *InterceptHandler) RuleCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rules)
}

// CacheCount returns the number of cached responses
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (h *InterceptHandler) CacheCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.cache)
}

// IsValidInterceptAction reports whether an action is valid
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func IsValidInterceptAction(a InterceptAction) bool {
	switch a {
	case InterceptActionContinue, InterceptActionMock, InterceptActionBlock, InterceptActionCache:
		return true
	}
	return false
}

// matchURL checks if a URL matches a pattern (simple prefix/contains match)
func matchURL(url, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return strings.Contains(url, pattern)
}

// NewMockResponse creates a new mock response
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func NewMockResponse(status int, body string) *InterceptedResponse {
	return &InterceptedResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(body),
	}
}

// ToHTTPResponse converts an InterceptedResponse to an http.Response
// (spec L4022: fetch/XHR bridge + OnRequest mock/intercept/cache).
func (r *InterceptedResponse) ToHTTPResponse() *http.Response {
	header := make(http.Header)
	for k, v := range r.Headers {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode: r.StatusCode,
		Header:     header,
		Body:       http.NoBody,
	}
}

// String returns a diagnostic summary.
func (r InterceptedRequest) String() string {
	return fmt.Sprintf("InterceptedRequest{method:%s url:%s}", r.Method, r.URL)
}

// String returns a diagnostic summary.
func (r InterceptedResponse) String() string {
	return fmt.Sprintf("InterceptedResponse{status:%d bodyLen:%d}", r.StatusCode, len(r.Body))
}
