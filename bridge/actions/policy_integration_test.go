package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func verifyOOPIFPolicyContext(t *testing.T, ctx context.Context, page *bridge.Page, observer *bridgeobserve.Collector, ref, frameID, mainURL, childURL string) {
	t.Helper()
	for _, test := range []struct {
		name    string
		confirm []string
		rules   []bridge.PolicyRule
		want    bridge.PolicyDecision
	}{
		{"target realm denied", nil, []bridge.PolicyRule{{Action: bridge.PolicyFileAllow, Pattern: mainURL}}, bridge.PolicyDecisionDeny},
		{"confirmation", []string{"click"}, []bridge.PolicyRule{{Action: bridge.PolicyFileAllow, Pattern: mainURL}, {Action: bridge.PolicyFileAllow, Pattern: childURL}}, bridge.PolicyDecisionChallenge},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(bridge.PolicyFile{Version: 1, Default: bridge.PolicyFileDeny, Allow: []string{"click"}, Confirm: test.confirm, Rules: test.rules})
			if err != nil {
				t.Fatal(err)
			}
			policy, err := bridge.NewStaticNavigationPolicy(data)
			if err != nil {
				t.Fatal(err)
			}
			defer policy.Stop()
			runtime, err := NewRuntimeWithConfig(page, observer, nil, RuntimeConfig{PolicyEngine: policy})
			if err != nil {
				t.Fatal(err)
			}
			defer closeActionTestResource(t, "policy runtime", runtime.Close)
			request := Request{Kind: KindClick, Ref: ref, URL: mainURL, RetryMax: 3, Idempotent: true}
			contextRequest, err := runtime.PolicyRequest(ctx, request)
			if err != nil || len(contextRequest.TargetURLs) != 1 || contextRequest.TargetURLs[0] != childURL+"/" {
				t.Fatalf("native referenced realm lost: %+v %v", contextRequest, err)
			}
			outcome := runtime.Execute(ctx, request)
			if outcome.Success || outcome.Failure != FailurePolicy || outcome.Evidence.PolicyDecision != test.want || outcome.Evidence.Attempts != 0 || outcome.Evidence.Effect != EffectNotAttempted {
				t.Fatalf("OOPIF policy bypassed before native input: %+v", outcome)
			}
			if count := oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').dataset.count"); count != "1" {
				t.Fatalf("denied/challenged action changed counter: %v", count)
			}
		})
	}
	if _, err := page.FramePolicyURL(ctx, "unowned-frame"); err == nil {
		t.Fatal("foreign frame inherited the main page policy URL")
	}
}

func TestRuntimePolicyChecksActualHistoryAndSelectedTab(t *testing.T) {
	fixture := newActionFixture(t)
	ctx := t.Context()
	requireAction(t, fixture.runtime.Execute(ctx, Request{Kind: KindNavigate, URL: fixture.server.URL + "/second"}))
	data, err := json.Marshal(bridge.PolicyFile{Version: 1, Default: bridge.PolicyFileDeny,
		Allow: []string{"back", "tab_switch", "tab_close", "assert"},
		Rules: []bridge.PolicyRule{{Action: bridge.PolicyFileAllow, Pattern: fixture.server.URL + "/second"}}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := bridge.NewStaticNavigationPolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	runtime, err := NewRuntimeWithConfig(fixture.page, fixture.observer, nil, RuntimeConfig{PolicyEngine: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer closeActionTestResource(t, "history policy runtime", runtime.Close)
	before := oopifEvaluate(t, ctx, fixture.page, "", "location.href")
	outcome := runtime.Execute(ctx, Request{Kind: KindBack})
	if outcome.Success || outcome.Failure != FailurePolicy || outcome.Evidence.Effect != EffectNotAttempted || outcome.Evidence.Attempts != 0 || outcome.Evidence.PolicyDecision != bridge.PolicyDecisionDeny {
		t.Fatalf("history target bypassed its policy: %+v", outcome)
	}
	if after := oopifEvaluate(t, ctx, fixture.page, "", "location.href"); after != before {
		t.Fatalf("denied history changed document: %v -> %v", before, after)
	}
	opened := fixture.runtime.Execute(ctx, Request{Kind: KindTabOpen, URL: fixture.server.URL})
	requireAction(t, opened)
	for _, kind := range []Kind{KindTabSwitch, KindTabClose} {
		outcome := runtime.Execute(ctx, Request{Kind: kind, TargetID: opened.TargetID})
		if outcome.Success || outcome.Failure != FailurePolicy || outcome.Evidence.Effect != EffectNotAttempted || outcome.Evidence.Attempts != 0 {
			t.Fatalf("%s admitted a forbidden target: %+v", kind, outcome)
		}
	}
	if tab, ok, err := fixture.runtime.tabs.GetTabContext(ctx, opened.TargetID); err != nil || !ok || tab == nil {
		t.Fatalf("denied close lost the actual tab: %+v %v", tab, err)
	}
	outcome = runtime.Execute(ctx, Request{Kind: KindAssert, Expression: "document.body.dataset.policyEffect='changed';true"})
	if outcome.Success || outcome.Failure != FailurePolicy || outcome.Evidence.Attempts != 0 || outcome.Evidence.PolicyDecision != bridge.PolicyDecisionDeny {
		t.Fatalf("assertion bypassed caller evaluation permission: %+v", outcome)
	}
	if value := oopifEvaluate(t, ctx, fixture.page, "", "document.body.dataset.policyEffect || ''"); value != "" {
		t.Fatal("denied expression changed the actual document")
	}
}
