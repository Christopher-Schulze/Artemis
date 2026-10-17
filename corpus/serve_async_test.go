package corpus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func TestServeAsyncWireAndActualConcurrentHTTP(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			released, entered, retired := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var release sync.Once
			var apiCalls, releaseCalls atomic.Int64
			server := fixture.NewServer()
			defer server.Close()
			if err := server.RegisterHandler("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiCalls.Add(1)
				close(entered)
				defer close(retired)
				if cancelled {
					<-r.Context().Done()
					return
				}
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
				if _, err := fmt.Fprint(w, "actual-Serve-concurrent-response"); err != nil {
					t.Error(err)
				}
			})); err != nil {
				t.Fatal(err)
			}
			if err := server.RegisterHandler("/release", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				releaseCalls.Add(1)
				release.Do(func() { close(released) })
				w.WriteHeader(http.StatusNoContent)
			})); err != nil {
				t.Fatal(err)
			}
			scenario := fixture.Scenario{ID: "concurrent", Path: "/concurrent", Kind: fixture.KindSPA,
				Status: 200, RunScripts: true, AsyncFetch: true, WaitForIdle: true,
				HTML: `<!doctype html><p id="out">pending</p><script>
fetch('/api').then(r=>r.text()).then(v=>{document.getElementById('out').textContent=v});
fetch('/release');</script>`,
				Expect: fixture.Expect{Status: 200, Contains: []string{"actual-Serve-concurrent-response"}, NotContains: []string{"pending"}}}
			if err := server.Register(scenario); err != nil {
				t.Fatal(err)
			}
			owner := newServeAsyncTestOwner(t, server)
			var cancelledOwner chan struct{}
			if cancelled {
				cancelledOwner = make(chan struct{})
				go func() {
					defer close(cancelledOwner)
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			result, err := runControlServe(ctx, owner, fixtureasset.Manifest{}, scenario, server)
			if cancelled {
				<-cancelledOwner
				if !errors.Is(err, context.Canceled) || apiCalls.Load() != 1 {
					t.Fatalf("actual cancelled Serve outcome: api=%d err=%v", apiCalls.Load(), err)
				}
			} else if err != nil || result.Text != "actual-Serve-concurrent-response" || apiCalls.Load() != 1 || releaseCalls.Load() != 1 {
				t.Fatalf("Serve did not settle actual concurrent HTTP: text=%q api=%d release=%d err=%v", result.Text, apiCalls.Load(), releaseCalls.Load(), err)
			}
			select {
			case <-retired:
			case <-time.After(time.Second):
				t.Fatal("actual pending HTTP survived owned Serve shutdown")
			}
			if len(owner.ListSessions()) != 0 {
				t.Fatal("Serve retained owned sessions after shutdown")
			}
		})
	}
}

func newServeAsyncTestOwner(t *testing.T, server *fixture.Server) *artemis.Agent {
	t.Helper()
	policy, err := corpusActionPolicy(server.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(policy.Stop)
	owner, err := artemis.NewAgent(artemis.AgentConfig{MaxSessions: 1, PolicyConfig: server.PolicyConfig(), PolicyEngine: policy})
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
	return owner
}

func TestServeAsyncDefaultAndExplicitWire(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		data, err := json.Marshal(serve.PageOpenParams{SessionID: "owner", URL: "http://127.0.0.1/", RunScripts: true, AsyncFetch: enabled})
		if err != nil || strings.Contains(string(data), "asyncFetch") != enabled {
			t.Fatalf("optional Serve async wire: %s %v", data, err)
		}
		var decoded serve.PageOpenParams
		if err := json.Unmarshal(data, &decoded); err != nil || decoded.AsyncFetch != enabled {
			t.Fatalf("Serve async wire lost option: %+v %v", decoded, err)
		}
	}
}

func TestServePageOptionsRejectAmbiguityBeforeHTTPAndPreserveLegacyCall(t *testing.T) {
	var calls atomic.Int64
	server := fixture.NewServer()
	defer server.Close()
	if err := server.RegisterHandler("/legacy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if _, err := fmt.Fprint(w, "legacy-response"); err != nil {
			t.Error(err)
		}
	})); err != nil {
		t.Fatal(err)
	}
	owner := newServeAsyncTestOwner(t, server)
	session, err := owner.CreateSession("actual-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.OpenPage(t.Context(), server.URL("/legacy"), false, artemis.PageOpenOptions{}, artemis.PageOpenOptions{}); err == nil || err.Code != artemis.TaskErrorInvalidInput || calls.Load() != 0 {
		t.Fatalf("ambiguous options reached HTTP: calls=%d err=%v", calls.Load(), err)
	}
	if _, page, err := session.OpenPage(t.Context(), server.URL("/legacy"), false); err != nil || page == nil || calls.Load() != 1 {
		t.Fatalf("legacy call changed: calls=%d err=%v", calls.Load(), err)
	}
	if err := session.Close(); err != nil || len(owner.ListSessions()) != 0 {
		t.Fatalf("page options retained session ownership: %v", err)
	}
}

func TestServeAsyncOriginalCanonicalScenario(t *testing.T) {
	manifests, data, err := DefaultAssets("serve")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if !strings.HasSuffix(manifest.ID, ":spa-001") {
			continue
		}
		scenario, err := fixture.CorpusScenario(manifest, data)
		if err != nil {
			t.Fatal(err)
		}
		result, err := RunServe(t.Context(), manifest, data)
		if err != nil || !result.Passed || result.StatusCode != scenario.Expect.Status || !strings.Contains(result.Text, "Hello SPA") {
			t.Fatalf("original admitted Serve SPA: status=%d text=%q err=%v", result.StatusCode, result.Text, err)
		}
		return
	}
	t.Fatal("original SPA source missing")
}
