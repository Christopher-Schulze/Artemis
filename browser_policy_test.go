package artemis

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestBrowserPolicyDefaultAndConfirmationAreNonPermissive(t *testing.T) {
	policy, err := NewBrowserPolicy(t.Context(), AgentConfig{ConfirmActions: []string{"navigate"}})
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	for _, test := range []struct {
		action string
		want   bridge.PolicyDecision
	}{
		{"navigate", bridge.PolicyDecisionChallenge}, {"query", bridge.PolicyDecisionAllow}, {"click", bridge.PolicyDecisionDeny}, {"evaluate", bridge.PolicyDecisionDeny}, {"assert", bridge.PolicyDecisionDeny},
	} {
		response, err := policy.Evaluate(t.Context(), bridge.PolicyRequest{Action: test.action, URL: "https://example.com"})
		if err != nil || response.Decision != test.want {
			t.Fatalf("default %s: %+v %v", test.action, response, err)
		}
	}
	policy.Stop()
	response, err := policy.Evaluate(t.Context(), bridge.PolicyRequest{Action: "query", URL: "https://example.com"})
	if err == nil || response.Decision != bridge.PolicyDecisionDeny {
		t.Fatal("stopped policy admitted a request")
	}
}

func TestBrowserPolicyConfiguredMissingFileCannotUseDefaultProfile(t *testing.T) {
	policy, err := NewBrowserPolicy(t.Context(), AgentConfig{ActionPolicyPath: filepath.Join(t.TempDir(), "missing.json")})
	if err == nil || policy != nil {
		t.Fatal("configured missing file fell back to implicit navigation permission")
	}
}

func TestBrowserPolicyZeroValueCannotAuthorizeOrReportReady(t *testing.T) {
	policy := &BrowserPolicy{}
	response, err := policy.Evaluate(t.Context(), bridge.PolicyRequest{Action: "click", URL: "https://example.com"})
	if err == nil || response.Decision != bridge.PolicyDecisionDeny || policy.Health().Ready {
		t.Fatalf("uninitialized authority failed open: %+v %v %+v", response, err, policy.Health())
	}
	policy.Stop()
}

// Existing behavioral fixtures explicitly delegate operations on their own DOM.
func fixtureActionPolicy(t *testing.T) *bridge.NavigationPolicy {
	t.Helper()
	policy, err := bridge.NewStaticNavigationPolicy([]byte(`{"default":"allow"}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(policy.Stop)
	return policy
}

func publishBrowserPolicy(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.CreateTemp(filepath.Dir(path), "action-policy-")
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file.Name(), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserPolicyLiveReloadControlsActualOnceOnlyClick(t *testing.T) {
	var navigationRequests atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/next" {
			navigationRequests.Add(1)
		}
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, `<!doctype html><button onclick="window.clicks=(window.clicks||0)+1;document.querySelector('p').textContent=String(window.clicks)">Apply</button><p>0</p>`); err != nil {
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
	path := filepath.Join(t.TempDir(), "policy.json")
	publishBrowserPolicy(t, path, `{"allow":["navigate","click"],"deny":["click"]}`)
	agent, err := NewAgent(AgentConfig{ActionPolicyPath: path, Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
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
	session, err := agent.CreateSession("policy-owner")
	if err != nil {
		t.Fatal(err)
	}
	pageID, evidence, taskErr := session.OpenChromiumPage(t.Context(), fixture.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	ref := ""
	for _, node := range evidence.Snapshot.Nodes {
		if node.Role == "button" {
			ref = node.Ref
		}
	}
	if ref == "" {
		t.Fatal("real button reference missing")
	}
	assertClick := func(want bool, decision bridge.PolicyDecision) {
		t.Helper()
		outcome, taskErr := session.ActChromiumPage(t.Context(), pageID, evidence.Snapshot.Epoch, actions.Request{Kind: actions.KindClick, Ref: ref, RetryMax: 3, Idempotent: true})
		if taskErr != nil || outcome.Success != want || outcome.Evidence.PolicyDecision != decision {
			t.Fatalf("actual action policy outcome: %+v %v", outcome, taskErr)
		}
		if !want && (outcome.Evidence.Effect != actions.EffectNotAttempted || outcome.Evidence.Attempts != 0) {
			t.Fatalf("denied action produced an effect receipt: %+v", outcome.Evidence)
		}
		if want && (outcome.Evidence.Attempts != 1 || outcome.Evidence.Effect != actions.EffectDispatched) {
			t.Fatalf("granted action replayed or lost its effect: %+v", outcome.Evidence)
		}
	}
	assertClick(false, bridge.PolicyDecisionDeny)
	publishBrowserPolicy(t, path, `{"allow":["navigate","click"]}`)
	waitBrowserPolicy(t, func() bool { return agent.Health().ActionPolicy.Reloads == 1 })
	assertClick(true, bridge.PolicyDecisionAllow)
	publishBrowserPolicy(t, path, `{"default":"invalid"}`)
	waitBrowserPolicy(t, func() bool { return agent.Health().ActionPolicy.LastReloadError != "" })
	assertClick(true, bridge.PolicyDecisionAllow)
	health := agent.Health().ActionPolicy
	if !health.Ready || !health.FileConfigured || health.HostConfigured || health.Reloads != 1 || health.ReloadErrors != 1 || strings.Contains(health.LastReloadError, path) || len(health.LastReloadError) > 256 {
		t.Fatalf("bounded public policy health/accounting: %+v", health)
	}
	publishBrowserPolicy(t, path, `{"allow":["navigate","click"],"deny":["navigate"]}`)
	waitBrowserPolicy(t, func() bool { return agent.Health().ActionPolicy.Reloads == 2 })
	navigation, taskErr := session.ActChromiumPage(t.Context(), pageID, 0, actions.Request{Kind: actions.KindNavigate, URL: fixture.URL + "/next"})
	if taskErr != nil || navigation.Success || navigation.Evidence.PolicyDecision != bridge.PolicyDecisionDeny || navigation.Evidence.Attempts != 0 || navigation.Evidence.Effect != actions.EffectNotAttempted || navigationRequests.Load() != 0 {
		t.Fatalf("reloaded navigation deny reached a real effect: %+v %v requests=%d", navigation, taskErr, navigationRequests.Load())
	}
	owned, release, taskErr := session.leaseChromiumPage(t.Context(), pageID)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	defer release.Release()
	var counter struct {
		Result struct {
			Value int `json:"value"`
		} `json:"result"`
	}
	if err := owned.page.Call(t.Context(), "Runtime.evaluate", struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{"window.clicks", true}, &counter); err != nil {
		t.Fatal(err)
	}
	if counter.Result.Value != 2 {
		t.Fatalf("native counter=%d, want exactly two granted clicks", counter.Result.Value)
	}
	health = agent.Health().ActionPolicy
	if health.Reloads != 2 || health.ReloadErrors != 1 || health.LastReloadError != "" {
		t.Fatalf("valid replacement did not clear failure or published more than once: %+v", health)
	}
}

func waitBrowserPolicy(t *testing.T, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal("live policy transition timed out")
		case <-ticker.C:
		}
	}
}

func TestAgentPolicyRejectsValueAndPointerFetchBeforeActualNetwork(t *testing.T) {
	var requests atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if _, err := fmt.Fprint(w, "<title>Must not fetch</title>"); err != nil {
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
	policyPath := filepath.Join(t.TempDir(), "deny.json")
	publishBrowserPolicy(t, policyPath, `{"default":"deny"}`)
	agent, err := NewAgent(AgentConfig{ActionPolicyPath: policyPath, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
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
	session, err := agent.CreateSession("policy-fetch")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []Action{FetchAction{URL: fixture.URL}, &FetchAction{URL: fixture.URL}} {
		result := agent.ExecuteTask(t.Context(), Task{ID: "denied-fetch", SessionID: session.SessionID(), Action: action})
		if result.Success || result.ErrorCode != TaskErrorPolicyDenied || requests.Load() != 0 {
			t.Fatalf("typed action escaped policy: %+v requests=%d", result, requests.Load())
		}
	}
	var missing *FetchAction
	result := agent.ExecuteTask(t.Context(), Task{ID: "nil-fetch", SessionID: session.SessionID(), Action: missing})
	if result.Success || result.ErrorCode != TaskErrorInvalidInput || requests.Load() != 0 {
		t.Fatalf("nil action bypassed validation: %+v", result)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	result = agent.ExecuteTask(cancelled, Task{ID: "cancelled-fetch", SessionID: session.SessionID(), Action: &FetchAction{URL: fixture.URL}})
	if result.Success || result.ErrorCode != TaskErrorCancelled || requests.Load() != 0 {
		t.Fatalf("policy hid cancellation or reached network: %+v", result)
	}
}

func TestManagedRenderlessPolicyBlocksCallerEffectsAndPermitsSiteScripts(t *testing.T) {
	var requests atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if _, err := fmt.Fprint(w, `<!doctype html><title>Original</title><button>Apply</button><input id="input" value="original"><script>document.querySelector('input').setAttribute('value','site code')</script>`); err != nil {
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
	agent, err := NewAgent(AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
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
	session, err := agent.CreateSession("renderless-policy")
	if err != nil {
		t.Fatal(err)
	}
	pageID, page, taskErr := session.OpenPage(t.Context(), fixture.URL, true)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	input, queryErr := page.Document().QuerySelector("#input")
	if queryErr != nil || input == nil || input.AttrOrEmpty("value") != "site code" || requests.Load() != 1 {
		t.Fatalf("actual site script did not mutate its DOM: query=%v requests=%d html=%s", queryErr, requests.Load(), page.HTML())
	}
	if _, err := page.Eval(t.Context(), `document.querySelector('button').addEventListener('click', function(){document.querySelector('input').setAttribute('value','clicked')})`); err != nil {
		t.Fatal(err)
	}
	before := page.HTML()
	tests := []struct {
		name string
		call func() *TaskError
	}{
		{"evaluate", func() *TaskError {
			_, err := session.Eval(t.Context(), pageID, `document.querySelector('input').setAttribute('value','evaluated')`)
			return err
		}},
		{"click", func() *TaskError { return session.ClickByText(t.Context(), pageID, "Apply") }},
		{"type", func() *TaskError { return session.Type(pageID, "#input", "changed") }},
		{"assert", func() *TaskError {
			_, err := session.Assert(t.Context(), pageID, "eval_truthy", "", "", `document.querySelector('input').setAttribute('value','asserted');true`, 0, nil)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil || err.Code != TaskErrorPolicyDenied || page.HTML() != before {
				t.Fatalf("managed operation escaped policy or changed DOM: %v", err)
			}
		})
	}
	if _, err := session.Dump(pageID, "html"); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitIdle(t.Context(), pageID); err != nil {
		t.Fatal(err)
	}
	if err := session.ClosePage(pageID); err != nil || session.Page(pageID) != nil || session.TabCount() != 0 {
		t.Fatalf("owned cleanup blocked or leaked: %v", err)
	}
}
