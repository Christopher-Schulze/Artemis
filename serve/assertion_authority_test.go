package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/network"
)

func TestAuthenticatedReaderAssertionsPreserveDocumentAndCodeAuthority(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		writeTestResponse(t, w, `<html><head><title>Control title</title></head><body><p id="notice">Control body</p></body></html>`)
	}))
	t.Cleanup(fixture.Close)
	address, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(address.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopTestAgent(t, agent) })
	server := New(agent, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	httpServer := httptest.NewServer(http.HandlerFunc(server.handleWS))
	t.Cleanup(httpServer.Close)
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	endpoint := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client, err := Dial(t.Context(), endpoint, testAuthToken, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	var session SessionNewResult
	if err := client.Call(t.Context(), CmdSessionNew, SessionNewParams{}, &session); err != nil {
		t.Fatal(err)
	}
	var page PageOpenResult
	if err := client.Call(t.Context(), CmdPageOpen, PageOpenParams{SessionID: session.SessionID, URL: fixture.URL + "/proof"}, &page); err != nil {
		t.Fatal(err)
	}
	if page.Status != 200 || page.Title != "Control title" || page.URL != fixture.URL+"/proof" {
		t.Fatalf("actual control page: %+v", page)
	}
	for _, test := range []struct {
		mode      string
		selector  string
		substring string
		got       string
	}{
		{"selector_exists", "#notice", "", "exists=true"},
		{"title_contains", "", "Control title", "Control title"},
		{"text_contains", "", "Control body", "Control body"},
		{"url_contains", "", "/proof", page.URL},
		{"status_eq", "", "", "200"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			params := PageAssertParams{SessionID: session.SessionID, PageID: page.PageID, Mode: test.mode, Selector: test.selector, Substring: test.substring, Status: 200}
			var result AssertResult
			if err := client.Call(t.Context(), CmdPageAssert, params, &result); err != nil || !result.Pass || !strings.Contains(result.Got, test.got) {
				t.Fatalf("authenticated positive document assertion: %+v %v", result, err)
			}
			params.Selector, params.Substring, params.Status = "#absent", "Absent marker", 503
			if err := client.Call(t.Context(), CmdPageAssert, params, &result); err != nil || result.Pass {
				t.Fatalf("authenticated negative document assertion: %+v %v", result, err)
			}
			params.Expr = `(globalThis.controlEffects=(globalThis.controlEffects||0)+1)>0`
			if err := client.Call(t.Context(), CmdPageAssert, params, &result); err == nil || !strings.HasPrefix(err.Error(), string(ErrBadParams)+":") {
				t.Fatalf("caller code disguised as %s: %v", test.mode, err)
			}
		})
	}
	params := PageAssertParams{SessionID: session.SessionID, PageID: page.PageID, Mode: "eval_truthy", Expr: `(globalThis.controlEffects=(globalThis.controlEffects||0)+1)>0`}
	var result AssertResult
	if err := client.Call(t.Context(), CmdPageAssert, params, &result); err == nil || !strings.HasPrefix(err.Error(), string(ErrOwnershipDenied)+":") {
		t.Fatalf("reader evaluated caller code: %+v %v", result, err)
	}
	owned, ok := agent.Session(session.SessionID)
	if !ok || owned.Page(page.PageID) == nil {
		t.Fatal("actual owned control page missing")
	}
	value, err := owned.Page(page.PageID).Eval(t.Context(), `String(globalThis.controlEffects||0)`)
	if err != nil || value == nil || value.String() != "0" {
		t.Fatalf("denied control code had effects: %v %v", value, err)
	}
	peer, err := Dial(t.Context(), endpoint, testAuthToken, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := peer.Close(); err != nil {
			t.Error(err)
		}
	})
	params.Mode, params.Expr, params.Substring = "title_contains", "", "Control"
	if err := peer.Call(t.Context(), CmdPageAssert, params, &result); err == nil || !strings.HasPrefix(err.Error(), string(ErrOwnershipDenied)+":") {
		t.Fatalf("foreign authenticated owner read a page: %+v %v", result, err)
	}
	params.Mode = "unknown"
	if err := client.Call(t.Context(), CmdPageAssert, params, &result); err == nil || !strings.HasPrefix(err.Error(), string(ErrBadMode)+":") {
		t.Fatalf("unknown mode lost wire contract: %+v %v", result, err)
	}
	if err := client.Call(t.Context(), CmdSessionClose, SessionCloseParams{SessionID: session.SessionID}, nil); err != nil {
		t.Fatal(fmt.Errorf("close control session: %w", err))
	}
}
