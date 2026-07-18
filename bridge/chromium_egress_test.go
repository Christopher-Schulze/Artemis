package bridge

import (
	"context"
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
	if _, _, err := page.Navigate(nil, "https://example.com"); !IsCDPError(err, CDPErrorInvalidConfig) {
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
}
