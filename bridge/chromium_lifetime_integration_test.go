package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumOwnedCloseCancelsQueuedWorkAndJoinsEveryCaller(t *testing.T) {
	for _, kind := range []string{"page", "context", "browser"} {
		t.Run(kind, func(t *testing.T) {
			var hits, handlers atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/held/") {
					hits.Add(1)
				}
				if _, err := io.WriteString(w, "<!doctype html><title>owned worker</title>"); err != nil {
					t.Errorf("fixture: %v", err)
				}
			}))
			defer server.Close()
			browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, server.URL)}})
			owner, err := browser.NewContext(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			page, err := owner.NewPage(t.Context(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			siblingOwner := owner
			if kind == "context" {
				siblingOwner, err = browser.NewContext(t.Context())
				if err != nil {
					t.Fatal(err)
				}
			}
			sibling, err := siblingOwner.NewPage(t.Context(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			entered, cancelled := make(chan struct{}, policyRequestWorkers), make(chan struct{}, policyRequestWorkers)
			release := make(chan struct{})
			var unblock sync.Once
			defer unblock.Do(func() { close(release) })
			if err := page.SetFetchRequestHandler(func(ctx context.Context, request FetchRequest, _ FetchRequestResolver) (bool, error) {
				if !strings.Contains(request.URL, "/held/") {
					return false, nil
				}
				handlers.Add(1)
				entered <- struct{}{}
				<-ctx.Done()
				cancelled <- struct{}{}
				<-release
				return true, ctx.Err()
			}); err != nil {
				t.Fatal(err)
			}
			events, err := page.SubscribeBrowserEvents(256)
			if err != nil {
				t.Fatal(err)
			}
			defer events.Close()
			if err := page.Call(t.Context(), "Runtime.evaluate", map[string]any{"expression": "for(let i=0;i<20;i++)fetch('/held/'+i).catch(()=>{});"}, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			for paused := 0; paused < 20; {
				select {
				case event, ok := <-events.Events:
					if !ok {
						t.Fatal("native event subscription closed before queued requests")
					}
					if event.Method == "Fetch.requestPaused" {
						var request fetchRequestPaused
						if err := json.Unmarshal(event.Params, &request); err != nil {
							t.Fatal(err)
						}
						if strings.Contains(request.Request.URL, "/held/") {
							paused++
						}
					}
				case <-ctx.Done():
					t.Fatalf("native request queue did not fill: %v", ctx.Err())
				}
			}
			for range policyRequestWorkers {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("bounded workers did not enter actual callbacks")
				}
			}
			page.frameMu.RLock()
			done := page.frameDone
			page.frameMu.RUnlock()
			if done == nil {
				t.Fatal("native frame monitor has no completion owner")
			}
			closeOwner, closeAgain := page.Close, func() error { return page.close(false) }
			if kind == "context" {
				closeOwner, closeAgain = owner.Close, func() error { return owner.close(true) }
			}
			if kind == "browser" {
				closeOwner, closeAgain = browser.Close, browser.Close
			}
			results := make(chan error, 2)
			go func() { results <- closeOwner() }()
			for range policyRequestWorkers {
				select {
				case <-cancelled:
				case err := <-results:
					t.Fatalf("close returned before callback cancellation/join: %v", err)
				case <-ctx.Done():
					t.Fatal("native owner did not cancel active callbacks")
				}
			}
			go func() { results <- closeAgain() }()
			select {
			case err := <-results:
				t.Fatalf("concurrent close bypassed unfinished worker join: %v", err)
			case <-time.After(40 * time.Millisecond):
			}
			unblock.Do(func() { close(release) })
			for range 2 {
				select {
				case err := <-results:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(12 * time.Second):
					t.Fatal("close did not join released callbacks")
				}
			}
			select {
			case <-done:
			default:
				t.Fatal("close returned with its monitor/workers running")
			}
			if handlers.Load() != policyRequestWorkers || hits.Load() != 0 {
				t.Fatalf("retired queue emitted work: handlers=%d hits=%d", handlers.Load(), hits.Load())
			}
			if page.fetchRequestHandler() != nil {
				t.Fatal("close retained its application handler")
			}
			var retired *TargetError
			if err := page.SetFetchRequestHandler(nil); !errors.As(err, &retired) || retired.State != TargetStateClosed {
				t.Fatalf("closed page admitted handler replacement: %v", err)
			}
			if kind != "browser" {
				value, err := nativeFetchValue(t.Context(), sibling, "document.title")
				if err != nil || value != "owned worker" || !browser.Healthy() {
					t.Fatalf("retired owner damaged sibling: title=%q error=%v healthy=%t", value, err, browser.Healthy())
				}
			}
		})
	}
}

func TestChromiumFailedPageStartupTransfersAndReleasesOneSlot(t *testing.T) {
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	if err := browser.SetMaxPages(2); err != nil {
		t.Fatal(err)
	}
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	creator, creatorCancel := context.WithCancel(t.Context())
	retained, err := owner.NewPage(creator, "about:blank")
	creatorCancel()
	if err != nil {
		t.Fatal(err)
	}
	if failed, err := owner.NewPage(t.Context(), "http://127.0.0.1/denied"); failed != nil || !errors.Is(err, network.ErrPolicyDenied) {
		t.Fatalf("expected failed creation: page=%v error=%v", failed, err)
	}
	browser.mu.RLock()
	slots, pages := browser.pageSlots, len(browser.pages)
	browser.mu.RUnlock()
	if slots != 1 || pages != 1 {
		t.Fatalf("failed startup released another page's slot: slots=%d pages=%d", slots, pages)
	}
	if err := browser.SetMaxPages(1); err != nil {
		t.Fatal(err)
	}
	if extra, err := owner.NewPage(t.Context(), "about:blank"); extra != nil || !IsCDPError(err, CDPErrorOverloaded) {
		t.Fatalf("retained tab did not hold its slot: page=%v error=%v", extra, err)
	}
	if value, err := nativeFetchValue(t.Context(), retained, "location.href"); err != nil || value != "about:blank" {
		t.Fatalf("retained page is unusable: url=%q error=%v", value, err)
	}
	retained.frameMu.RLock()
	done := retained.frameDone
	retained.frameMu.RUnlock()
	select {
	case <-done:
		t.Fatal("completed creator cancellation retired a healthy page monitor")
	default:
	}
	if err := retained.Close(); err != nil {
		t.Fatal(err)
	}
	browser.mu.RLock()
	slots = browser.pageSlots
	browser.mu.RUnlock()
	if slots != 0 {
		t.Fatalf("closed retained page still holds %d slots", slots)
	}
}

func TestChromiumNativeRetirementAndTransportLossCancelOwnedMonitor(t *testing.T) {
	for _, kind := range []string{"native root close", "transport loss"} {
		t.Run(kind, func(t *testing.T) {
			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/held" {
					hits.Add(1)
				}
				if _, err := io.WriteString(w, "<!doctype html><title>retirement</title>"); err != nil {
					t.Errorf("fixture: %v", err)
				}
			}))
			defer server.Close()
			binary := requireChromium(t)
			browser, err := LaunchChromium(context.Background(), browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second, AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, server.URL)}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := browser.Close(); err != nil && (kind != "transport loss" || !IsCDPError(err, CDPErrorClosed)) {
					t.Errorf("close retired browser: %v", err)
				}
			})
			owner, err := browser.NewContext(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			page, err := owner.NewPage(t.Context(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			entered, cancelled := make(chan struct{}), make(chan struct{})
			if err := page.SetFetchRequestHandler(func(ctx context.Context, request FetchRequest, _ FetchRequestResolver) (bool, error) {
				if !strings.HasSuffix(request.URL, "/held") {
					return false, nil
				}
				close(entered)
				<-ctx.Done()
				close(cancelled)
				return true, ctx.Err()
			}); err != nil {
				t.Fatal(err)
			}
			if err := page.Call(t.Context(), "Runtime.evaluate", map[string]any{"expression": "fetch('/held').catch(()=>{});"}, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("actual held request did not enter")
			}
			page.frameMu.RLock()
			done := page.frameDone
			page.frameMu.RUnlock()
			if kind == "transport loss" {
				if err := browser.Transport().Close(); err != nil {
					t.Fatal(err)
				}
			} else if err := browser.Transport().Call(t.Context(), "Target.closeTarget", targetParams{TargetID: page.TargetID()}, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cancelled:
			case <-ctx.Done():
				t.Fatal("native retirement left callback running")
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("native retirement left monitor/workers running")
			}
			if hits.Load() != 0 {
				t.Fatalf("retired request reached server %d times", hits.Load())
			}
			if kind == "native root close" {
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for page.State() != TargetStateClosed {
					select {
					case <-ticker.C:
					case <-ctx.Done():
						t.Fatalf("native target destruction was not reflected: %s", page.State())
					}
				}
				if page.State() != TargetStateClosed || !browser.Healthy() {
					t.Fatalf("wrong retirement state: page=%s healthy=%t", page.State(), browser.Healthy())
				}
			} else if browser.Healthy() {
				t.Fatal("lost transport remained healthy")
			}
		})
	}
}

func TestChromiumRetiredStartupCannotPublishMonitorOrLeakSubscription(t *testing.T) {
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := browser.reservePage(); err != nil {
		t.Fatal(err)
	}
	target, err := owner.createTarget(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	session, err := owner.attachTarget(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.registerPage(target, session, TargetScriptConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	browser.transport.mu.Lock()
	before := len(browser.transport.subs)
	browser.transport.mu.Unlock()
	var retired *TargetError
	if err := page.enableFrameRouting(t.Context()); !errors.As(err, &retired) || retired.State != TargetStateClosed {
		t.Fatalf("retired startup was not rejected at its owner: %v", err)
	}
	browser.transport.mu.Lock()
	after := len(browser.transport.subs)
	browser.transport.mu.Unlock()
	page.frameMu.RLock()
	done, sub := page.frameDone, page.frameSub
	page.frameMu.RUnlock()
	if before != after || done != nil || sub != nil {
		t.Fatalf("retired startup leaked/published monitor state: subscriptions=%d/%d done=%t sub=%t", before, after, done != nil, sub != nil)
	}
	if !browser.Healthy() {
		t.Fatal("rejected startup damaged the live browser")
	}
}

func TestChromiumLivePageLimitTightensAdmissionWithoutRestart(t *testing.T) {
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	if err := browser.SetMaxPages(3); err != nil {
		t.Fatal(err)
	}
	var live atomic.Int64
	live.Store(1)
	browser.SetLivePageLimit(func() int { return int(live.Load()) })
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if extra, err := owner.NewPage(t.Context(), "about:blank"); extra != nil || !IsCDPError(err, CDPErrorOverloaded) {
		t.Fatalf("live page limit 1 not enforced: page=%v error=%v", extra, err)
	}
	if value, err := nativeFetchValue(t.Context(), first, "location.href"); err != nil || value != "about:blank" {
		t.Fatalf("refused admission disturbed the admitted page: url=%q error=%v", value, err)
	}
	live.Store(10)
	second, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatalf("raised live limit not applied: %v", err)
	}
	third, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if extra, err := owner.NewPage(t.Context(), "about:blank"); extra != nil || !IsCDPError(err, CDPErrorOverloaded) {
		t.Fatalf("live value above the static bound loosened it: page=%v error=%v", extra, err)
	}
	for _, page := range []*Page{first, second, third} {
		if err := page.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChromiumInputReachesEveryPageOfAContext(t *testing.T) {
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	second, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []*Page{first, second} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		err := page.Call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 10, "y": 10, "deltaX": 0, "deltaY": 40}, &struct{}{})
		cancel()
		if err != nil {
			t.Fatalf("input to page %s did not complete: %v", page.TargetID(), err)
		}
	}
	for _, page := range []*Page{first, second} {
		if err := page.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
