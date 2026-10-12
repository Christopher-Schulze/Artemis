package cdpops

import (
	"context"
	"errors"
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

// NavigationResponse is the typed result returned by Page.navigate.
type NavigationResponse struct {
	FrameID   string `json:"frameId"`
	LoaderID  string `json:"loaderId"`
	ErrorText string `json:"errorText"`
}

// NavigatorCaller preserves page-level policy while still allowing the
// navigator to use the common Caller contract for every other CDP method.
type NavigatorCaller interface {
	Caller
	Navigate(context.Context, string, string, *NavigationResponse) error
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
	caller     Caller
}

// NewNavigator creates a new Navigator
// (spec L4019: page navigation + wait).
func NewNavigator(callers ...Caller) *Navigator {
	var caller Caller
	if len(callers) > 0 {
		caller = callers[0]
	}
	return &Navigator{state: NavigationStateIdle, caller: caller}
}

// Navigate navigates to a URL
// (spec L4019: page navigation + wait).
func (n *Navigator) Navigate(ctx context.Context, req NavigationRequest) NavigationResult {
	start := time.Now()
	if ctx == nil {
		return NavigationResult{Success: false, State: NavigationStateError, Error: "navigation: context required", Duration: time.Since(start)}
	}
	if req.URL == "" {
		return NavigationResult{
			Success: false, State: NavigationStateError,
			Error: "navigation: empty URL", Duration: time.Since(start),
		}
	}
	if req.WaitUntil == "" {
		req.WaitUntil = WaitLoad
	}
	if !IsValidWaitCondition(req.WaitUntil) {
		return NavigationResult{Success: false, State: NavigationStateError, Error: fmt.Sprintf("navigation: invalid wait condition %q", req.WaitUntil), Duration: time.Since(start)}
	}
	if err := n.requireCaller(); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	n.mu.Lock()
	n.state = NavigationStateLoading
	n.mu.Unlock()
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var response NavigationResponse
	var err error
	if navigatorCaller, ok := n.caller.(NavigatorCaller); ok {
		err = navigatorCaller.Navigate(callCtx, req.URL, req.Referer, &response)
	} else {
		err = n.caller.Call(callCtx, "Page.navigate", navigateParams{URL: req.URL, Referrer: req.Referer}, &response)
	}
	if err == nil && response.ErrorText != "" {
		err = fmt.Errorf("navigation: %s", response.ErrorText)
	}
	if err == nil {
		err = n.waitForCondition(callCtx, req.WaitUntil)
	}
	if err != nil {
		n.setError(err)
		state := NavigationStateError
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			state = NavigationStateAborted
		}
		return NavigationResult{Success: false, URL: req.URL, State: state, Error: err.Error(), Duration: time.Since(start)}
	}
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
	if ctx == nil {
		return errors.New("wait: context required")
	}
	if !IsValidWaitCondition(condition) {
		return fmt.Errorf("wait: invalid condition %q", condition)
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if err := n.requireCaller(); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := n.waitForCondition(waitCtx, condition); err != nil {
		n.setError(err)
		return err
	}
	n.mu.Lock()
	n.state = NavigationStateComplete
	n.mu.Unlock()
	return nil
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
	return n.navigateHistory(ctx, -1)
}

// GoForward navigates forward in history
// (spec L4019: page navigation + wait).
func (n *Navigator) GoForward(ctx context.Context) NavigationResult {
	return n.navigateHistory(ctx, 1)
}

// Reload reloads the current page
// (spec L4019: page navigation + wait).
func (n *Navigator) Reload(ctx context.Context) NavigationResult {
	start := time.Now()
	if ctx == nil {
		return NavigationResult{Success: false, State: NavigationStateError, Error: "reload: context required", Duration: time.Since(start)}
	}
	if err := n.requireCaller(); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	n.mu.Lock()
	n.state = NavigationStateLoading
	currentURL := n.currentURL
	n.mu.Unlock()
	if err := n.caller.Call(ctx, "Page.reload", reloadParams{IgnoreCache: false}, &emptyResult{}); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, URL: currentURL, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	if err := n.waitForCondition(ctx, WaitLoad); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, URL: currentURL, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	n.mu.Lock()
	n.state = NavigationStateComplete
	n.lastNav = time.Now()
	n.mu.Unlock()
	return NavigationResult{Success: true, URL: currentURL, State: NavigationStateComplete, Duration: time.Since(start)}
}

func (n *Navigator) navigateHistory(ctx context.Context, delta int) NavigationResult {
	start := time.Now()
	if ctx == nil {
		return NavigationResult{Success: false, State: NavigationStateError, Error: "history: context required", Duration: time.Since(start)}
	}
	if err := n.requireCaller(); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	var history navigationHistoryResult
	if err := n.caller.Call(ctx, "Page.getNavigationHistory", navigationHistoryParams{}, &history); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	index := history.CurrentIndex + delta
	if index < 0 || index >= len(history.Entries) {
		err := fmt.Errorf("history: entry unavailable")
		n.setError(err)
		return NavigationResult{Success: false, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	n.mu.Lock()
	n.state = NavigationStateLoading
	n.mu.Unlock()
	entry := history.Entries[index]
	if err := n.caller.Call(ctx, "Page.navigateToHistoryEntry", navigateToHistoryEntryParams{EntryID: entry.ID}, &emptyResult{}); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, URL: entry.URL, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	if err := n.waitForCondition(ctx, WaitLoad); err != nil {
		n.setError(err)
		return NavigationResult{Success: false, URL: entry.URL, State: NavigationStateError, Error: err.Error(), Duration: time.Since(start)}
	}
	n.mu.Lock()
	n.state = NavigationStateComplete
	n.currentURL = entry.URL
	n.lastNav = time.Now()
	n.mu.Unlock()
	return NavigationResult{Success: true, URL: entry.URL, State: NavigationStateComplete, Duration: time.Since(start)}
}

func (n *Navigator) waitForCondition(ctx context.Context, condition WaitCondition) error {
	if ctx == nil {
		return errors.New("wait: context required")
	}
	for {
		ready, err := n.documentReady(ctx, condition)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (n *Navigator) documentReady(ctx context.Context, condition WaitCondition) (bool, error) {
	params := runtimeEvaluateParams{Expression: "document.readyState", ReturnByValue: true}
	var result readyStateResult
	if err := n.caller.Call(ctx, "Runtime.evaluate", params, &result); err != nil {
		return false, fmt.Errorf("wait: ready-state probe: %w", err)
	}
	switch condition {
	case WaitDOMContentLoaded:
		return result.Result.Value == "interactive" || result.Result.Value == "complete", nil
	case WaitLoad, WaitNetworkIdle, WaitNetworkAlmostIdle:
		return result.Result.Value == "complete", nil
	default:
		return false, fmt.Errorf("wait: invalid condition %q", condition)
	}
}

func (n *Navigator) requireCaller() error {
	n.mu.RLock()
	caller := n.caller
	n.mu.RUnlock()
	if caller == nil {
		return ErrCallerRequired
	}
	return nil
}

func (n *Navigator) setError(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.state = NavigationStateError
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		n.state = NavigationStateAborted
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
