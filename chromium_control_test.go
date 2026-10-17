package artemis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestSessionOwnedChromiumControlAndState(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, `<!doctype html><title>Owned control</title><label>Name <input id="name"></label><button onclick="document.querySelector('#result').textContent=document.querySelector('#name').value">Save</button><p id="result"></p>`); err != nil {
			t.Error(err)
		}
	}))
	defer fixture.Close()
	parsed, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgent(AgentConfig{PolicyEngine: fixtureActionPolicy(t), Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	alice, err := agent.CreateSession("alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := agent.CreateSession("bob")
	if err != nil {
		t.Fatal(err)
	}
	pageID, evidence, taskErr := alice.OpenChromiumPage(ctx, fixture.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	dump, taskErr := alice.DumpChromiumPage(ctx, pageID, "markdown")
	if taskErr != nil || dump == "" {
		t.Fatalf("live extraction unavailable: %v %v", dump, taskErr)
	}
	var inputRef, buttonRef string
	for _, node := range evidence.Snapshot.Nodes {
		if node.Tag == "input" {
			inputRef = node.Ref
		}
		if node.Role == "button" {
			buttonRef = node.Ref
		}
	}
	if inputRef == "" || buttonRef == "" {
		t.Fatalf("missing actionable refs: %+v", evidence.Snapshot.Nodes)
	}
	if _, err := bob.ActChromiumPage(ctx, pageID, evidence.Snapshot.Epoch, actions.Request{Kind: actions.KindFill, Ref: inputRef, Text: "intruder"}); err == nil || err.Code != TaskErrorPageNotFound {
		t.Fatalf("cross-session page admitted: %v", err)
	}
	for _, request := range []actions.Request{{Kind: actions.KindFill, Ref: inputRef, Text: "Alice"}, {Kind: actions.KindClick, Ref: buttonRef}} {
		outcome, err := alice.ActChromiumPage(ctx, pageID, evidence.Snapshot.Epoch, request)
		if err != nil || !outcome.Success {
			t.Fatalf("action: %+v %v", outcome, err)
		}
	}
	outcome, taskErr := alice.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindEvaluate, Expression: `document.querySelector('#result').textContent`})
	if taskErr != nil || !outcome.Success || outcome.Value != "Alice" {
		t.Fatalf("real business effect missing: %+v %v", outcome, taskErr)
	}
	set, taskErr := alice.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindEvaluate, Expression: `document.cookie='owner=alice'; localStorage.setItem('owner','alice'); 'stored'`})
	if taskErr != nil || !set.Success {
		t.Fatalf("seed: %+v %v", set, taskErr)
	}
	bobPage, _, taskErr := bob.OpenChromiumPage(ctx, fixture.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	wrongPage, wrongErr := bob.ActChromiumPage(ctx, bobPage, evidence.Snapshot.Epoch, actions.Request{Kind: actions.KindFill, Ref: inputRef, Text: "cross-page"})
	if wrongErr == nil || wrongErr.Code != TaskErrorStaleReference || wrongPage.Success {
		t.Fatalf("another page's ref silently rebound: %+v %v", wrongPage, wrongErr)
	}
	isolated, taskErr := bob.ActChromiumPage(ctx, bobPage, 0, actions.Request{Kind: actions.KindEvaluate, Expression: `document.cookie + ':' + localStorage.getItem('owner')`})
	if taskErr != nil || !isolated.Success || isolated.Value != ":null" {
		t.Fatalf("state crossed contexts: %+v %v", isolated, taskErr)
	}
	navigated, taskErr := alice.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindNavigate, URL: fixture.URL + "/next"})
	if taskErr != nil || !navigated.Success {
		t.Fatalf("navigate: %+v %v", navigated, taskErr)
	}
	if _, err := alice.ActChromiumPage(ctx, pageID, evidence.Snapshot.Epoch, actions.Request{Kind: actions.KindClick, Ref: buttonRef}); err == nil || err.Code != TaskErrorStaleReference {
		t.Fatalf("old-document ref admitted: %v", err)
	}
	if _, err := alice.ObserveChromiumPage(ctx, pageID, bridgeobserve.ModeFull, ""); err != nil {
		t.Fatal(err)
	}
	if err := agent.CloseSession(alice.SessionID()); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.ObserveChromiumPage(ctx, pageID, bridgeobserve.ModeFull, ""); err == nil {
		t.Fatal("closed session remained usable")
	}
	if _, err := bob.ObserveChromiumPage(ctx, bobPage, bridgeobserve.ModeFull, ""); err != nil {
		t.Fatalf("other session lost browser: %v", err)
	}
}

func TestMissingChromiumPreservesRenderlessStartup(t *testing.T) {
	agent, err := NewAgent(AgentConfig{Chromium: &browserprocess.LaunchConfig{BinaryPath: "/does-not-exist/artemis-chrome", Headless: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	session, err := agent.CreateSession("owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.OpenChromiumPage(t.Context(), "https://example.com"); err == nil || err.Code != TaskErrorCapabilityUnavailable {
		t.Fatalf("missing Chrome must be actionable denial: %v", err)
	}
	if !agent.Health().Renderless.Healthy || agent.Health().Chromium.Healthy || session.TabCount() != 0 {
		t.Fatalf("failed CDP admission corrupted state: %+v tabs=%d", agent.Health(), session.TabCount())
	}
}
