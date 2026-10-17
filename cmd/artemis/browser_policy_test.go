package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

func writeCLIActionPolicy(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIActionPolicyEnvironmentAndConfirmation(t *testing.T) {
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", "")
	t.Setenv("AGENT_BROWSER_POLICY", writeCLIActionPolicy(t, `{"allow":["navigate","click"],"deny":["evaluate"]}`))
	t.Setenv("AGENT_BROWSER_CONFIRM_ACTIONS", " Click,click ")
	flags := addBrowserPolicyFlags(flag.NewFlagSet("policy", flag.ContinueOnError))
	policy, err := flags.start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	for _, test := range []struct {
		action string
		want   bridge.PolicyDecision
	}{{"navigate", bridge.PolicyDecisionAllow}, {"click", bridge.PolicyDecisionChallenge}, {"evaluate", bridge.PolicyDecisionDeny}} {
		response, err := policy.Evaluate(t.Context(), bridge.PolicyRequest{Action: test.action, URL: "https://example.com"})
		if err != nil || response.Decision != test.want {
			t.Fatalf("%s: %+v %v", test.action, response, err)
		}
	}
}

func TestCLIActionPolicyDeniesBeforeNetworkOrBrowserLaunch(t *testing.T) {
	var requests atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeTestBody(t, w, "<title>Effect</title>")
	}))
	defer fixture.Close()
	script := filepath.Join(t.TempDir(), "script.js")
	if err := os.WriteFile(script, []byte(`document.body.setAttribute('data-effect','changed')`), 0o600); err != nil {
		t.Fatal(err)
	}
	deny := writeCLIActionPolicy(t, `{"default":"deny"}`)
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", "")
	t.Setenv("AGENT_BROWSER_POLICY", "")
	t.Setenv("AGENT_BROWSER_CONFIRM_ACTIONS", "")
	for _, test := range []struct {
		name string
		call func() int
	}{
		{"fetch", func() int {
			return cmdFetch([]string{"--action-policy", deny, "--allow-private-networks", "--allow-port", srvPort(fixture), fixture.URL})
		}},
		{"run default", func() int {
			return cmdRun([]string{"--script", script, "--allow-private-networks", "--allow-port", srvPort(fixture), fixture.URL})
		}},
		{"observe", func() int {
			return cmdObserve([]string{"--action-policy", deny, "--binary", "/nonexistent/browser", fixture.URL})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if code := test.call(); code != 1 || requests.Load() != 0 {
				t.Fatalf("denied command reached network: exit=%d requests=%d", code, requests.Load())
			}
		})
	}
}
