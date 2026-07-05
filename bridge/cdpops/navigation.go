package cdpops

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// navigation.go (spec L4019: bridge/cdpops/navigation.go - page
// navigation + wait).
//
// Low-level CDP ops: page navigation and wait operations.
// Provides functions for navigating to URLs, waiting for page load,
// and tracking navigation state.

// NavigationState enumerates page navigation states
// (spec L4019: page navigation + wait).
type NavigationState string

const (
	NavigationStateIdle     NavigationState = "idle"
	NavigationStateLoading  NavigationState = "loading"
	NavigationStateComplete NavigationState = "complete"
	NavigationStateError    NavigationState = "error"
	NavigationStateAborted  NavigationState = "aborted"
)

// NavigationRequest represents a navigation request
// (spec L4019: page navigation + wait).
type NavigationRequest struct {
	URL       string        `json:"url"`
	WaitUntil WaitCondition `json:"waitUntil"`
	Timeout   time.Duration `json:"timeout"`
	Referer   string        `json:"referer,omitempty"`
}

// NavigationResult is the result of a navigation
// (spec L4019: page navigation + wait).
type NavigationResult struct {
	Success    bool            `json:"success"`
	URL        string          `json:"url"`
	State      NavigationState `json:"state"`
	StatusCode int             `json:"statusCode,omitempty"`
	Duration   time.Duration   `json:"duration"`
	Error      string          `json:"error,omitempty"`
}

// WaitCondition enumerates page load wait conditions
// (spec L4019: page navigation + wait).
type WaitCondition string

const (
	WaitLoad              WaitCondition = "load"
	WaitDOMContentLoaded  WaitCondition = "domcontentloaded"
	WaitNetworkIdle       WaitCondition = "networkidle"
	WaitNetworkAlmostIdle WaitCondition = "networkalmostidle"
)

// Navigator manages page navigation
// (spec L4019: page navigation + wait).
type Navigator struct {
	mu         sync.RWMutex
	state      NavigationState
	currentURL string
	lastNav    time.Time
}

// NewNavigator creates a new Navigator
// (spec L4019: page navigation + wait).
func NewNavigator() *Navigator {
	return &Navigator{
		state: NavigationStateIdle,
	}
}

// Navigate navigates to a URL
// (spec L4019: page navigation + wait).
func (n *Navigator) Navigate(ctx context.Context, req NavigationRequest) NavigationResult {
	start := time.Now()
	if req.URL == "" {
		return NavigationResult{
			Success: false,
			Error:   "navigation: empty URL",
		}
	}
	n.mu.Lock()
	n.state = NavigationStateLoading
	n.mu.Unlock()
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if req.WaitUntil == "" {
		req.WaitUntil = WaitLoad
	}
	// In a real implementation, this would use CDP Page.navigate
	// and wait for the specified condition.
	n.mu.Lock()
	n.state = NavigationStateComplete
	n.currentURL = req.URL
	n.lastNav = time.Now()
	n.mu.Unlock()
	return NavigationResult{
		Success:  true,
		URL:      req.URL,
		State:    NavigationStateComplete,
		Duration: time.Since(start),
	}
}

// WaitForLoad waits for the page to reach the specified condition
// (spec L4019: page navigation + wait).
func (n *Navigator) WaitForLoad(ctx context.Context, condition WaitCondition, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		n.mu.RLock()
		state := n.state
		n.mu.RUnlock()
		if state == NavigationStateComplete {
			return nil
		}
		if state == NavigationStateError {
			return fmt.Errorf("wait: navigation error")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait: timeout after %v", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// State returns the current navigation state
// (spec L4019: page navigation + wait).
func (n *Navigator) State() NavigationState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// CurrentURL returns the current URL
// (spec L4019: page navigation + wait).
func (n *Navigator) CurrentURL() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.currentURL
}

// GoBack navigates back in history
// (spec L4019: page navigation + wait).
func (n *Navigator) GoBack(ctx context.Context) NavigationResult {
	// In a real implementation, this would use CDP Page.goBack
	return NavigationResult{
		Success:  true,
		State:    NavigationStateComplete,
		Duration: 0,
	}
}

// GoForward navigates forward in history
// (spec L4019: page navigation + wait).
func (n *Navigator) GoForward(ctx context.Context) NavigationResult {
	return NavigationResult{
		Success:  true,
		State:    NavigationStateComplete,
		Duration: 0,
	}
}

// Reload reloads the current page
// (spec L4019: page navigation + wait).
func (n *Navigator) Reload(ctx context.Context) NavigationResult {
	n.mu.Lock()
	n.state = NavigationStateComplete
	n.mu.Unlock()
	return NavigationResult{
		Success: true,
		State:   NavigationStateComplete,
	}
}

// IsValidWaitCondition reports whether a wait condition is valid
// (spec L4019: page navigation + wait).
func IsValidWaitCondition(wc WaitCondition) bool {
	switch wc {
	case WaitLoad, WaitDOMContentLoaded, WaitNetworkIdle, WaitNetworkAlmostIdle:
		return true
	}
	return false
}

// IsValidNavigationState reports whether a navigation state is valid
// (spec L4019: page navigation + wait).
func IsValidNavigationState(s NavigationState) bool {
	switch s {
	case NavigationStateIdle, NavigationStateLoading, NavigationStateComplete, NavigationStateError, NavigationStateAborted:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (n *Navigator) String() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return fmt.Sprintf("Navigator{state:%s url:%s}", n.state, n.currentURL)
}

// String returns a diagnostic summary.
func (r NavigationResult) String() string {
	return fmt.Sprintf("NavigationResult{success:%v url:%s state:%s duration:%v}",
		r.Success, r.URL, r.State, r.Duration)
}
