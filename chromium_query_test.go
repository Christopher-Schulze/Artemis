package artemis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestSessionOwnedTaskQueryRevisionsAndBudgets(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		var html strings.Builder
		html.WriteString("<!doctype html><title>Task query</title>")
		for i := range 2300 {
			if _, err := fmt.Fprintf(&html, "<p>Unrelated content %d</p>", i); err != nil {
				t.Error(err)
				return
			}
		}
		html.WriteString(`<button aria-label="Final action" style="position:fixed;top:20px;left:20px" onclick="this.dataset.count=String(Number(this.dataset.count||0)+1)"><span>Final action</span></button><input aria-label="Value field" value="before" style="position:fixed;top:60px;left:20px">`)
		if _, err := fmt.Fprint(w, html.String()); err != nil {
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
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	alice, err := agent.CreateSession("query-owner")
	if err != nil {
		t.Fatal(err)
	}
	page, legacy, taskErr := alice.OpenChromiumPage(ctx, fixture.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if !legacy.Snapshot.Truncated {
		t.Fatal("large fixture must exercise the old whole-page emission limit")
	}
	request := bridgeobserve.QueryRequest{Options: bridgeobserve.QueryOptions{Text: "Final action", Roles: []string{"button"}, MaxNodes: 10, MaxBytes: 8192}}
	first, taskErr := alice.QueryChromiumPage(ctx, page, request)
	if taskErr != nil || first.Kind != bridgeobserve.QueryFull || first.Lost || first.Snapshot == nil || len(first.Snapshot.Nodes) != 1 {
		t.Fatalf("large-page target was lost behind unrelated content: %+v error=%v", first, taskErr)
	}
	node := first.Snapshot.Nodes[0]
	if !node.Interactable || node.Hit != bridgeobserve.HitClear || !strings.HasPrefix(node.Ref, page+":") {
		t.Fatalf("scoped span/button ancestry or page binding invalid: %+v", node)
	}
	request.BaseRevision = first.Revision
	unchanged, taskErr := alice.QueryChromiumPage(ctx, page, request)
	if taskErr != nil || unchanged.Kind != bridgeobserve.QueryUnchanged || unchanged.Snapshot != nil || unchanged.Revision != first.Revision {
		t.Fatalf("persistent unchanged query invalid: %+v error=%v", unchanged, taskErr)
	}
	outcome, taskErr := alice.ActChromiumPage(ctx, page, first.Epoch, actions.Request{Kind: actions.KindClick, Ref: node.Ref})
	if taskErr != nil || !outcome.Success || outcome.Evidence.Attempts != 1 {
		t.Fatalf("late scoped ref was not actually actionable: %+v err=%v", outcome, taskErr)
	}
	delta, taskErr := alice.QueryChromiumPage(ctx, page, request)
	if taskErr != nil || delta.Kind != bridgeobserve.QueryDelta || delta.Delta == nil || len(delta.Delta.Changed) != 1 || delta.Delta.Changed[0].Attributes["data-count"] != "1" {
		t.Fatalf("actual once-only effect missing from material delta: %+v err=%v", delta, taskErr)
	}
	raw, err := json.Marshal(delta)
	if err != nil || len(raw) > request.Options.MaxBytes || !strings.HasPrefix(delta.Delta.Changed[0].Ref, page+":") {
		t.Fatalf("bound delta bytes/ref invalid: bytes=%d result=%+v err=%v", len(raw), delta, err)
	}
	if _, err := alice.QueryChromiumPage(ctx, page, bridgeobserve.QueryRequest{Options: bridgeobserve.QueryOptions{TargetRef: "another-page:e1"}}); err == nil || err.Code != TaskErrorStaleReference {
		t.Fatalf("foreign-page target ref admitted: %v", err)
	}
	valueRequest := bridgeobserve.QueryRequest{Options: bridgeobserve.QueryOptions{Roles: []string{"textbox"}, Text: "Value field"}}
	valueBefore, taskErr := alice.QueryChromiumPage(ctx, page, valueRequest)
	if taskErr != nil || valueBefore.Snapshot == nil || len(valueBefore.Snapshot.Nodes) != 1 || valueBefore.Snapshot.Nodes[0].Value != "before" {
		t.Fatalf("native input value missing: %+v err=%v", valueBefore, taskErr)
	}
	filled, taskErr := alice.ActChromiumPage(ctx, page, valueBefore.Epoch, actions.Request{Kind: actions.KindFill, Ref: valueBefore.Snapshot.Nodes[0].Ref, Text: "after"})
	if taskErr != nil || !filled.Success {
		t.Fatalf("scoped input could not fill: %+v err=%v", filled, taskErr)
	}
	valueRequest.BaseRevision = valueBefore.Revision
	valueAfter, taskErr := alice.QueryChromiumPage(ctx, page, valueRequest)
	if taskErr != nil || valueAfter.Kind != bridgeobserve.QueryDelta || valueAfter.Delta == nil || len(valueAfter.Delta.Changed) != 1 || valueAfter.Delta.Changed[0].Value != "after" {
		t.Fatalf("actual input value mutation missing from delta: %+v err=%v", valueAfter, taskErr)
	}
	otherPage, _, taskErr := alice.OpenChromiumPage(ctx, fixture.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	foreign, taskErr := alice.QueryChromiumPage(ctx, otherPage, request)
	if taskErr != nil || foreign.Kind != bridgeobserve.QueryFull || foreign.ResetReason != "baseline_unavailable" || !foreign.Lost {
		t.Fatalf("another page accepted this baseline: %+v err=%v", foreign, taskErr)
	}
	verified, taskErr := alice.ActChromiumPage(ctx, page, 0, actions.Request{Kind: actions.KindAssert, Expression: "document.querySelector('button').dataset.count==='1'"})
	if taskErr != nil || !verified.Success {
		t.Fatalf("counter changed after querying another page: %+v err=%v", verified, taskErr)
	}
}
