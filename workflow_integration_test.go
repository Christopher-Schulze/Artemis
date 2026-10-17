package artemis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
)

const workflowFormPage = `<!doctype html><title>Profile</title>
<form id="profile" action="/save" onsubmit="event.preventDefault(); document.getElementById('result').textContent = 'Saved ' + document.getElementById('name').value + ' len' + document.getElementById('pw').value.length">
<label>Name <input id="name" name="name"></label>
<label>Password <input id="pw" name="pw" type="password"></label>
<button type="submit">Save</button></form><p id="result"></p>`

func workflowServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		page := workflowFormPage
		if r.URL.Path == "/dup" {
			page = strings.Replace(page, `<p id="result">`, `<button type="button">Save</button><p id="result">`, 1)
		}
		fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)
	return server
}

func workflowNode(t *testing.T, session *Session, pageID, role, name string) (string, uint64) {
	t.Helper()
	evidence, taskErr := session.ObserveChromiumPage(t.Context(), pageID, bridgeobserve.ModeFull, "")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	for _, node := range evidence.Snapshot.Nodes {
		if node.Role == role && node.Name == name {
			return node.Ref, evidence.Snapshot.Epoch
		}
	}
	t.Fatalf("no %s %q in %+v", role, name, evidence.Snapshot.Nodes)
	return "", 0
}

func pageText(t *testing.T, session *Session, pageID string) string {
	t.Helper()
	text, taskErr := session.DumpChromiumPage(t.Context(), pageID, "text")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	return fmt.Sprint(text)
}

func reload(t *testing.T, session *Session, pageID, target string) {
	t.Helper()
	if outcome, taskErr := session.ActChromiumPage(t.Context(), pageID, 0, actions.Request{Kind: actions.KindNavigate, URL: target}); taskErr != nil || !outcome.Success {
		t.Fatalf("navigate %s: %+v %v", target, outcome, taskErr)
	}
}

func TestWorkflowRecordConvertRunShadowAndDrift(t *testing.T) {
	server := workflowServer(t)
	agent := startAdBlockAgent(t, server, nil, false)
	session, err := agent.CreateSession("workflow")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if taskErr := session.StartWorkflowRecording(pageID, "operator", "save profile", observe.DefaultWorkflowCaptureConfig()); taskErr != nil {
		t.Fatal(taskErr)
	}
	nameRef, epoch := workflowNode(t, session, pageID, "textbox", "Name")
	if outcome, taskErr := session.ActChromiumPage(ctx, pageID, epoch, actions.Request{Kind: actions.KindFill, Ref: nameRef, Text: "Ada-Secret-Typed"}); taskErr != nil || !outcome.Success {
		t.Fatalf("fill: %+v %v", outcome, taskErr)
	}
	saveRef, epoch := workflowNode(t, session, pageID, "button", "Save")
	if outcome, taskErr := session.ActChromiumPage(ctx, pageID, epoch, actions.Request{Kind: actions.KindClick, Ref: saveRef}); taskErr != nil || !outcome.Success {
		t.Fatalf("click: %+v %v", outcome, taskErr)
	}
	recording, taskErr := session.StopWorkflowRecording(pageID)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if len(recording.Actions) != 2 || recording.Actions[0].AXBefore == nil || recording.Actions[0].AXAfter == nil || !recording.Actions[1].IsDestructive {
		t.Fatalf("recording is not an owned before/action/after trace: %+v", recording)
	}
	// The typed value is never recorded as input: not in tool arguments or
	// results and not as a field value, even right after the fill. (The page
	// later displays it, which the after-click AX snapshot rightly observes.)
	afterFill, err := json.Marshal(recording.Actions[0].AXAfter)
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range []string{recording.Actions[0].ToolCall.ArgsJSON, recording.Actions[0].ToolCall.ResultJSON, string(afterFill)} {
		if strings.Contains(recorded, "Ada-Secret-Typed") {
			t.Fatalf("typed input reached the recording: %s", recorded)
		}
	}
	if selectors := recording.Actions[0].Selectors; len(selectors) == 0 || selectors[0].Value != "textbox:Name" || selectors[0].Confidence != 1 {
		t.Fatalf("stable selector missing: %+v", selectors)
	}
	origin := strings.TrimSuffix(server.URL, "/")
	def, err := WorkflowFromRecording(recording, []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]WorkflowInput{"input_0": {Value: "Grace"}}
	if err := ValidateWorkflow(def, inputs); err == nil || !strings.Contains(err.Error(), "compensation") {
		t.Fatalf("destructive step without compensation was activatable: %v", err)
	}
	def.Steps[1].Compensation = "clear the saved name"
	def.Steps[1].Outcome = &WorkflowOutcome{Type: "text_present", Value: "Saved Grace"}

	reload(t, session, pageID, server.URL)
	shadow, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{Shadow: true})
	if taskErr != nil || !shadow.Success || shadow.Steps[0].Status != WorkflowStepPlanned || shadow.Steps[1].Status != WorkflowStepPlanned || strings.Contains(pageText(t, session, pageID), "Saved") {
		t.Fatalf("shadow run dispatched or failed: %+v %v", shadow, taskErr)
	}

	run, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{})
	if taskErr != nil || !run.Success || run.Steps[1].Status != WorkflowStepVerified || !strings.Contains(pageText(t, session, pageID), "Saved Grace") {
		t.Fatalf("workflow did not run and verify: %+v %v", run, taskErr)
	}
	if trust := run.Steps[0].Trust; trust.Origin != origin || trust.Source != "workflow" || trust.Trust != "untrusted" {
		t.Fatalf("step result lacks its page trust label: %+v", trust)
	}

	reload(t, session, pageID, server.URL+"/dup")
	drift, taskErr := session.RunWorkflow(ctx, pageID, def, inputs, WorkflowRunOptions{})
	if taskErr != nil || drift.Success || drift.Steps[1].Status != WorkflowStepFailed || !strings.Contains(drift.Steps[1].Error, "selector drift") || drift.Steps[0].Status != WorkflowStepVerified {
		t.Fatalf("duplicate target did not fail closed after the first receipt: %+v %v", drift, taskErr)
	}
	if strings.Contains(pageText(t, session, pageID), "Saved") {
		t.Fatal("ambiguous step was dispatched")
	}

	reload(t, session, pageID, server.URL)
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, taskErr := session.RunWorkflow(cancelled, pageID, def, inputs, WorkflowRunOptions{}); taskErr == nil {
		t.Fatal("cancelled workflow ran")
	}
}

func TestWorkflowCredentialInputsAndAuthority(t *testing.T) {
	server := workflowServer(t)
	agent := startAdBlockAgent(t, server, nil, false)
	session, err := agent.CreateSession("workflow-credentials")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	store, err := profile.NewCredentialStore(filepath.Join(t.TempDir(), "credentials.json"), []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StoreCredential("ops", "127.0.0.1", "ada", "s3cret-pw", profile.LoginSelectors{}); err != nil {
		t.Fatal(err)
	}
	origin := strings.TrimSuffix(server.URL, "/")
	def := WorkflowDefinition{Version: WorkflowVersion, Name: "login", Origins: []string{origin},
		Params: []WorkflowParam{{Name: "password", Sensitive: true}},
		Steps: []WorkflowStep{
			{Action: actions.KindFill, Target: &WorkflowTarget{Role: "textbox", Name: "Password"}, Param: "password"},
			{Action: actions.KindClick, Target: &WorkflowTarget{Role: "button", Name: "Save"}, Compensation: "none needed", Outcome: &WorkflowOutcome{Type: "text_present", Value: "len9"}},
		}}
	if err := ValidateWorkflow(def, map[string]WorkflowInput{"password": {Value: "literal"}}); err == nil {
		t.Fatal("literal value accepted for a sensitive param")
	}
	run, taskErr := session.RunWorkflow(ctx, pageID, def, map[string]WorkflowInput{"password": {Credential: &CredentialRef{Profile: "ops", Domain: "127.0.0.1", Field: "password"}}}, WorkflowRunOptions{Credentials: store})
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if taskErr != nil || !run.Success || strings.Contains(string(encoded), "s3cret-pw") {
		t.Fatalf("credential workflow failed or leaked: %s %v", encoded, taskErr)
	}
	reload(t, session, pageID, server.URL)
	foreign, taskErr := session.RunWorkflow(ctx, pageID, def, map[string]WorkflowInput{"password": {Credential: &CredentialRef{Profile: "ops", Domain: "other.test", Field: "password"}}}, WorkflowRunOptions{Credentials: store})
	if taskErr != nil || foreign.Success || foreign.Steps[0].Status != WorkflowStepFailed || foreign.Steps[0].Effect == actions.EffectDispatched {
		t.Fatalf("credential typed into a foreign page: %+v %v", foreign, taskErr)
	}

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := NewAgent(AgentConfig{ConfirmActions: []string{"click"}, Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := guarded.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guarded.Stop(); err != nil {
			t.Error(err)
		}
	})
	guardedSession, err := guarded.CreateSession("guarded")
	if err != nil {
		t.Fatal(err)
	}
	guardedPage, _, taskErr := guardedSession.OpenChromiumPage(ctx, server.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	plain := WorkflowDefinition{Version: WorkflowVersion, Name: "save", Origins: []string{origin}, Steps: []WorkflowStep{
		{Action: actions.KindFill, Target: &WorkflowTarget{Role: "textbox", Name: "Name"}, Value: "Eve"},
		{Action: actions.KindClick, Target: &WorkflowTarget{Role: "button", Name: "Save"}, Compensation: "none needed"},
	}}
	if result, taskErr := guardedSession.RunWorkflow(ctx, guardedPage, plain, nil, WorkflowRunOptions{}); taskErr == nil || len(result.Steps) != 0 {
		t.Fatalf("workflow with an unapproved consequential step started: %+v %v", result, taskErr)
	}
	if text := pageText(t, guardedSession, guardedPage); strings.Contains(text, "Eve") {
		t.Fatal("effect happened before authority was denied")
	}
}

func TestWorkflowRecordingCapturesRealFallbackPixelsForAmbiguousSelectors(t *testing.T) {
	server := workflowServer(t)
	agent := startAdBlockAgent(t, server, nil, false)
	session, err := agent.CreateSession("workflow-pixels")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL+"/dup")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	config := observe.DefaultWorkflowCaptureConfig()
	config.ScreenshotFallback = true
	if taskErr := session.StartWorkflowRecording(pageID, "operator", "ambiguous", config); taskErr != nil {
		t.Fatal(taskErr)
	}
	saveRef, epoch := workflowNode(t, session, pageID, "button", "Save")
	if outcome, taskErr := session.ActChromiumPage(ctx, pageID, epoch, actions.Request{Kind: actions.KindClick, Ref: saveRef}); taskErr != nil || !outcome.Success {
		t.Fatalf("click: %+v %v", outcome, taskErr)
	}
	recording, taskErr := session.StopWorkflowRecording(pageID)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	action := recording.Actions[0]
	if action.Selectors[0].Confidence >= config.SelectorConfidenceThreshold || action.Screenshot == nil || len(action.Screenshot.DataB64) < 100 || action.Screenshot.Reason != "low_selector_confidence" {
		t.Fatalf("ambiguous selector recorded without real fallback pixels: %+v", action.Selectors)
	}
	if _, taskErr := session.StopWorkflowRecording(pageID); taskErr == nil {
		t.Fatal("a stopped recording could be stopped again")
	}
}
