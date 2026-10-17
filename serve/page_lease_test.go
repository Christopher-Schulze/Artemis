package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/coder/websocket"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func dispatchTyped(t *testing.T, server *Server, client clientIdentity, id string, command Command, params any) *Response {
	t.Helper()
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return server.dispatch(t.Context(), t.Context(), (*websocket.Conn)(nil), client, &Request{ID: id, Cmd: string(command), Params: encoded}, nil)
}

func TestPageLeaseAndHandoffCommandsGateWritesAndTokens(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestResponse(t, w, `<!doctype html><title>Login</title><input id="code" aria-label="Code">`)
	}))
	defer page.Close()
	parsed, err := url.Parse(page.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := artemis.NewAgent(artemis.AgentConfig{Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer stopTestAgent(t, agent)
	session, err := agent.CreateSession("lease")
	if err != nil {
		t.Fatal(err)
	}
	pageID, _, taskErr := session.OpenChromiumPage(t.Context(), page.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	server := New(agent, Opts{AuthToken: testAuthToken})
	client := clientIdentity{id: "client.test", ownerRef: "test", rateKey: "127.0.0.1"}
	server.trackSession(session.SessionID(), client.id)

	resp := dispatchTyped(t, server, client, "1", CmdPageLease, PageLeaseParams{SessionID: session.SessionID(), PageID: pageID, Op: "acquire", Holder: "ops", TTLMillis: 60_000})
	var lease artemis.WriterLease
	mustDecode(t, *resp, &lease)
	if lease.Token == "" || lease.Holder != "ops" {
		t.Fatalf("lease = %+v", lease)
	}
	if resp := dispatchTyped(t, server, client, "2", CmdPageLease, PageLeaseParams{SessionID: session.SessionID(), PageID: pageID, Op: "acquire", Holder: "other", TTLMillis: 60_000}); resp.OK || resp.Error.Code != string(ErrOwnershipDenied) {
		t.Fatalf("second writer acquired the page: %+v", resp)
	}
	act := ChromiumActParams{SessionID: session.SessionID(), PageID: pageID, Request: actions.Request{Kind: actions.KindNavigate, URL: page.URL}}
	if resp := dispatchTyped(t, server, client, "3", CmdChromiumAct, act); resp.OK || resp.Error.Code != string(ErrOwnershipDenied) {
		t.Fatalf("action without the lease token ran: %+v", resp)
	}
	if resp := dispatchTyped(t, server, client, "4", CmdPageLease, PageLeaseParams{SessionID: session.SessionID(), PageID: pageID, Op: "release", Token: "forged"}); resp.OK {
		t.Fatalf("forged token released the lease: %+v", resp)
	}
	if resp := dispatchTyped(t, server, client, "5", CmdPageLease, PageLeaseParams{SessionID: session.SessionID(), PageID: pageID, Op: "release", Token: lease.Token}); !resp.OK {
		t.Fatalf("holder could not release: %+v", resp)
	}

	resp = dispatchTyped(t, server, client, "6", CmdPageHandoff, PageHandoffParams{SessionID: session.SessionID(), PageID: pageID, Op: "begin", TTLMillis: 60_000})
	var begun PageHandoffResult
	mustDecode(t, *resp, &begun)
	if begun.Ticket == nil || begun.Ticket.OpenToken == "" || begun.Ticket.LeaseToken == "" {
		t.Fatalf("handoff ticket = %+v", begun)
	}
	resp = dispatchTyped(t, server, client, "7", CmdPageHandoff, PageHandoffParams{SessionID: session.SessionID(), PageID: pageID, Op: "open", Token: begun.Ticket.OpenToken})
	var opened PageHandoffResult
	mustDecode(t, *resp, &opened)
	if !strings.Contains(opened.InspectorURL, "/devtools/inspector.html?ws=") {
		t.Fatalf("inspector url = %q", opened.InspectorURL)
	}
	if resp := dispatchTyped(t, server, client, "8", CmdPageHandoff, PageHandoffParams{SessionID: session.SessionID(), PageID: pageID, Op: "open", Token: begun.Ticket.OpenToken}); resp.OK || resp.Error.Code != string(ErrOwnershipDenied) {
		t.Fatalf("handoff token reused: %+v", resp)
	}
	if resp := dispatchTyped(t, server, client, "9", CmdPageHandoff, PageHandoffParams{SessionID: session.SessionID(), PageID: pageID, Op: "end", Token: begun.Ticket.LeaseToken}); !resp.OK {
		t.Fatalf("handoff end failed: %+v", resp)
	}
	for _, bad := range []struct {
		command Command
		params  any
	}{
		{CmdPageLease, PageLeaseParams{SessionID: session.SessionID(), PageID: pageID, Op: "steal"}},
		{CmdPageHandoff, PageHandoffParams{SessionID: session.SessionID(), PageID: pageID, Op: "share"}},
		{CmdPageLease, PageLeaseParams{SessionID: session.SessionID(), PageID: pageID, Op: "acquire", Holder: "ops"}},
	} {
		if resp := dispatchTyped(t, server, client, "bad", bad.command, bad.params); resp.OK || resp.Error.Code != string(ErrBadParams) {
			t.Fatalf("invalid %s request accepted: %+v", bad.command, resp)
		}
	}
}

func TestWorkflowJournalRunIDsAreValidatedAndClientScoped(t *testing.T) {
	request := &Request{ID: "1"}
	unconfigured := &Server{}
	if _, resp := unconfigured.workflowJournalPath(request, "client.a", "run-1"); resp == nil || resp.Error.Code != string(ErrCapabilityUnavailable) {
		t.Fatalf("journal without a directory accepted: %+v", resp)
	}
	dir := t.TempDir()
	server := &Server{opts: Opts{WorkflowJournalDir: dir}}
	for _, bad := range []string{"../escape", "a/b", strings.Repeat("x", 65), "run.jsonl"} {
		if _, resp := server.workflowJournalPath(request, "client.a", bad); resp == nil || resp.Error.Code != string(ErrBadParams) {
			t.Fatalf("run id %q accepted: %+v", bad, resp)
		}
	}
	first, resp := server.workflowJournalPath(request, "client.a", "run-1")
	if resp != nil {
		t.Fatal(resp.Error)
	}
	other, resp := server.workflowJournalPath(request, "client.b", "run-1")
	if resp != nil {
		t.Fatal(resp.Error)
	}
	if first == other || filepath.Dir(filepath.Dir(first)) != dir || filepath.Base(first) != "run-1.jsonl" {
		t.Fatalf("journals not scoped per client: %s %s", first, other)
	}
}
