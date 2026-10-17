package artemis

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestAgentHybridEscalationTransfersOwnedStateAndActualStatus(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/seed" {
			w.Header().Set("Set-Cookie", "route_owner=alice; Path=/")
			if _, err := fmt.Fprint(w, `<!doctype html><title>Seed</title><script>localStorage.setItem('route_owner','alice')</script><p>seeded</p>`); err != nil {
				t.Error(err)
			}
			return
		}
		w.WriteHeader(http.StatusAccepted)
		if _, err := fmt.Fprint(w, `<!doctype html><title>Escalated</title><p id="state"></p><script>document.querySelector('#state').textContent = document.cookie + ':' + localStorage.getItem('route_owner') + ':' + document.body.getBoundingClientRect().width</script>`); err != nil {
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
	alice, err := agent.CreateSession("alice")
	if err != nil {
		t.Fatal(err)
	}
	seed := agent.ExecuteTask(t.Context(), Task{ID: "seed", SessionID: alice.SessionID(), Action: FetchAction{URL: fixture.URL + "/seed", RunScripts: true}})
	if !seed.Success {
		t.Fatalf("renderless seed: %+v", seed)
	}
	result := agent.ExecuteTask(t.Context(), Task{ID: "layout", SessionID: alice.SessionID(), Action: FetchAction{URL: fixture.URL + "/state", RequiredWebAPIs: []string{"getBoundingClientRect"}, NeedsRealSemantics: true}})
	if !result.Success || result.Data == nil || result.Data.StatusCode != http.StatusAccepted || !strings.Contains(result.Data.Markdown, "route_owner=alice:alice:") {
		t.Fatalf("owned escalation or actual status failed: success=%v code=%s error=%s data=%+v", result.Success, result.ErrorCode, result.Error, result.Data)
	}
	if !agent.Health().Chromium.Healthy {
		t.Fatalf("CDP health disagrees: %+v", agent.Health())
	}
	if alice.TabCount() != 0 {
		t.Fatalf("route page leaked: %d", alice.TabCount())
	}
	denied := agent.ExecuteTask(t.Context(), Task{ID: "downgrade", SessionID: alice.SessionID(), Action: FetchAction{URL: fixture.URL, Mode: "static_fetch"}})
	if denied.Success || denied.ErrorCode != TaskErrorPolicyDenied {
		t.Fatalf("implicit identity downgrade admitted: %+v", denied)
	}
	bob, err := agent.CreateSession("bob")
	if err != nil {
		t.Fatal(err)
	}
	other := agent.ExecuteTask(t.Context(), Task{ID: "bob-layout", SessionID: bob.SessionID(), Action: FetchAction{URL: fixture.URL + "/state", RequiredWebAPIs: []string{"getBoundingClientRect"}}})
	if !other.Success || other.Data == nil || strings.Contains(other.Data.Text, "alice") {
		t.Fatalf("state crossed owned route: %+v", other)
	}
}

func TestAgentHybridKeepsPolicyDenialPrimary(t *testing.T) {
	agent, err := NewAgent(AgentConfig{Chromium: &browserprocess.LaunchConfig{BinaryPath: "/missing/chrome", Headless: true}})
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
	result := agent.ExecuteTask(t.Context(), Task{ID: "private", SessionID: session.SessionID(), Action: FetchAction{URL: "http://127.0.0.1"}})
	if result.Success || result.ErrorCode != TaskErrorPolicyDenied || !strings.Contains(result.Error, "network policy denied") || strings.Contains(result.Error, "chromium_launch") {
		t.Fatalf("policy denial was replaced or retried: %+v", result)
	}
}
