package policy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNavigationPolicyActionPrecedenceAndAtomicTargetDecision(t *testing.T) {
	policy, err := NewStaticNavigationPolicy([]byte(`{"allow":["click","type","danger","submit"],"deny":["danger"],"confirm":["submit"],"domain_allowlist":["EXAMPLE.COM"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	cases := []struct {
		action, url string
		want        PolicyDecision
	}{
		{"click", "https://example.com/", PolicyDecisionAllow},
		{"TYPE", "https://nested.example.com/", PolicyDecisionAllow},
		{"danger", "https://example.com/", PolicyDecisionDeny},
		{"submit", "https://example.com/", PolicyDecisionChallenge},
		{"navigate", "https://example.com/", PolicyDecisionDeny},
		{"click", "https://evil-example.com/", PolicyDecisionDeny},
		{"click", "https://example.com.evil/", PolicyDecisionDeny},
		{"click", "https://user@example.com/", PolicyDecisionDeny},
		{"click", "", PolicyDecisionDeny},
	}
	for _, test := range cases {
		response := mustEvaluateNavigationPolicy(t, policy, context.Background(), PolicyRequest{Action: test.action, URL: test.url})
		if response.Decision != test.want {
			t.Errorf("%s %s: %+v, want %s", test.action, test.url, response, test.want)
		}
	}
	copy := policy.CurrentPolicy()
	copy.Allow[0], copy.Deny[0], copy.Confirm[0], copy.DomainAllowlist[0] = "navigate", "click", "type", "evil.com"
	fresh := policy.CurrentPolicy()
	if fresh.Allow[0] != "click" || fresh.Deny[0] != "danger" || fresh.Confirm[0] != "submit" || fresh.DomainAllowlist[0] != "example.com" {
		t.Fatal("exported arrays corrupted the active policy document")
	}
	if policy.EvaluateAction("click").Decision != PolicyDecisionAllow || policy.EvaluateAction("danger").Decision != PolicyDecisionDeny || policy.EvaluateAction("submit").Decision != PolicyDecisionChallenge || policy.EvaluateDomain("example.com").Decision != PolicyDecisionAllow {
		t.Fatal("exported policy arrays mutated the active decision")
	}
	policy.StartHotReload()
	if policy.started || policy.Reload() == nil {
		t.Fatal("static policy acquired a file-reload lifecycle")
	}
}

func TestNavigationPolicyURLGrantCannotAuthorizeUnlistedAction(t *testing.T) {
	policy, err := NewStaticNavigationPolicy([]byte(`{"version":1,"default":"allow","rules":[{"action":"allow","pattern":"https://localhost"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	for _, test := range []struct {
		action string
		want   PolicyDecision
	}{{"navigate", PolicyDecisionAllow}, {"click", PolicyDecisionDeny}, {"evaluate", PolicyDecisionDeny}} {
		response := mustEvaluateNavigationPolicy(t, policy, context.Background(), PolicyRequest{Action: test.action, URL: "https://localhost/"})
		if response.Decision != test.want {
			t.Fatalf("URL-only grant authorized %s: %+v", test.action, response)
		}
	}
	combined, err := NewStaticNavigationPolicy([]byte(`{"version":1,"default":"deny","allow":["click"],"rules":[{"action":"allow","pattern":"https://localhost/safe"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer combined.Stop()
	if response := mustEvaluateNavigationPolicy(t, combined, context.Background(), PolicyRequest{Action: "click", URL: "https://localhost/safe"}); response.Decision != PolicyDecisionAllow {
		t.Fatal(response)
	}
	if response := mustEvaluateNavigationPolicy(t, combined, context.Background(), PolicyRequest{Action: "click", URL: "https://localhost/private"}); response.Decision != PolicyDecisionDeny {
		t.Fatal("category bypassed a URL deny")
	}
}

func TestNavigationPolicyCanonicalJSONRoundTripPreservesEmptyListMeaning(t *testing.T) {
	for _, content := range []string{
		`{"version":1,"default":"allow","rules":[]}`,
		`{"version":1,"default":"allow","rules":[],"allow":[],"deny":[],"confirm":[]}`,
		`{"default":"allow"}`,
		`{"allow":[" CLICK ","click"],"domain_allowlist":["example.com"]}`,
	} {
		policy, err := NewStaticNavigationPolicy([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(policy.CurrentPolicy())
		if err != nil {
			t.Fatal(err)
		}
		copy, err := NewStaticNavigationPolicy(encoded)
		if err != nil {
			t.Fatalf("round trip %s: %s %v", content, encoded, err)
		}
		for _, action := range []string{"click", "navigate", "submit"} {
			request := PolicyRequest{Action: action, URL: "https://example.com/"}
			before := mustEvaluateNavigationPolicy(t, policy, context.Background(), request)
			after := mustEvaluateNavigationPolicy(t, copy, context.Background(), request)
			if before.Decision != after.Decision {
				t.Fatalf("round trip changed %s authorization: %s -> %s", action, content, encoded)
			}
		}
		policy.Stop()
		copy.Stop()
	}
}

func TestNavigationPolicyActionAndDomainValidation(t *testing.T) {
	for _, content := range []string{
		`{"allow":[""]}`, `{"allow":["click;eval"]}`, `{"allow":["click\nother"]}`,
		`{"allow":null}`, `{"allow":["click"],"Allow":["evaluate"]}`,
		`{"domain_allowlist":["https://example.com"]}`, `{"domain_allowlist":["*.example.com"]}`,
		`{"domain_allowlist":["example..com"]}`, `{"domain_allowlist":["example.com."]}`,
		`{"version":0}`, `{"allow":["` + strings.Repeat("x", maxPolicyCategoryBytes+1) + `"]}`,
		`{"allow":[` + strings.Repeat(`"click",`, maxNavigationPolicyRules) + `"click"]}`,
	} {
		if policy, err := NewStaticNavigationPolicy([]byte(content)); err == nil || policy != nil {
			t.Fatalf("invalid policy published: %s", content)
		}
	}
	for _, raw := range []string{"click,***", strings.Repeat("x", maxPolicyCategoryBytes+1), strings.Repeat("x", maxNavigationPolicyRules*(maxPolicyCategoryBytes+1)+1)} {
		if _, err := ParseConfirmActions(raw); err == nil {
			t.Fatal("invalid environment value admitted")
		}
	}
	confirm, err := ParseConfirmActions(" navigate, CLICK, click, ,fill ")
	if err != nil || strings.Join(confirm, ",") != "navigate,click,fill" {
		t.Fatalf("confirm normalization: %v %v", confirm, err)
	}
	policy, err := NewStaticNavigationPolicy([]byte(`{"allow":[],"deny":[],"confirm":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if policy.EvaluateAction("anything").Decision != PolicyDecisionDeny {
		t.Fatal("empty action lists failed open")
	}
	policy.Stop()
	if policy.EvaluateAction("anything").Decision != PolicyDecisionDeny || policy.EvaluateDomain("example.com").Decision != PolicyDecisionDeny {
		t.Fatal("stopped metadata evaluation admitted permission")
	}
}

func TestNavigationPolicyCallerCodeCannotBypassEvaluationConstraint(t *testing.T) {
	for _, test := range []struct {
		content string
		want    PolicyDecision
	}{
		{`{"allow":["assert","frame_evaluate"],"deny":["evaluate"]}`, PolicyDecisionDeny},
		{`{"allow":["assert","frame_evaluate"],"confirm":["evaluate"]}`, PolicyDecisionChallenge},
		{`{"allow":["assert","frame_evaluate","evaluate"]}`, PolicyDecisionAllow},
	} {
		policy, err := NewStaticNavigationPolicy([]byte(test.content))
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"assert", "frame_evaluate"} {
			response := mustEvaluateNavigationPolicy(t, policy, t.Context(), PolicyRequest{Action: action, URL: "https://example.com"})
			if response.Decision != test.want {
				t.Fatalf("%s bypassed caller-code constraint: %+v", action, response)
			}
		}
		policy.Stop()
	}
}
