package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/playwright"
)

// TestEngineIntentFailsTypedAndNeverSubstitutes drives the protocol 2.5.0
// engine intent over the wire: the capabilities command reports the
// Playwright lane truthfully, explicit or required Playwright-only work fails
// capability_unavailable with remediation instead of opening an Artemis page,
// and explicit or automatic Artemis intent opens normally.
func TestEngineIntentFailsTypedAndNeverSubstitutes(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestResponse(t, w, `<!doctype html><html><head><title>Intent</title></head><body>ok</body></html>`)
	}))
	defer page.Close()
	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer closeTestWebsocketNow(t, c)

	resp := roundTrip(t, c, Request{ID: "1", Cmd: string(CmdCapabilities)})
	var capabilities VersionResponse
	mustDecode(t, resp, &capabilities)
	if capabilities.Protocol != "2.5.0" || capabilities.PlaywrightLane == nil || capabilities.PlaywrightLane.Availability != playwright.AvailabilityUnavailable || capabilities.PlaywrightLane.Remediation == "" {
		t.Fatalf("capabilities = %+v", capabilities)
	}
	resp = roundTrip(t, c, Request{ID: "2", Cmd: string(CmdSessionNew)})
	var session SessionNewResult
	mustDecode(t, resp, &session)

	refusals := []struct {
		id     string
		intent EngineIntent
		code   ErrCode
	}{
		{"3", EngineIntent{Engine: "playwright"}, ErrCapabilityUnavailable},
		{"4", EngineIntent{Requires: []string{"capability:har"}}, ErrCapabilityUnavailable},
		{"5", EngineIntent{Engine: "artemis", Requires: []string{"browser:firefox"}}, ErrCapabilityUnavailable},
		{"6", EngineIntent{Engine: "chromium"}, ErrBadParams},
		{"7", EngineIntent{Requires: []string{"capability:teleport"}}, ErrBadParams},
	}
	for _, refusal := range refusals {
		resp = roundTrip(t, c, mustMarshalTyped(t, refusal.id, CmdPageOpen, PageOpenParams{SessionID: session.SessionID, URL: page.URL, EngineIntent: refusal.intent}))
		if resp.OK || resp.Error == nil || resp.Error.Code != string(refusal.code) {
			t.Fatalf("intent %+v response = %+v", refusal.intent, resp)
		}
		if refusal.code == ErrCapabilityUnavailable && !strings.Contains(resp.Error.Message, "unavailable") {
			t.Fatalf("intent %+v error lacks the typed unavailability: %q", refusal.intent, resp.Error.Message)
		}
	}
	resp = roundTrip(t, c, Request{ID: "8", Cmd: string(CmdSessionList)})
	var list SessionListResult
	mustDecode(t, resp, &list)
	for _, info := range list.Sessions {
		if info.TabCount != 0 {
			t.Fatalf("a refused intent opened a page: %+v", list)
		}
	}
	for id, intent := range map[string]EngineIntent{"9": {Engine: "artemis"}, "10": {Requires: []string{"capability:screenshot"}}} {
		resp = roundTrip(t, c, mustMarshalTyped(t, id, CmdPageOpen, PageOpenParams{SessionID: session.SessionID, URL: page.URL, EngineIntent: intent}))
		var opened PageOpenResult
		mustDecode(t, resp, &opened)
		if opened.Title != "Intent" {
			t.Fatalf("artemis intent %+v did not open the page: %+v", intent, opened)
		}
	}
}
