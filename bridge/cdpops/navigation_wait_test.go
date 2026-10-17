package cdpops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lifecycleTestCaller replays a scripted Page.lifecycleEvent timeline after
// each Page.navigate and records watcher ownership.
type lifecycleTestCaller struct {
	mu          sync.Mutex
	calls       []string
	events      chan LifecycleEvent
	timeline    []LifecycleEvent
	navigations atomic.Int64
	gate        chan struct{}
	watchers    atomic.Int64
	stops       atomic.Int64
}

func newLifecycleTestCaller(timeline ...LifecycleEvent) *lifecycleTestCaller {
	return &lifecycleTestCaller{events: make(chan LifecycleEvent, 32), timeline: timeline}
}

func (c *lifecycleTestCaller) Call(ctx context.Context, method string, _ any, result any) error {
	c.mu.Lock()
	c.calls = append(c.calls, method)
	c.mu.Unlock()
	var raw string
	switch method {
	case "Page.setLifecycleEventsEnabled":
		raw = `{}`
	case "Page.getFrameTree":
		raw = `{"frameTree":{"frame":{"id":"frame-1","loaderId":"loader-current"}}}`
		for _, event := range c.timeline {
			c.events <- event
		}
	case "Runtime.evaluate":
		raw = `{"result":{"value":"complete"}}`
	case "Page.navigate":
		if c.gate != nil {
			select {
			case <-c.gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		number := c.navigations.Add(1)
		raw = fmt.Sprintf(`{"frameId":"frame-1","loaderId":"loader-%d"}`, number)
		for _, event := range c.timeline {
			c.events <- event
		}
	default:
		return fmt.Errorf("unexpected CDP method %s", method)
	}
	return json.Unmarshal([]byte(raw), result)
}

func (c *lifecycleTestCaller) WatchLifecycle() (<-chan LifecycleEvent, func(), error) {
	c.watchers.Add(1)
	return c.events, func() { c.stops.Add(1) }, nil
}

func (c *lifecycleTestCaller) called(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, call := range c.calls {
		if call == method {
			count++
		}
	}
	return count
}

func lifecycle(name, loaderID string) LifecycleEvent {
	return LifecycleEvent{FrameID: "frame-1", LoaderID: loaderID, Name: name}
}

func TestNetworkWaitsRequireLifecycleEventsInsteadOfReadyState(t *testing.T) {
	for _, condition := range []WaitCondition{WaitNetworkIdle, WaitNetworkAlmostIdle} {
		caller := newCDPOpsTestCaller()
		result := NewNavigator(caller).Navigate(context.Background(), NavigationRequest{URL: "https://example.test", WaitUntil: condition, Timeout: time.Second})
		if result.Success || result.Error != ErrLifecycleEventsRequired.Error() || len(caller.calls) != 0 {
			t.Fatalf("%s collapsed to readyState or navigated without lifecycle support: %+v calls=%v", condition, result, caller.calls)
		}
		if err := NewNavigator(caller).WaitForLoad(context.Background(), condition, time.Second); !errors.Is(err, ErrLifecycleEventsRequired) {
			t.Fatalf("%s wait collapsed to readyState: %v", condition, err)
		}
	}
}

func TestNetworkWaitConditionsFollowTheirOwnLoaderLifecycle(t *testing.T) {
	// Load and almost-idle are reached, but full idle only for a stale document,
	// a foreign frame and a differently named event.
	timeline := []LifecycleEvent{
		lifecycle("load", "loader-1"),
		lifecycle("networkAlmostIdle", "loader-1"),
		lifecycle("networkIdle", "loader-0"),
		{FrameID: "child-frame", LoaderID: "loader-1", Name: "networkIdle"},
	}
	for _, tc := range []struct {
		condition WaitCondition
		success   bool
	}{{WaitNetworkAlmostIdle, true}, {WaitNetworkIdle, false}} {
		caller := newLifecycleTestCaller(timeline...)
		navigator := NewNavigator(caller)
		result := navigator.Navigate(context.Background(), NavigationRequest{URL: "https://example.test/app", WaitUntil: tc.condition, Timeout: 150 * time.Millisecond})
		if result.Success != tc.success {
			t.Fatalf("%s success=%t, want %t: %+v", tc.condition, result.Success, tc.success, result)
		}
		if !tc.success && (result.State != NavigationStateAborted || navigator.CurrentURL() != "") {
			t.Fatalf("unmet %s published completion: %+v url=%q", tc.condition, result, navigator.CurrentURL())
		}
		if caller.called("Page.setLifecycleEventsEnabled") != 1 || caller.called("Runtime.evaluate") != 0 || caller.watchers.Load() != 1 || caller.stops.Load() != 1 {
			t.Fatalf("%s did not use exactly one owned lifecycle watcher: calls=%v watchers=%d stops=%d", tc.condition, caller.calls, caller.watchers.Load(), caller.stops.Load())
		}
	}
	idle := newLifecycleTestCaller(append(timeline, lifecycle("networkIdle", "loader-1"))...)
	navigator := NewNavigator(idle)
	if result := navigator.Navigate(context.Background(), NavigationRequest{URL: "https://example.test/app", WaitUntil: WaitNetworkIdle, Timeout: time.Second}); !result.Success || navigator.CurrentURL() != "https://example.test/app" {
		t.Fatalf("matching networkIdle did not complete navigation: %+v", result)
	}
}

func TestWaitForLoadNetworkIdleUsesCurrentMainFrameDocument(t *testing.T) {
	stale := newLifecycleTestCaller(lifecycle("networkIdle", "loader-old"))
	if err := NewNavigator(stale).WaitForLoad(context.Background(), WaitNetworkIdle, 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale document idleness satisfied the current wait: %v", err)
	}
	current := newLifecycleTestCaller(lifecycle("networkIdle", "loader-current"))
	if err := NewNavigator(current).WaitForLoad(context.Background(), WaitNetworkIdle, time.Second); err != nil {
		t.Fatalf("replayed current-document idleness was missed: %v", err)
	}
	if current.called("Page.setLifecycleEventsEnabled") != 2 || current.stops.Load() != 1 {
		t.Fatalf("current-document wait did not reset/replay and release its watcher: calls=%v stops=%d", current.calls, current.stops.Load())
	}
}

func TestOverlappingNavigationsPublishTheirOwnState(t *testing.T) {
	caller := newLifecycleTestCaller()
	caller.gate = make(chan struct{})
	navigator := NewNavigator(caller)
	first := make(chan NavigationResult, 1)
	go func() {
		first <- navigator.Navigate(context.Background(), NavigationRequest{URL: "https://example.test/first", WaitUntil: WaitLoad, Timeout: 5 * time.Second})
	}()
	for caller.called("Page.navigate") == 0 {
		time.Sleep(time.Millisecond)
	}
	queued, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if result := navigator.Navigate(queued, NavigationRequest{URL: "https://example.test/cancelled", WaitUntil: WaitLoad}); result.Success || result.State != NavigationStateAborted {
		t.Fatalf("queued navigation did not stop on cancellation: %+v", result)
	}
	second := make(chan NavigationResult, 1)
	go func() {
		second <- navigator.Navigate(context.Background(), NavigationRequest{URL: "https://example.test/second", WaitUntil: WaitLoad, Timeout: 5 * time.Second})
	}()
	time.Sleep(20 * time.Millisecond)
	if calls := caller.called("Page.navigate"); calls != 1 {
		t.Fatalf("overlapping navigation started before the first finished: %d Page.navigate calls", calls)
	}
	close(caller.gate)
	if result := <-first; !result.Success || result.URL != "https://example.test/first" {
		t.Fatalf("first navigation lost its own result: %+v", result)
	}
	if result := <-second; !result.Success || result.URL != "https://example.test/second" {
		t.Fatalf("second navigation lost its own result: %+v", result)
	}
	if navigator.CurrentURL() != "https://example.test/second" || navigator.State() != NavigationStateComplete {
		t.Fatalf("navigator published stale state: url=%q state=%s", navigator.CurrentURL(), navigator.State())
	}
}
