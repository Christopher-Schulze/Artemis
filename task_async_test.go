package artemis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestAgentAsyncTaskWireWaitsForActualConcurrentHTTP(t *testing.T) {
	for _, pointer := range []bool{false, true} {
		t.Run(fmt.Sprintf("pointer=%t", pointer), func(t *testing.T) {
			released := make(chan struct{})
			var release sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := "released"
				switch r.URL.Path {
				case "/api":
					select {
					case <-released:
					case <-r.Context().Done():
						return
					}
					select {
					case <-time.After(150 * time.Millisecond):
					case <-r.Context().Done():
						return
					}
					body = "actual-Agent-concurrent-result"
				case "/release":
					release.Do(func() { close(released) })
				default:
					w.Header().Set("Content-Type", "text/html")
					body = `<!doctype html><p id="out">pending</p><script>
fetch('/api').then(r=>r.text()).then(value=>{document.getElementById('out').textContent=value});
fetch('/release');</script>`
				}
				if _, err := fmt.Fprint(w, body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			origin, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(origin.Port())
			if err != nil {
				t.Fatal(err)
			}
			owner, err := NewAgent(AgentConfig{PolicyConfig: network.PolicyConfig{AllowedDomains: []string{origin.Hostname()}, AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := owner.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := owner.Stop(); err != nil {
					t.Error(err)
				}
			}()
			session, err := owner.CreateSession("actual-owner")
			if err != nil {
				t.Fatal(err)
			}
			fetch := FetchAction{URL: server.URL, RunScripts: true, AsyncFetch: true}
			var action Action = fetch
			if pointer {
				action = &fetch
			}
			encoded, err := json.Marshal(Task{ID: "actual-async", SessionID: session.SessionID(), Action: action})
			if err != nil {
				t.Fatal(err)
			}
			var task Task
			if err := json.Unmarshal(encoded, &task); err != nil {
				t.Fatal(err)
			}
			decoded, ok := task.Action.(FetchAction)
			if !ok || !decoded.AsyncFetch {
				t.Fatal("wire lost explicit async setting")
			}
			result := owner.ExecuteTask(ctx, task)
			if !result.Success || result.Data == nil || result.Data.StatusCode != 200 || result.Data.Text != "actual-Agent-concurrent-result" {
				t.Fatalf("actual Agent task returned before concurrent completion: success=%t code=%s err=%s", result.Success, result.ErrorCode, result.Error)
			}
			if err := session.Close(); err != nil || len(owner.ListSessions()) != 0 {
				t.Fatalf("actual task session ownership retained: %v", err)
			}
		})
	}
}

func TestAgentAsyncTaskDefaultWireRetainsSynchronousContract(t *testing.T) {
	encoded, err := json.Marshal(Task{ID: "default", SessionID: "session", Action: FetchAction{URL: "https://example.test/"}})
	if err != nil || strings.Contains(string(encoded), "asyncFetch") {
		t.Fatalf("default task changed its wire contract: %s %v", encoded, err)
	}
	var task Task
	if err := json.Unmarshal(encoded, &task); err != nil {
		t.Fatal(err)
	}
	action, ok := task.Action.(FetchAction)
	if !ok || action.AsyncFetch {
		t.Fatal("absent async setting enabled asynchronous fetch")
	}
}

func TestAgentAsyncTaskStopRetiresActualPendingHTTP(t *testing.T) {
	entered, retired := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			close(entered)
			<-r.Context().Done()
			close(retired)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, `<!doctype html><script>fetch('/api');</script>`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(origin.Port())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewAgent(AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := owner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Stop(); err != nil {
			t.Error(err)
		}
	}()
	session, err := owner.CreateSession("actual-owner")
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan TaskResult, 1)
	go func() {
		completed <- owner.ExecuteTask(ctx, Task{ID: "actual-stop", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, RunScripts: true, AsyncFetch: true}})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual asynchronous request did not start")
	}
	if err := owner.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-completed:
		if result.Success || result.ErrorCode != TaskErrorCancelled {
			t.Fatalf("stopped async task lost cancellation: success=%t code=%s", result.Success, result.ErrorCode)
		}
	case <-ctx.Done():
		t.Fatal("Agent stop did not retire its actual task")
	}
	select {
	case <-retired:
	case <-ctx.Done():
		t.Fatal("Agent stop left actual HTTP active")
	}
	if owner.State() != AgentStateStopped || len(owner.ListSessions()) != 0 {
		t.Fatal("Agent stop retained session/runtime ownership")
	}
}
