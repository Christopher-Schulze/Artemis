package bridge

import (
	"sync"
	"time"
)

// NetworkRequest is a single network request entry in the correlation buffer.
type NetworkRequest struct {
	ID            string
	Timestamp     time.Time
	Method        string
	URL           string
	ResourceType  string
	Status        int
	OK            bool
	FailureText   string
	ResponseTime  time.Duration
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
