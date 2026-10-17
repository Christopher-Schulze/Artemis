package corpus

import (
	"context"
	"errors"
	"strings"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

func TestRunServeActualPinnedHTTPAndEvaluation(t *testing.T) {
	manifests, data, err := DefaultAssets("serve")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id     string
		status int
	}{{"html-001", 200}, {"js-001", 200}, {"frame-001", 200}, {"auth-basic-001", 401}, {"challenge-001", 403}, {"crash-001", 500}} {
		t.Run(test.id, func(t *testing.T) {
			for _, manifest := range manifests {
				if !strings.HasSuffix(manifest.ID, ":"+test.id) {
					continue
				}
				result, err := RunServe(t.Context(), manifest, data)
				if err != nil || !result.Passed || result.StatusCode != test.status || result.FixtureManifest.ID != manifest.ID || result.HTML == "" {
					t.Fatalf("actual Serve outcome %s: status=%d title=%q passed=%t err=%v", test.id, result.StatusCode, result.Title, result.Passed, err)
				}
				return
			}
			t.Fatal("required actual source case missing")
		})
	}
}

func TestRunServeRejectsUnownedChangedAndForeignInput(t *testing.T) {
	manifests, data, err := DefaultAssets("serve")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunServe(nil, manifests[0], data); err == nil || result.Passed {
		t.Fatal("missing owning context acquired coverage")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := RunServe(ctx, manifests[0], data); !errors.Is(err, context.Canceled) || result.Passed || result.StatusCode != 0 {
		t.Fatal("cancelled owner acquired an observation")
	}
	changed := append([]byte{}, data...)
	changed[0] ^= 1
	if result, err := RunServe(t.Context(), manifests[0], changed); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("changed source acquired an observation")
	}
	foreign, source, err := DefaultAssets("agent")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunServe(t.Context(), foreign[0], source); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("foreign runner silently fell back")
	}
}

func TestServeControlCleanupReleasesActualSessions(t *testing.T) {
	manifests, data, err := DefaultAssets("serve")
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := fixture.CorpusScenario(manifests[0], data)
	if err != nil {
		t.Fatal(err)
	}
	server, err := fixture.NewServerFromCorpus(data)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	policy, err := corpusNetworkPolicy(server.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := artemis.NewAgent(artemis.AgentConfig{MaxSessions: 1, PolicyConfig: policy})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Stop(); err != nil {
			t.Error(err)
		}
	}()
	for range 2 {
		result, err := runControlServe(t.Context(), owner, manifests[0], scenario, server)
		if err != nil || result.StatusCode != 200 || len(owner.ListSessions()) != 0 {
			t.Fatalf("actual control cleanup retained ownership: status=%d sessions=%d err=%v", result.StatusCode, len(owner.ListSessions()), err)
		}
	}
}

func TestServeFixtureActionPolicyOnlyGrantsItsOriginAndRequiredActions(t *testing.T) {
	policy, err := corpusActionPolicy("http://127.0.0.1:12345")
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	for _, test := range []struct {
		action, target string
		want           bridge.PolicyDecision
	}{
		{"navigate", "http://127.0.0.1:12345/html-001", bridge.PolicyDecisionAllow},
		{"evaluate", "http://127.0.0.1:12345/js-001", bridge.PolicyDecisionAllow},
		{"wait", "http://127.0.0.1:12345/spa-001", bridge.PolicyDecisionAllow},
		{"extract", "http://127.0.0.1:12345/html-001", bridge.PolicyDecisionAllow},
		{"tab_close", "http://127.0.0.1:12345/html-001", bridge.PolicyDecisionAllow},
		{"click", "http://127.0.0.1:12345/html-001", bridge.PolicyDecisionDeny},
		{"evaluate", "http://127.0.0.1:54321/js-001", bridge.PolicyDecisionDeny},
		{"evaluate", "http://127.0.0.10:12345/js-001", bridge.PolicyDecisionDeny},
		{"evaluate", "https://127.0.0.1:12345/js-001", bridge.PolicyDecisionDeny},
		{"evaluate", "http://outside.invalid/path?next=http://127.0.0.1:12345/js-001", bridge.PolicyDecisionDeny},
		{"evaluate", "http://outside.invalid/http://127.0.0.1:12345/js-001", bridge.PolicyDecisionDeny},
	} {
		decision, err := policy.Evaluate(t.Context(), bridge.PolicyRequest{Action: test.action, URL: test.target})
		if err != nil || decision.Decision != test.want {
			t.Fatalf("fixture policy %s %s = %s, want %s: %v", test.action, test.target, decision.Decision, test.want, err)
		}
	}
}
