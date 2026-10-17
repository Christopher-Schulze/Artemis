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
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

type assertionFixture struct {
	agent   *Agent
	session *Session
	pageID  string
	page    *engine.Page
	url     string
}

func newAssertionFixture(t *testing.T, config AgentConfig) assertionFixture {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if request.URL.Path == "/alternate" {
			w.WriteHeader(http.StatusCreated)
			if _, err := fmt.Fprint(w, `<html><head><title>Alternate title</title></head><body><p id="revision">Alternate body</p></body></html>`); err != nil {
				t.Error(err)
			}
			return
		}
		if _, err := fmt.Fprint(w, `<html><head><title>Measured title</title></head><body><p id="notice">Measured body</p></body></html>`); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(address.Port())
	if err != nil {
		t.Fatal(err)
	}
	config.PolicyConfig = network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}
	agent, err := NewAgent(config)
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
	session, err := agent.CreateSession("assertion-owner")
	if err != nil {
		t.Fatal(err)
	}
	pageURL := server.URL + "/proof"
	pageID, page, taskErr := session.OpenPage(t.Context(), pageURL, false)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	return assertionFixture{agent: agent, session: session, pageID: pageID, page: page, url: pageURL}
}

func TestReaderAssertionsUseActualDocumentWithoutEvaluationGrant(t *testing.T) {
	f := newAssertionFixture(t, AgentConfig{})
	for _, test := range []struct {
		mode      string
		selector  string
		substring string
		got       string
	}{
		{"selector_exists", "#notice", "", "exists=true"},
		{"title_contains", "", "Measured title", "Measured title"},
		{"text_contains", "", "Measured body", "Measured body"},
		{"url_contains", "", "/proof", f.url},
		{"status_eq", "", "", "200"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			result, err := f.session.Assert(t.Context(), f.pageID, test.mode, test.selector, test.substring, "", 200, nil)
			if err != nil || !result.Pass || !strings.Contains(result.Got, test.got) {
				t.Fatalf("real positive predicate: %+v %v", result, err)
			}
			result, err = f.session.Assert(t.Context(), f.pageID, test.mode, "#absent", "Absent marker", "", 503, nil)
			if err != nil || result.Pass {
				t.Fatalf("real negative predicate: %+v %v", result, err)
			}
			if test.mode == "status_eq" {
				if result.Got != "200" {
					t.Fatalf("actual status lost: %+v", result)
				}
				return
			}
			want := false
			result, err = f.session.Assert(t.Context(), f.pageID, test.mode, "#absent", "Absent marker", "", 0, &want)
			if err != nil || !result.Pass {
				t.Fatalf("negative expectation: %+v %v", result, err)
			}
		})
	}
}

const assertionMutation = `(globalThis.assertEffects=(globalThis.assertEffects||0)+1)>0`

func TestReaderAssertionsObserveTheSelectedPageRatherThanCachedValues(t *testing.T) {
	f := newAssertionFixture(t, AgentConfig{})
	pageURL := strings.TrimSuffix(f.url, "/proof") + "/alternate"
	pageID, _, taskErr := f.session.OpenPage(t.Context(), pageURL, false)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	for _, test := range []struct {
		mode      string
		selector  string
		substring string
		status    int
		got       string
	}{
		{"selector_exists", "#revision", "", 0, "exists=true"},
		{"title_contains", "", "Alternate title", 0, "Alternate title"},
		{"text_contains", "", "Alternate body", 0, "Alternate body"},
		{"url_contains", "", "/alternate", 0, pageURL},
		{"status_eq", "", "", http.StatusCreated, "201"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			result, err := f.session.Assert(t.Context(), pageID, test.mode, test.selector, test.substring, "", test.status, nil)
			if err != nil || !result.Pass || !strings.Contains(result.Got, test.got) {
				t.Fatalf("selected alternate document: %+v %v", result, err)
			}
			result, err = f.session.Assert(t.Context(), pageID, test.mode, "#notice", "Measured", "", 200, nil)
			if err != nil || result.Pass {
				t.Fatalf("stale values from another page: %+v %v", result, err)
			}
		})
	}
}

func assertEffectCount(t *testing.T, page *engine.Page, want string) {
	t.Helper()
	value, err := page.Eval(t.Context(), `String(globalThis.assertEffects||0)`)
	if err != nil || value == nil || value.String() != want {
		t.Fatalf("actual V8 effect count: %v %v; want %s", value, err, want)
	}
}

func TestEvaluationAssertionsRetainDenyConfirmationAndExplicitGrant(t *testing.T) {
	for _, test := range []struct {
		name     string
		policy   string
		confirm  []string
		decision bridge.PolicyDecision
	}{
		{"reader default", "", nil, bridge.PolicyDecisionDeny},
		{"deny precedes grant", `{"default":"allow","deny":["evaluate"]}`, nil, bridge.PolicyDecisionDeny},
		{"confirmation precedes grant", `{"default":"allow"}`, []string{"evaluate"}, bridge.PolicyDecisionChallenge},
		{"explicit grant", `{"default":"deny","allow":["navigate","query","assert","evaluate","tab_close"]}`, nil, bridge.PolicyDecisionAllow},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := AgentConfig{ConfirmActions: test.confirm}
			if test.policy != "" {
				config.ActionPolicyPath = filepath.Join(t.TempDir(), "policy.json")
				if err := os.WriteFile(config.ActionPolicyPath, []byte(test.policy), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			f := newAssertionFixture(t, config)
			result, err := f.session.Assert(t.Context(), f.pageID, "eval_truthy", "", "", assertionMutation, 0, nil)
			if test.decision == bridge.PolicyDecisionAllow {
				if err != nil || !result.Pass || result.Got != "true" {
					t.Fatalf("granted real evaluation: %+v %v", result, err)
				}
				assertEffectCount(t, f.page, "1")
				return
			}
			var decision *bridge.PolicyError
			if err == nil || err.Code != TaskErrorPolicyDenied || !errors.As(err, &decision) || decision.Decision != test.decision {
				t.Fatalf("deny/confirmation changed: %+v %v", result, err)
			}
			assertEffectCount(t, f.page, "0")
		})
	}
}

func TestAssertionsRejectInvalidInputAndForeignOrClosedPagesBeforeEffects(t *testing.T) {
	f := newAssertionFixture(t, AgentConfig{})
	for _, mode := range []string{"selector_exists", "title_contains", "text_contains", "url_contains", "status_eq", "unknown"} {
		_, err := f.session.Assert(t.Context(), f.pageID, mode, "#notice", "Measured", assertionMutation, 200, nil)
		if err == nil || err.Code != TaskErrorInvalidInput {
			t.Fatalf("caller code disguised as %s: %v", mode, err)
		}
	}
	for _, test := range []struct {
		ctx  context.Context
		mode string
	}{
		{nil, "title_contains"}, {t.Context(), "eval_truthy"},
	} {
		_, err := f.session.Assert(test.ctx, f.pageID, test.mode, "", "Measured", "", 200, nil)
		if err == nil || err.Code != TaskErrorInvalidInput {
			t.Fatalf("invalid request admitted: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, taskErr := f.session.Assert(ctx, f.pageID, "title_contains", "", "Measured", "", 0, nil)
	if taskErr == nil || taskErr.Code != TaskErrorCancelled {
		t.Fatalf("canceled assertion: %v", taskErr)
	}
	other, err := f.agent.CreateSession("another-owner")
	if err != nil {
		t.Fatal(err)
	}
	_, taskErr = other.Assert(t.Context(), f.pageID, "title_contains", "", "Measured", "", 0, nil)
	if taskErr == nil || taskErr.Code != TaskErrorPageNotFound {
		t.Fatalf("foreign page admitted: %v", taskErr)
	}
	assertEffectCount(t, f.page, "0")
	if err := f.session.ClosePage(f.pageID); err != nil {
		t.Fatal(err)
	}
	_, taskErr = f.session.Assert(t.Context(), f.pageID, "title_contains", "", "Measured", "", 0, nil)
	if taskErr == nil || taskErr.Code != TaskErrorPageNotFound {
		t.Fatalf("closed page admitted: %v", taskErr)
	}
	if err := f.agent.CloseSession(f.session.SessionID()); err != nil {
		t.Fatal(err)
	}
	_, taskErr = f.session.Assert(t.Context(), f.pageID, "title_contains", "", "Measured", "", 0, nil)
	if taskErr == nil || taskErr.Code != TaskErrorStaleReference {
		t.Fatalf("closed session admitted: %v", taskErr)
	}
}

func TestReaderAssertionsRejectTheActualPageOriginAfterPolicyReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	publishBrowserPolicy(t, path, `{"default":"deny","allow":["navigate","query","tab_close"]}`)
	f := newAssertionFixture(t, AgentConfig{ActionPolicyPath: path})
	publishBrowserPolicy(t, path, `{"default":"deny","allow":["navigate","query","tab_close"],"domain_allowlist":["example.invalid"]}`)
	waitBrowserPolicy(t, func() bool { return f.agent.Health().ActionPolicy.Reloads == 1 })
	for _, mode := range []string{"selector_exists", "title_contains", "text_contains", "url_contains", "status_eq"} {
		_, err := f.session.Assert(t.Context(), f.pageID, mode, "#notice", "Measured", "", 200, nil)
		if err == nil || err.Code != TaskErrorPolicyDenied {
			t.Fatalf("%s ignored actual page origin %s: %v", mode, f.url, err)
		}
	}
	assertEffectCount(t, f.page, "0")
}
