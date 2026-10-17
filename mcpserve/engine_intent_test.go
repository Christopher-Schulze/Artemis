package mcpserve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Christopher-Schulze/Artemis/playwright"
)

// TestMCPEngineIntentIsTypedForExternalAgents proves the agent-facing MCP
// contract: the capabilities tool reports the Playwright lane truthfully and
// Playwright-only requests fail capability_unavailable without opening a page
// through another engine, while Artemis intent opens normally.
func TestMCPEngineIntentIsTypedForExternalAgents(t *testing.T) {
	f := startFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	reported, failed := call(t, ctx, f, "artemis_browser_capabilities", map[string]any{})
	if failed != "" {
		t.Fatal(failed)
	}
	var capabilities browserCapabilities
	if err := json.Unmarshal(reported.Value, &capabilities); err != nil || len(capabilities.Capabilities) == 0 ||
		capabilities.PlaywrightLane.Availability != playwright.AvailabilityUnavailable || capabilities.PlaywrightLane.Remediation == "" {
		t.Fatalf("capabilities = %s %v", reported.Value, err)
	}
	for _, arguments := range []map[string]any{
		{"url": f.url, "engine": "playwright"},
		{"url": f.url, "requires": []string{"capability:video"}},
		{"url": f.url, "engine": "artemis", "requires": []string{"browser:webkit"}},
	} {
		if _, failed := call(t, ctx, f, "artemis_open", arguments); !strings.HasPrefix(failed, "capability_unavailable:") {
			t.Fatalf("open %v = %q, want capability_unavailable", arguments, failed)
		}
	}
	if _, failed := call(t, ctx, f, "artemis_open", map[string]any{"url": f.url, "engine": "netscape"}); !strings.HasPrefix(failed, "invalid_input:") {
		t.Fatalf("unknown engine = %q", failed)
	}
	opened, failed := call(t, ctx, f, "artemis_open", map[string]any{"url": f.url, "engine": "artemis"})
	if failed != "" || !strings.Contains(string(opened.Value), `"engine":"renderless"`) {
		t.Fatalf("artemis open = %s %q", opened.Value, failed)
	}
}

func TestReadObservationRejectsForeignResourceURIs(t *testing.T) {
	server := New(nil, "test")
	for _, uri := range []string{"p1/observation", "https://evil.test/p1/observation", "artemis://pages//observation", "artemis://pages/a/b/observation", "artemis://pages/p1"} {
		_, err := server.readObservation(t.Context(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: uri}})
		if err == nil || err.Error() != mcp.ResourceNotFoundError(uri).Error() {
			t.Fatalf("resource URI %q = %v, want resource not found", uri, err)
		}
	}
}
