package bridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumAuthChallengeUsesActual401WithoutImplicitCredentials(t *testing.T) {
	var implicitCredentials, explicitRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/authorized" && r.Header.Get("Authorization") == "Bearer explicit-fixture" {
			explicitRequests.Add(1)
			if _, err := io.WriteString(w, "explicit authorization preserved"); err != nil {
				t.Error(err)
			}
			return
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			implicitCredentials.Add(1)
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="actual-challenge"`)
		http.Error(w, "actual unauthorized body", http.StatusUnauthorized)
	}))
	defer server.Close()
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, server.URL)}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	events, err := page.SubscribeBrowserEvents(128)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	frame, loader, err := page.Navigate(ctx, server.URL+"/unauthorized")
	if err != nil {
		t.Fatal(err)
	}
	sawChallenge, sawResponse := false, false
	for !sawChallenge || !sawResponse {
		select {
		case event, ok := <-events.Events:
			if !ok {
				t.Fatal("browser event owner closed before auth evidence")
			}
			if event.SessionID != page.sessionID {
				continue
			}
			if event.Method == "Fetch.authRequired" {
				sawChallenge = true
			}
			if event.Method == "Network.responseReceived" {
				var response struct {
					FrameID  string `json:"frameId"`
					LoaderID string `json:"loaderId"`
					Type     string `json:"type"`
					Response struct {
						Status int `json:"status"`
					} `json:"response"`
				}
				if err := json.Unmarshal(event.Params, &response); err != nil {
					t.Fatal(err)
				}
				if response.FrameID == frame && response.LoaderID == loader && response.Type == "Document" && response.Response.Status == 401 {
					sawResponse = true
				}
			}
		case err, ok := <-events.Errors:
			if !ok {
				t.Fatal("auth evidence error subscription closed")
			}
			t.Fatalf("auth evidence subscription: %v", err)
		case <-ctx.Done():
			t.Fatalf("missing actual auth/401 evidence: %v", ctx.Err())
		}
	}
	if implicitCredentials.Load() != 0 {
		t.Fatal("unhandled challenge received implicit credentials")
	}
	if err := page.SetFetchRequestHandler(func(ctx context.Context, request FetchRequest, resolve FetchRequestResolver) (bool, error) {
		if !strings.HasSuffix(request.URL, "/authorized") {
			return false, nil
		}
		headers := []FetchHeader{{Name: "Authorization", Value: "Bearer explicit-fixture"}}
		for name, value := range request.Headers {
			if !strings.EqualFold(name, "Authorization") {
				headers = append(headers, FetchHeader{Name: name, Value: value})
			}
		}
		return true, resolve(ctx, FetchResolution{Kind: FetchResolutionContinue, Headers: headers})
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := page.Navigate(ctx, server.URL+"/authorized"); err != nil {
		t.Fatal(err)
	}
	if explicitRequests.Load() != 1 || !browser.Healthy() {
		t.Fatalf("explicit request or browser health failed: requests=%d err=%v", explicitRequests.Load(), browser.Err())
	}
}
