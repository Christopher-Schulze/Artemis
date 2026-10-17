package artemis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

const orderFormPage = `<!doctype html><title>Order</title>
<form method="post" action="/order"><label>Item <input id="item" name="item"></label>
<button type="submit">Order</button></form>`

// orderServer counts server-side order effects so a test can prove that a
// resumed run never repeats one.
func orderServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var orders atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/order" && r.Method == http.MethodPost {
			count := orders.Add(1)
			fmt.Fprintf(w, `<!doctype html><title>Done</title><p>Order placed #%d for %s</p>`, count, r.FormValue("item"))
			return
		}
		fmt.Fprint(w, orderFormPage)
	}))
	t.Cleanup(server.Close)
	return server, &orders
}

func orderWorkflow(origin string) WorkflowDefinition {
	return WorkflowDefinition{
		Version: WorkflowVersion, Name: "place order", Origins: []string{origin},
		Params: []WorkflowParam{{Name: "item"}},
		Steps: []WorkflowStep{
			{Action: actions.KindFill, Target: &WorkflowTarget{Role: "textbox", Name: "Item"}, Param: "item"},
			{Action: actions.KindClick, Target: &WorkflowTarget{Role: "button", Name: "Order"}, Compensation: "cancel the order", Outcome: &WorkflowOutcome{Type: "text_present", Value: "Order placed"}},
		},
	}
}

func startOrderSession(t *testing.T) (*Session, string, *httptest.Server, *atomic.Int32, context.Context) {
	t.Helper()
	server, orders := orderServer(t)
	agent := startAdBlockAgent(t, server, nil, false)
	session, err := agent.CreateSession("resume")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	return session, pageID, server, orders, ctx
}

// dropLastJournalLines simulates a crash by removing the last n records.
func dropLastJournalLines(t *testing.T, path string, n int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	lines = lines[:len(lines)-1-n]
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowResumeNeverRepeatsAVerifiedOrInterruptedEffect(t *testing.T) {
	session, pageID, server, orders, ctx := startOrderSession(t)
	origin := strings.TrimSuffix(server.URL, "/")
	def := orderWorkflow(origin)
	inputs := map[string]WorkflowInput{"item": {Value: "lamp"}}
	journal := filepath.Join(t.TempDir(), "runs", "order.jsonl")

	run, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || !run.Success || orders.Load() != 1 {
		t.Fatalf("journaled run failed: %+v %v orders=%d", run, taskErr, orders.Load())
	}

	// Repeated run after a lost reply: the completed journal replays receipts.
	again, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || !again.Success || again.Steps[1].Status != WorkflowStepVerified || orders.Load() != 1 {
		t.Fatalf("repeated run duplicated the effect: %+v %v orders=%d", again, taskErr, orders.Load())
	}

	// Crash after the effect, before its receipt: the outcome is verified on
	// the current page and journaled instead of ordering twice.
	dropLastJournalLines(t, journal, 1)
	resumed, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || !resumed.Success || resumed.Steps[1].Status != WorkflowStepVerified || orders.Load() != 1 {
		t.Fatalf("resume after effect repeated it: %+v %v orders=%d", resumed, taskErr, orders.Load())
	}
	records, err := (&workflowJournal{path: journal}).load()
	if err != nil || len(records) == 0 {
		t.Fatalf("journal unreadable: %v", err)
	}
	if last := records[len(records)-1]; last.Kind != journalReceipt || last.Step != 1 || last.Status != WorkflowStepVerified || last.URL != server.URL+"/order" {
		t.Fatalf("resume did not journal the verified receipt: %+v %v", records, err)
	}

	// Crash after dispatching the destructive step on a page that no longer
	// shows its outcome: the effect is unknown, so the run stops uncertain.
	dropLastJournalLines(t, journal, 1)
	reload(t, session, pageID, server.URL)
	uncertain, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || uncertain.Success || uncertain.Steps[1].Status != WorkflowStepUncertain || orders.Load() != 1 {
		t.Fatalf("interrupted destructive step was not held uncertain: %+v %v orders=%d", uncertain, taskErr, orders.Load())
	}
	held, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || held.Success || held.Steps[1].Status != WorkflowStepUncertain || orders.Load() != 1 {
		t.Fatalf("uncertain journal resumed with dispatch: %+v %v orders=%d", held, taskErr, orders.Load())
	}
}

func TestWorkflowResumeRetriesFailedAndUnattemptedStepsFromTheVerifiedPage(t *testing.T) {
	session, pageID, server, orders, ctx := startOrderSession(t)
	origin := strings.TrimSuffix(server.URL, "/")
	def := orderWorkflow(origin)
	inputs := map[string]WorkflowInput{"item": {Value: "desk"}}
	journal := filepath.Join(t.TempDir(), "order.jsonl")

	// Crash before the destructive step's intent: fill is verified, the
	// order was never dispatched.
	run, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || !run.Success || orders.Load() != 1 {
		t.Fatalf("journaled run failed: %+v %v", run, taskErr)
	}
	dropLastJournalLines(t, journal, 2)
	// The tab is lost; the recovered run starts on a fresh page of the same
	// session elsewhere and returns to the last verified URL first.
	if taskErr := session.CloseChromiumPage(ctx, pageID); taskErr != nil {
		t.Fatal(taskErr)
	}
	pageID, _, taskErr = session.OpenChromiumPage(ctx, server.URL+"/elsewhere")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	resumed, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: journal})
	if taskErr != nil || !resumed.Success || orders.Load() != 2 || !strings.Contains(pageText(t, session, pageID), "Order placed #2") {
		t.Fatalf("unattempted step was not executed after alignment: %+v %v orders=%d", resumed, taskErr, orders.Load())
	}
	if resumed.Steps[0].Status != WorkflowStepVerified || resumed.Steps[0].Effect != "" {
		t.Fatalf("verified fill was re-executed: %+v", resumed.Steps[0])
	}

	// A step journaled as failed had no effect and is retried.
	failedJournal := filepath.Join(t.TempDir(), "failed.jsonl")
	reload(t, session, pageID, server.URL)
	journalFile, _, err := openWorkflowJournal(failedJournal, def)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []journalRecord{
		{Kind: journalIntent, Step: 0}, {Kind: journalReceipt, Step: 0, Status: WorkflowStepFailed},
	} {
		if err := journalFile.append(record); err != nil {
			t.Fatal(err)
		}
	}
	retried, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Journal: failedJournal})
	if taskErr != nil || !retried.Success || orders.Load() != 3 {
		t.Fatalf("failed step was not retried: %+v %v orders=%d", retried, taskErr, orders.Load())
	}
}

func TestWorkflowJournalRefusesCorruptionAndForeignDefinitions(t *testing.T) {
	origin := "http://127.0.0.1:1"
	def := orderWorkflow(origin)
	dir := t.TempDir()

	torn := filepath.Join(dir, "torn.jsonl")
	journal, _, err := openWorkflowJournal(torn, def)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.append(journalRecord{Kind: journalIntent, Step: 0}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(torn, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"kind":"receipt","st`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, records, err := openWorkflowJournal(torn, def)
	if err != nil || len(records) != 1 || records[0].Kind != journalIntent {
		t.Fatalf("torn tail was not dropped: %+v %v", records, err)
	}
	states, err := journalSteps(records, len(def.Steps))
	if err != nil || states[0].status != "pending" || states[1].status != "" {
		t.Fatalf("torn journal state: %+v %v", states, err)
	}
	if data, err := os.ReadFile(torn); err != nil || strings.Contains(string(data), `"st`+"\n") || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("torn tail was not rewritten away: %q %v", data, err)
	}

	corrupt := filepath.Join(dir, "corrupt.jsonl")
	if _, _, err := openWorkflowJournal(corrupt, def); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corrupt, append([]byte("{not json}\n"), data...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openWorkflowJournal(corrupt, def); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt journal was accepted: %v", err)
	}

	foreign := filepath.Join(dir, "foreign.jsonl")
	if _, _, err := openWorkflowJournal(foreign, def); err != nil {
		t.Fatal(err)
	}
	changed := def
	changed.Steps = append([]WorkflowStep(nil), def.Steps...)
	changed.Steps[1].Outcome = &WorkflowOutcome{Type: "text_present", Value: "Thanks"}
	if _, _, err := openWorkflowJournal(foreign, changed); err == nil || !strings.Contains(err.Error(), "different workflow definition") {
		t.Fatalf("journal of another definition was resumed: %v", err)
	}
	if _, err := journalSteps([]journalRecord{{Kind: journalIntent, Step: 7}}, len(def.Steps)); err == nil {
		t.Fatal("journal step outside the definition was accepted")
	}
}

func TestWriterLeaseIsExclusiveExpiringAndKeepsReadersOpen(t *testing.T) {
	session, pageID, server, orders, ctx := startOrderSession(t)
	lease, taskErr := session.AcquireWriterLease(pageID, "operator", 200*time.Millisecond)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if _, taskErr := session.AcquireWriterLease(pageID, "other", time.Minute); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied {
		t.Fatalf("second writer acquired a held page: %v", taskErr)
	}
	itemRef, epoch := workflowNode(t, session, pageID, "textbox", "Item")
	if _, taskErr := session.ActChromiumPage(ctx, pageID, epoch, actions.Request{Kind: actions.KindFill, Ref: itemRef, Text: "x"}); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied {
		t.Fatalf("write without the lease token was allowed: %v", taskErr)
	}
	if outcome, taskErr := session.ActChromiumPage(WithWriterLease(ctx, lease.Token), pageID, epoch, actions.Request{Kind: actions.KindFill, Ref: itemRef, Text: "x"}); taskErr != nil || !outcome.Success {
		t.Fatalf("lease holder could not write: %+v %v", outcome, taskErr)
	}
	def := orderWorkflow(strings.TrimSuffix(server.URL, "/"))
	if _, taskErr := session.RunWorkflow(ctx, pageID, def, map[string]WorkflowInput{"item": {Value: "chair"}}, WorkflowRunOptions{}); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied || orders.Load() != 0 {
		t.Fatalf("workflow ran on a page leased to another holder: %v orders=%d", taskErr, orders.Load())
	}
	if taskErr := session.ReleaseWriterLease(pageID, "forged"); taskErr == nil {
		t.Fatal("foreign token released the lease")
	}
	if _, taskErr := session.AcquireWriterLease(pageID, "operator", time.Hour); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("lease beyond the 30m bound was granted: %v", taskErr)
	}
	expiry := time.NewTimer(300 * time.Millisecond)
	select {
	case <-expiry.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	run, taskErr := session.RunWorkflow(ctx, pageID, def, map[string]WorkflowInput{"item": {Value: "chair"}}, WorkflowRunOptions{})
	if taskErr != nil || !run.Success || orders.Load() != 1 {
		t.Fatalf("expired lease still blocked the page: %+v %v", run, taskErr)
	}
	// The run released its own lease.
	if released, taskErr := session.AcquireWriterLease(pageID, "operator", time.Minute); taskErr != nil {
		t.Fatalf("workflow kept its lease: %v", taskErr)
	} else if taskErr := session.ReleaseWriterLease(pageID, released.Token); taskErr != nil {
		t.Fatal(taskErr)
	}
}

func TestHandoffTokenIsOneTimeAndResumeRequiresFreshObservation(t *testing.T) {
	session, pageID, _, _, ctx := startOrderSession(t)
	itemRef, epoch := workflowNode(t, session, pageID, "textbox", "Item")
	ticket, taskErr := session.BeginHandoff(pageID, time.Minute)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	inspector, taskErr := session.OpenHandoff(ticket.OpenToken)
	if taskErr != nil || !strings.Contains(inspector, "/devtools/inspector.html?ws=") || !strings.Contains(inspector, "%2Fdevtools%2Fpage%2F") {
		t.Fatalf("handoff did not open the page inspector: %q %v", inspector, taskErr)
	}
	if _, taskErr := session.OpenHandoff(ticket.OpenToken); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied {
		t.Fatalf("handoff token was reusable: %v", taskErr)
	}
	if _, taskErr := session.ActChromiumPage(ctx, pageID, epoch, actions.Request{Kind: actions.KindFill, Ref: itemRef, Text: "agent"}); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied {
		t.Fatalf("agent wrote during the human handoff: %v", taskErr)
	}
	if outcome, taskErr := session.ActChromiumPage(WithWriterLease(ctx, ticket.LeaseToken), pageID, epoch, actions.Request{Kind: actions.KindFill, Ref: itemRef, Text: "human"}); taskErr != nil || !outcome.Success {
		t.Fatalf("human could not act during the handoff: %+v %v", outcome, taskErr)
	}
	if taskErr := session.EndHandoff(pageID, "forged"); taskErr == nil {
		t.Fatal("handoff ended with a foreign token")
	}
	if taskErr := session.EndHandoff(pageID, ticket.LeaseToken); taskErr != nil {
		t.Fatal(taskErr)
	}
	if _, taskErr := session.ActChromiumPage(ctx, pageID, epoch, actions.Request{Kind: actions.KindFill, Ref: itemRef, Text: "agent"}); taskErr == nil || taskErr.Code != TaskErrorStaleReference {
		t.Fatalf("reference action ran without observing the human's changes: %v", taskErr)
	}
	evidence, taskErr := session.ObserveChromiumPage(ctx, pageID, bridgeobserve.ModeFull, "")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	freshRef, freshEpoch := workflowNode(t, session, pageID, "textbox", "Item")
	if outcome, taskErr := session.ActChromiumPage(ctx, pageID, freshEpoch, actions.Request{Kind: actions.KindFill, Ref: freshRef, Text: "agent"}); taskErr != nil || !outcome.Success || evidence.Snapshot.Epoch == 0 {
		t.Fatalf("agent could not act after observing: %+v %v", outcome, taskErr)
	}
	if _, taskErr := session.OpenHandoff("unknown"); taskErr == nil {
		t.Fatal("unknown handoff token opened the inspector")
	}
}
