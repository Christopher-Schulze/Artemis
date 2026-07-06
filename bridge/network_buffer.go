package bridge

import (
	"strings"
	"sync"
	"time"
)

// NetworkRequest is a single network request entry in the correlation buffer.
type NetworkRequest struct {
	ID           string
	Timestamp    time.Time
	Method       string
	URL          string
	ResourceType string
	Status       int
	OK           bool
	FailureText  string
	ResponseTime time.Duration
}

// NetworkRequestBuffer stores network request-response correlations
// per page (spec ss28.10: network_requests per page_id). Thread-safe.
type NetworkRequestBuffer struct {
	mu       sync.RWMutex
	requests map[string][]NetworkRequest // pageID -> requests
	maxPer   int
}

// NewNetworkRequestBuffer creates a buffer with the given max requests per page.
// maxPer=0 means unlimited.
func NewNetworkRequestBuffer(maxPer int) *NetworkRequestBuffer {
	return &NetworkRequestBuffer{
		requests: make(map[string][]NetworkRequest),
		maxPer:   maxPer,
	}
}

// AddRequest records a new network request for the given page.
func (b *NetworkRequestBuffer) AddRequest(pageID string, req NetworkRequest) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxPer > 0 && len(b.requests[pageID]) >= b.maxPer {
		// Drop oldest to make room
		b.requests[pageID] = b.requests[pageID][1:]
	}
	b.requests[pageID] = append(b.requests[pageID], req)
}

// AddResponse updates the response fields for a request matched by ID.
func (b *NetworkRequestBuffer) AddResponse(pageID, requestID string, status int, ok bool, failureText string, responseTime time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	reqs := b.requests[pageID]
	for i := range reqs {
		if reqs[i].ID == requestID {
			reqs[i].Status = status
			reqs[i].OK = ok
			reqs[i].FailureText = failureText
			reqs[i].ResponseTime = responseTime
			return
		}
	}
}

// GetRequests returns all requests for a page.
func (b *NetworkRequestBuffer) GetRequests(pageID string) []NetworkRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	reqs := b.requests[pageID]
	out := make([]NetworkRequest, len(reqs))
	copy(out, reqs)
	return out
}

// GetRequest returns a single request by ID.
func (b *NetworkRequestBuffer) GetRequest(pageID, requestID string) (NetworkRequest, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, req := range b.requests[pageID] {
		if req.ID == requestID {
			return req, true
		}
	}
	return NetworkRequest{}, false
}

// Clear removes all requests for a page.
func (b *NetworkRequestBuffer) Clear(pageID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.requests, pageID)
}

// ClearAll removes all requests from all pages.
func (b *NetworkRequestBuffer) ClearAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = make(map[string][]NetworkRequest)
}

// Count returns the number of requests for a page.
func (b *NetworkRequestBuffer) Count(pageID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.requests[pageID])
}

// FindByURL returns requests whose URL matches the given pattern (spec L4324:
// URL-based matching backfill). The pattern supports substring matching and
// wildcard '*' globs. Returns matches in insertion order.
func (b *NetworkRequestBuffer) FindByURL(pageID, pattern string) []NetworkRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []NetworkRequest
	for _, req := range b.requests[pageID] {
		if urlMatchesPattern(req.URL, pattern) {
			out = append(out, req)
		}
	}
	return out
}

// FindByMethod returns requests matching the given HTTP method (case-insensitive).
func (b *NetworkRequestBuffer) FindByMethod(pageID, method string) []NetworkRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []NetworkRequest
	upper := strings.ToUpper(method)
	for _, req := range b.requests[pageID] {
		if strings.ToUpper(req.Method) == upper {
			out = append(out, req)
		}
	}
	return out
}

// FindByStatusRange returns requests with status code in [min, max] inclusive.
func (b *NetworkRequestBuffer) FindByStatusRange(pageID string, min, max int) []NetworkRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []NetworkRequest
	for _, req := range b.requests[pageID] {
		if req.Status >= min && req.Status <= max {
			out = append(out, req)
		}
	}
	return out
}

// FindByResourceType returns requests matching the given resource type.
func (b *NetworkRequestBuffer) FindByResourceType(pageID, resourceType string) []NetworkRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []NetworkRequest
	for _, req := range b.requests[pageID] {
		if req.ResourceType == resourceType {
			out = append(out, req)
		}
	}
	return out
}

// FindFailed returns requests that have a non-OK status or failure text.
func (b *NetworkRequestBuffer) FindFailed(pageID string) []NetworkRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []NetworkRequest
	for _, req := range b.requests[pageID] {
		if !req.OK || req.FailureText != "" || (req.Status >= 400 && req.Status != 0) {
			out = append(out, req)
		}
	}
	return out
}

// urlMatchesPattern checks if a URL matches a pattern with '*' wildcards.
// Empty pattern matches nothing. "*" matches everything. Otherwise, the
// pattern is split on '*' and each segment must appear in order in the URL.
func urlMatchesPattern(url, pattern string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	// Simple substring match if no wildcards
	if !strings.Contains(pattern, "*") {
		return strings.Contains(url, pattern)
	}
	// Wildcard match: split on '*', each segment must appear in order
	idx := 0
	for _, seg := range strings.Split(pattern, "*") {
		if seg == "" {
			continue
		}
		pos := strings.Index(url[idx:], seg)
		if pos < 0 {
			return false
		}
		idx += pos + len(seg)
	}
	return true
}
