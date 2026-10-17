package bridge

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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

type nativeRequestEvidence struct{ method, body, origin, approved string }
type nativeResolutionEvidence struct {
	err      error
	repeated int
}

func TestChromiumInterceptorFinalAuthorityAndOnceOnlyEffects(t *testing.T) {
	var received, deniedHits atomic.Int64
	observed := make(chan nativeRequestEvidence, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/intercept" && r.URL.Path != "/received" {
			w.Header().Set("Content-Type", "text/html")
			if _, err := io.WriteString(w, "<!doctype html><body>network authority</body>"); err != nil {
				t.Errorf("initial page: %v", err)
			}
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("request body: %v", err)
			return
		}
		received.Add(1)
		observed <- nativeRequestEvidence{r.Method, string(body), r.Header.Get("X-Origin"), r.Header.Get("X-Approved")}
		if _, err := io.WriteString(w, "native-wire"); err != nil {
			t.Errorf("native response: %v", err)
		}
	}))
	defer server.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deniedHits.Add(1)
		if _, err := io.WriteString(w, "forbidden-wire"); err != nil {
			t.Errorf("denied response: %v", err)
		}
	}))
	defer denied.Close()
	var compressed bytes.Buffer
	compressor := gzip.NewWriter(&compressed)
	if _, err := compressor.Write([]byte(strings.Repeat("x", 512))); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	policy := network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, server.URL)}, MaxRequestBodyBytes: 8, MaxResponseBodyBytes: 128}
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{NetworkPolicy: &policy})
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name            string
		resolution      FetchResolution
		deny            bool
		get, concurrent bool
		want            string
	}{
		{"frozen original", FetchResolution{Kind: FetchResolutionContinue}, false, false, false, "native-wire"},
		{"allowed rewritten bytes", FetchResolution{Kind: FetchResolutionContinue, URL: server.URL + "/received", Method: "POST", Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}, {Name: "X-Approved", Value: "yes"}}, PostData: base64.StdEncoding.EncodeToString([]byte("éé"))}, false, false, false, "native-wire"},
		{"explicit empty headers", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{}}, false, true, false, "native-wire"},
		{"forbidden destination", FetchResolution{Kind: FetchResolutionContinue, URL: denied.URL}, true, false, false, "denied"},
		{"forbidden method", FetchResolution{Kind: FetchResolutionContinue, Method: "TRACE"}, true, false, false, "denied"},
		{"oversized body", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}}, PostData: base64.StdEncoding.EncodeToString([]byte("ééééé"))}, true, false, false, "denied"},
		{"body framing", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}, {Name: "Content-Length", Value: "1"}}}, true, false, false, "denied"},
		{"borrowed Host", FetchResolution{Kind: FetchResolutionContinue, Headers: []FetchHeader{{Name: "Host", Value: "other.example"}}}, true, false, false, "denied"},
		{"base64 input", FetchResolution{Kind: FetchResolutionContinue, PostData: "not-base64"}, true, false, false, "denied"},
		{"bounded fulfillment", FetchResolution{Kind: FetchResolutionFulfill, Body: "local-native", ResponseHeaders: []FetchHeader{{Name: "Content-Type", Value: "text/plain"}}}, false, false, false, "local-native"},
		{"oversized fulfillment", FetchResolution{Kind: FetchResolutionFulfill, Body: strings.Repeat("x", 129)}, true, false, false, "denied"},
		{"expanded fulfillment", FetchResolution{Kind: FetchResolutionFulfill, Body: compressed.String(), ResponseHeaders: []FetchHeader{{Name: "Content-Encoding", Value: "gzip"}}}, true, false, false, "denied"},
		{"concurrent terminal callers", FetchResolution{Kind: FetchResolutionContinue}, false, false, true, "native-wire"},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := owner.NewPage(t.Context(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := page.Close(); err != nil {
					t.Errorf("close test page: %v; browser cause: %v", err, browser.Err())
				}
			})
			results := make(chan nativeResolutionEvidence, 1)
			if err := page.SetFetchRequestHandler(func(ctx context.Context, request FetchRequest, resolve FetchRequestResolver) (bool, error) {
				if !strings.HasSuffix(request.URL, "/intercept") {
					return false, nil
				}
				request.URL = denied.URL
				request.Method = "DELETE"
				request.SessionID = "foreign"
				request.RequestID = "foreign"
				request.Headers["Content-Type"] = "application/octet-stream"
				results <- exerciseNativeResolvers(ctx, resolve, test.resolution, test.concurrent)
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			before := received.Load()
			options := `{method:'POST',headers:{'Content-Type':'text/plain','X-Origin':'original'},body:'orig'}`
			if test.get {
				options = `{headers:{'X-Origin':'original'}}`
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			result, err := nativeFetchValue(ctx, page, "fetch('/intercept',"+options+").then(r=>r.text()).catch(()=> 'denied')")
			if err != nil || result != test.want {
				t.Fatalf("actual native result=%q error=%v want=%q", result, err, test.want)
			}
			var evidence nativeResolutionEvidence
			select {
			case evidence = <-results:
			case <-ctx.Done():
				t.Fatal("interceptor did not resolve actual request")
			}
			if errors.Is(evidence.err, network.ErrPolicyDenied) != test.deny || evidence.err != nil && !test.deny {
				t.Fatalf("callback policy denial=%t error=%v", test.deny, evidence.err)
			}
			wantRequests := int64(0)
			if !test.deny && test.resolution.Kind == FetchResolutionContinue {
				wantRequests = 1
			}
			if actual := received.Load() - before; actual != wantRequests || deniedHits.Load() != 0 {
				t.Fatalf("actual request counts allowed=%d want=%d denied=%d", actual, wantRequests, deniedHits.Load())
			}
			if wantRequests == 1 {
				observedRequest := <-observed
				if test.name == "allowed rewritten bytes" && (observedRequest.method != "POST" || observedRequest.body != "éé" || observedRequest.approved != "yes") {
					t.Fatalf("rewritten wire differs: %+v", observedRequest)
				}
				if test.name == "frozen original" && (observedRequest.method != "POST" || observedRequest.body != "orig" || observedRequest.origin != "original") {
					t.Fatalf("source mutated: %+v", observedRequest)
				}
				if test.get && (observedRequest.origin != "" || observedRequest.body != "" || observedRequest.method != "GET") {
					t.Fatalf("explicit header clear omitted: %+v", observedRequest)
				}
			}
			if test.concurrent && evidence.repeated != 7 || !test.concurrent && evidence.repeated != 1 {
				t.Fatalf("once-only terminal receipt: %+v", evidence)
			}
			if !browser.Healthy() {
				t.Fatalf("expected denial poisoned browser: %v", browser.Err())
			}
		})
	}
}

func exerciseNativeResolvers(ctx context.Context, resolve FetchRequestResolver, resolution FetchResolution, concurrent bool) nativeResolutionEvidence {
	if !concurrent {
		err := resolve(ctx, resolution)
		repeat := resolve(ctx, resolution)
		count := 0
		if errors.Is(repeat, ErrFetchAlreadyResolved) {
			count = 1
		}
		return nativeResolutionEvidence{err, count}
	}
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() { defer workers.Done(); results <- resolve(ctx, resolution) }()
	}
	workers.Wait()
	close(results)
	var evidence nativeResolutionEvidence
	for err := range results {
		if errors.Is(err, ErrFetchAlreadyResolved) {
			evidence.repeated++
			continue
		}
		evidence.err = errors.Join(evidence.err, err)
	}
	return evidence
}

func nativeFetchValue(ctx context.Context, page *Page, expression string) (string, error) {
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}, &result); err != nil {
		return "", err
	}
	if result.ExceptionDetails != nil {
		return "", fmt.Errorf("native fetch expression: %s", result.ExceptionDetails.Text)
	}
	return result.Result.Value, nil
}

func TestChromiumInterceptorLateResolverCannotBorrowClosedPage(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/intercept" {
			hits.Add(1)
		}
		if _, err := io.WriteString(w, "<!doctype html><body>late resolver</body>"); err != nil {
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
	resolvers := make(chan FetchRequestResolver, 1)
	if err := page.SetFetchRequestHandler(func(_ context.Context, request FetchRequest, resolve FetchRequestResolver) (bool, error) {
		if !strings.HasSuffix(request.URL, "/intercept") {
			return false, nil
		}
		resolvers <- resolve
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Call(t.Context(), "Runtime.evaluate", map[string]any{"expression": "fetch('/intercept').catch(()=>{})"}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var resolve FetchRequestResolver
	select {
	case resolve = <-resolvers:
	case <-ctx.Done():
		t.Fatal("request was not paused")
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	var targetErr *TargetError
	if err := resolve(ctx, FetchResolution{Kind: FetchResolutionContinue}); !errors.As(err, &targetErr) || targetErr.State != TargetStateClosed {
		t.Fatalf("closed owner admitted late resolution: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("closed page request reached fixture %d times", hits.Load())
	}
}

func TestChromiumInterceptorCancelledAdmissionStillFailsPausedRequest(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/intercept" {
			hits.Add(1)
		}
		if _, err := io.WriteString(w, "<!doctype html><body>cancelled admission</body>"); err != nil {
			t.Errorf("fixture: %v", err)
		}
	}))
	defer server.Close()
	var callbackMu sync.Mutex
	var activeResolve FetchRequestResolver
	var activeCancel context.CancelFunc
	reentrant := make(chan error, 1)
	sink := func(decision network.Decision) error {
		if decision.Host != "127.0.0.2" || decision.Kind != network.TargetSubresource {
			return nil
		}
		callbackMu.Lock()
		resolve, cancel := activeResolve, activeCancel
		callbackMu.Unlock()
		if resolve == nil || cancel == nil {
			return errors.New("active callback absent")
		}
		reentrant <- resolve(t.Context(), FetchResolution{Kind: FetchResolutionContinue})
		cancel()
		return nil
	}
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, server.URL)}, PolicyDecisionSink: sink})
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	if err := page.SetFetchRequestHandler(func(ctx context.Context, request FetchRequest, resolve FetchRequestResolver) (bool, error) {
		if !strings.HasSuffix(request.URL, "/intercept") {
			return false, nil
		}
		operationCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		callbackMu.Lock()
		activeResolve, activeCancel = resolve, cancel
		callbackMu.Unlock()
		results <- resolve(operationCtx, FetchResolution{Kind: FetchResolutionContinue, URL: strings.Replace(server.URL, "127.0.0.1", "127.0.0.2", 1)})
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	value, err := nativeFetchValue(ctx, page, "fetch('/intercept').then(r=>r.text()).catch(()=> 'denied')")
	if err != nil || value != "denied" {
		t.Fatalf("cancelled request remained paused or dispatched: value=%q error=%v", value, err)
	}
	select {
	case err := <-results:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, network.ErrPolicyDenied) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("cancellation resolver did not return")
	}
	select {
	case err := <-reentrant:
		if !errors.Is(err, ErrFetchAlreadyResolved) {
			t.Fatalf("external decision reentered terminal attempt: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("decision callback blocked under resolver lock")
	}
	if hits.Load() != 0 || !browser.Healthy() {
		t.Fatalf("cancelled cleanup changed effect/health: hits=%d error=%v", hits.Load(), browser.Err())
	}
}

func TestChromiumRetiredNetworkOwnerKeepsOtherPageUsable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, "<!doctype html><title>still live</title><body>owned page</body>"); err != nil {
			t.Errorf("fixture: %v", err)
		}
	}))
	defer server.Close()
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, server.URL)}})
	liveOwner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	livePage, err := liveOwner.NewPage(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name         string
		closeContext bool
	}{{"page close", false}, {"context close", true}} {
		t.Run(test.name, func(t *testing.T) {
			retiredOwner, err := browser.NewContext(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			retiredPage, err := retiredOwner.NewPage(t.Context(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			if test.closeContext {
				err = retiredOwner.Close()
			} else {
				err = retiredPage.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			_, lateErr := retiredPage.NetworkPolicy()
			var targetErr *TargetError
			if !errors.As(lateErr, &targetErr) || targetErr.State != TargetStateClosed {
				t.Fatalf("retired owner error=%v", lateErr)
			}
			retiredPage.failNetworkEnforcement(lateErr)
			value, err := nativeFetchValue(t.Context(), livePage, "document.title")
			if err != nil || value != "still live" || !browser.Healthy() {
				t.Fatalf("retired owner poisoned live page: value=%q error=%v cause=%v", value, err, browser.Err())
			}
		})
	}
}
