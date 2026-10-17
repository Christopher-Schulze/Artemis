package artemis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func newTaskEvaluationAgent(t *testing.T, origin string, policy bridge.PolicyEngine, native bool) (*Agent, *Session) {
	t.Helper()
	parsed, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	config := AgentConfig{ScriptTimeout: 2 * time.Second, FetchTimeout: 2 * time.Second, PolicyEngine: policy,
		PolicyConfig: network.PolicyConfig{AllowedDomains: []string{parsed.Hostname()}, AllowedPorts: []int{port}, AllowPrivateNetworks: true}}
	if native {
		// Native route admission uses ScriptTimeout for launch plus context
		// creation. Exercise the product defaults, not the short HTTP budget.
		config.ScriptTimeout, config.FetchTimeout = 0, 0
		config.Chromium = &browserprocess.LaunchConfig{Headless: true}
	}
	owner, err := NewAgent(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Stop(); err != nil {
			t.Error(err)
		}
	})
	session, err := owner.CreateSession("actual-evaluation-owner")
	if err != nil {
		t.Fatal(err)
	}
	return owner, session
}

func TestTaskEvaluationRechecksTheActualRedirectedURLBeforeEvaluation(t *testing.T) {
	var effects atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/target", http.StatusFound)
		case "/effect":
			effects.Add(1)
			if _, err := fmt.Fprint(w, "actual effect"); err != nil {
				t.Error(err)
			}
		default:
			if _, err := fmt.Fprint(w, "<!doctype html><p>actual redirected target</p>"); err != nil {
				t.Error(err)
			}
		}
	}))
	defer server.Close()
	encoded, err := json.Marshal(bridge.PolicyFile{Version: bridge.PolicyFileVersion, Default: bridge.PolicyFileDeny,
		Allow: []string{"navigate", "evaluate"}, Rules: []bridge.PolicyRule{{Action: bridge.PolicyFileAllow, Pattern: regexp.QuoteMeta(server.URL + "/start")}}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := bridge.NewStaticNavigationPolicy(encoded)
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	owner, session := newTaskEvaluationAgent(t, server.URL, policy, false)
	result := owner.ExecuteTask(t.Context(), Task{ID: "redirected-evaluation", SessionID: session.SessionID(),
		Action: FetchAction{URL: server.URL + "/start", EvaluateExpression: `(fetch('/effect'),'must not execute')`}})
	if result.Success || result.Data != nil || result.ErrorCode != TaskErrorPolicyDenied || effects.Load() != 0 {
		t.Fatalf("requested-URL permission authorized evaluation on a denied final page: code=%s effects=%d error=%s", result.ErrorCode, effects.Load(), result.Error)
	}
}

func TestTaskEvaluationConcurrentOwnedStringsRemainDetachedAcrossPageReuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marker, err := json.Marshal(r.URL.Query().Get("marker"))
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprintf(w, "<!doctype html><p>actual page</p><script>window.marker=%s</script>", marker); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	policy, err := bridge.NewStaticNavigationPolicy([]byte(`{"default":"deny","allow":["navigate","evaluate"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	owner, session := newTaskEvaluationAgent(t, server.URL, policy, false)
	failures := make(chan error, 4)
	for worker := range 4 {
		go func() {
			for iteration := range 4 {
				marker := fmt.Sprintf("owned-%d-%d", worker, iteration)
				result := owner.ExecuteTask(t.Context(), Task{ID: marker, SessionID: session.SessionID(),
					Action: FetchAction{URL: server.URL + "?marker=" + marker, RunScripts: true, EvaluateExpression: "window.marker"}})
				if !result.Success || result.Data == nil || result.Data.Evaluation == nil || *result.Data.Evaluation != marker {
					failures <- fmt.Errorf("owned value changed across concurrent contexts: %s: %s", marker, result.Error)
					return
				}
			}
			failures <- nil
		}()
	}
	for range 4 {
		if err := <-failures; err != nil {
			t.Error(err)
		}
	}
}

func TestTaskEvaluationWireUsesTheActualRetainedPageAndRefreshesItsSnapshot(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprintf(w, `<!doctype html><title>before</title><p id="out">before</p><script>window.answer='load-%d';</script>`, requests.Add(1)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	policy, err := bridge.NewStaticNavigationPolicy([]byte(`{"default":"deny","allow":["navigate","evaluate"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	owner, session := newTaskEvaluationAgent(t, server.URL, policy, false)
	for _, pointer := range []bool{false, true} {
		t.Run(fmt.Sprintf("pointer=%t", pointer), func(t *testing.T) {
			before := requests.Load()
			fetch := FetchAction{URL: server.URL, RunScripts: true, EvaluateExpression: `(document.title='evaluated',document.getElementById('out').textContent='after-eval',window.answer)`}
			var action Action = fetch
			if pointer {
				action = &fetch
			}
			encoded, err := json.Marshal(Task{ID: "actual-evaluation", SessionID: session.SessionID(), Action: action})
			if err != nil {
				t.Fatal(err)
			}
			var task Task
			if err := json.Unmarshal(encoded, &task); err != nil {
				t.Fatal(err)
			}
			decoded, ok := task.Action.(FetchAction)
			if !ok || decoded.EvaluateExpression != fetch.EvaluateExpression {
				t.Fatal("wire lost the requested exact expression")
			}
			result := owner.ExecuteTask(t.Context(), task)
			if !result.Success || result.Data == nil || result.Data.Evaluation == nil || *result.Data.Evaluation != fmt.Sprintf("load-%d", before+1) ||
				result.Data.Title != "evaluated" || result.Data.Text != "after-eval" || requests.Load() != before+1 {
				t.Fatalf("evaluation refetched, lost its real value or returned a stale snapshot: success=%t requests=%d error=%s", result.Success, requests.Load()-before, result.Error)
			}
		})
	}
}

func TestTaskEvaluationDefaultDenialAndInvalidExpressionsPrecedeHTTP(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if _, err := fmt.Fprint(w, "<!doctype html><title>actual</title><p>actual</p>"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	owner, session := newTaskEvaluationAgent(t, server.URL, nil, false)
	for _, expression := range []string{"1+1", " ", string([]byte{0xff}), "1\x00", strings.Repeat("x", maxTaskExpressionBytes+1)} {
		result := owner.ExecuteTask(t.Context(), Task{ID: "denied-evaluation", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, EvaluateExpression: expression}})
		want := TaskErrorInvalidInput
		if expression == "1+1" {
			want = TaskErrorPolicyDenied
		}
		if result.Success || result.Data != nil || result.ErrorCode != want || requests.Load() != 0 {
			t.Fatalf("evaluation denial/invalid input started HTTP or lost its category: code=%s requests=%d error=%s", result.ErrorCode, requests.Load(), result.Error)
		}
	}
	legacy := Task{ID: "legacy-fetch", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL}}
	encoded, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(encoded), "evaluateExpression") {
		t.Fatalf("absent expression changed the legacy task wire: %v", err)
	}
	result := owner.ExecuteTask(t.Context(), legacy)
	if !result.Success || result.Data == nil || result.Data.Evaluation != nil || requests.Load() != 1 {
		t.Fatalf("legacy navigation acquired evaluation or stopped working: code=%s error=%s", result.ErrorCode, result.Error)
	}
	output, err := json.Marshal(result.Data)
	if err != nil || strings.Contains(string(output), `"evaluation"`) {
		t.Fatalf("absent evaluation changed the legacy result wire: %v", err)
	}
}

func TestTaskEvaluationStringValuesAndActualErrorsStayExplicitAndBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprint(w, "<!doctype html><p>actual page</p>"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	policy, err := bridge.NewStaticNavigationPolicy([]byte(`{"default":"deny","allow":["navigate","evaluate"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	owner, session := newTaskEvaluationAgent(t, server.URL, policy, false)
	for _, test := range []struct {
		name, expression, expected, failure string
	}{
		{"empty", `''`, "", ""},
		{"undefined", `undefined`, "undefined", ""},
		{"number", `0.000001`, "0.000001", ""},
		{"object", `({toString(){return 'actual-object'}})`, "actual-object", ""},
		{"exact-bytes", `'x'.repeat(65536)`, strings.Repeat("x", maxTaskEvaluationBytes), ""},
		{"excess-characters", `'x'.repeat(65537)`, "", "exceeds limit"},
		{"excess-utf8", `'ä'.repeat(40000)`, "", "UTF-8 byte limit"},
		{"exception", `(()=>{throw new Error('actual-eval-failure')})()`, "", "actual-eval-failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := owner.ExecuteTask(t.Context(), Task{ID: test.name, SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, EvaluateExpression: test.expression}})
			if test.failure != "" {
				if result.Success || result.Data != nil || result.ErrorCode != TaskErrorExecutionFailed || !strings.Contains(result.Error, test.failure) {
					t.Fatalf("actual evaluation failure was lost: success=%t code=%s error=%s", result.Success, result.ErrorCode, result.Error)
				}
				return
			}
			if !result.Success || result.Data == nil || result.Data.Evaluation == nil || *result.Data.Evaluation != test.expected {
				t.Fatalf("actual bounded evaluation was lost: success=%t error=%s", result.Success, result.Error)
			}
		})
	}
}

func TestTaskEvaluationUsesTheActualNativeRouteAndReleasesItsPage(t *testing.T) {
	var documents atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/document" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusAccepted)
		if _, err := fmt.Fprintf(w, `<!doctype html><title>before</title><p id="out">before</p><script>window.answer='native-load-%d';</script>`, documents.Add(1)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	policy, err := bridge.NewStaticNavigationPolicy([]byte(`{"default":"deny","allow":["navigate","evaluate"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	owner, session := newTaskEvaluationAgent(t, server.URL, policy, true)
	result := owner.ExecuteTask(t.Context(), Task{ID: "actual-native-evaluation", SessionID: session.SessionID(), Timeout: 30 * time.Second,
		Action: FetchAction{URL: server.URL + "/document", Mode: "chromium_cdp", EvaluateExpression: `(document.title='native-evaluated',document.getElementById('out').textContent='native-after',window.answer)`}})
	if !result.Success || result.Data == nil || result.Data.Evaluation == nil || *result.Data.Evaluation != "native-load-1" || result.Data.StatusCode != http.StatusAccepted ||
		result.Data.Title != "native-evaluated" || result.Data.Text != "native-after" || documents.Load() != 1 || session.TabCount() != 0 {
		t.Fatalf("actual native evaluation/refreshed snapshot/resource ownership failed: success=%t code=%s documents=%d tabs=%d error=%s", result.Success, result.ErrorCode, documents.Load(), session.TabCount(), result.Error)
	}
}
