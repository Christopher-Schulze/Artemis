package bridge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestPolicyTargetKindClassifiesBrowserEgress(t *testing.T) {
	page := &Page{sessionID: "page", childSessions: map[string]string{"frame": "iframe", "worker": "worker"}}
	tests := []struct {
		name      string
		sessionID string
		resource  string
		redirect  string
		want      network.TargetKind
	}{
		{name: "navigation", sessionID: "page", resource: "Document", want: network.TargetNavigation},
		{name: "redirect", sessionID: "page", resource: "Document", redirect: "prior", want: network.TargetRedirect},
		{name: "subframe", sessionID: "frame", resource: "Document", want: network.TargetSubframe},
		{name: "worker target", sessionID: "worker", resource: "Fetch", want: network.TargetWorker},
		{name: "worker resource", sessionID: "page", resource: "Worker", want: network.TargetWorker},
		{name: "websocket", sessionID: "page", resource: "WebSocket", want: network.TargetWebSocket},
		{name: "subresource", sessionID: "page", resource: "Image", want: network.TargetSubresource},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paused := fetchRequestPaused{ResourceType: test.resource, RedirectedRequestID: test.redirect}
			if got := page.policyTargetKind(test.sessionID, paused); got != test.want {
				t.Fatalf("kind=%q want=%q", got, test.want)
			}
		})
	}
}

func TestNavigateRequiresContextBeforePolicyEvaluation(t *testing.T) {
	page := &Page{}
	var invalidContext context.Context
	if _, _, err := page.Navigate(invalidContext, "https://example.com"); !IsCDPError(err, CDPErrorInvalidConfig) {
		t.Fatalf("nil context error=%v", err)
	}
	if _, _, err := page.Navigate(context.Background(), ""); err == nil {
		t.Fatal("empty URL was accepted")
	}
}

func TestFetchRequestMetadataIsCaseInsensitiveAndBounded(t *testing.T) {
	headers := map[string]any{"Content-Type": "application/json", "CONTENT-LENGTH": "12"}
	if got := fetchHeader(headers, "content-type"); got != "application/json" {
		t.Fatalf("content type=%q", got)
	}
	if got := fetchContentLength(headers, "ignored", true); got != 12 {
		t.Fatalf("content length=%d", got)
	}
	if got := fetchContentLength(nil, "payload", true); got != 7 {
		t.Fatalf("post data length=%d", got)
	}
	if got := fetchContentLength(nil, "", true); got != -1 {
		t.Fatalf("unknown post data length=%d", got)
	}
}

func TestFetchResolutionCommandsUseCDPShapes(t *testing.T) {
	tests := []struct {
		name       string
		resolution FetchResolution
		method     string
		want       string
	}{
		{
			name:       "continue",
			resolution: FetchResolution{Kind: FetchResolutionContinue, URL: "https://example.com/next", Method: "POST", Headers: []FetchHeader{{Name: "X-Test", Value: "yes"}}, PostData: "body"},
			method:     "Fetch.continueRequest",
			want:       `{"requestId":"cdp-1","url":"https://example.com/next","method":"POST","headers":[{"name":"X-Test","value":"yes"}],"postData":"body"}`,
		},
		{
			name:       "fulfill encodes raw body",
			resolution: FetchResolution{Kind: FetchResolutionFulfill, StatusCode: 201, ResponseHeaders: []FetchHeader{{Name: "Content-Type", Value: "application/json"}}, Body: `{"ok":true}`},
			method:     "Fetch.fulfillRequest",
			want:       `{"requestId":"cdp-1","responseCode":201,"responseHeaders":[{"name":"Content-Type","value":"application/json"}],"body":"eyJvayI6dHJ1ZX0="}`,
		},
		{
			name:       "timeout fails",
			resolution: FetchResolution{Kind: FetchResolutionTimeout, ErrorReason: "TTL expired"},
			method:     "Fetch.failRequest",
			want:       `{"requestId":"cdp-1","errorReason":"TimedOut"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			method, params, err := fetchResolutionCommand("cdp-1", test.resolution)
			if err != nil {
				t.Fatal(err)
			}
			if method != test.method {
				t.Fatalf("method=%q want=%q", method, test.method)
			}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.want {
				t.Fatalf("params=%s want=%s", encoded, test.want)
			}
		})
	}
}
