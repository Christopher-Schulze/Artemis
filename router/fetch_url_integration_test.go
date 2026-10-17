package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeFetchURLUsesActualRedirectAndDocumentBase(t *testing.T) {
	for _, test := range []struct{ name, head, reference, want string }{
		{"redirected document", "", "api", "/dir/api"},
		{"parent", "", "../api?q=1", "/api?q=1"},
		{"first base", `<base href="/assets/"><base href="/ignored/">`, "api", "/assets/api"},
		{"invalid first base", `<base href="http://["><base href="/ignored/">`, "api", "/dir/api"},
		{"empty", "", "", "/dir/page"},
		{"query", "", "?next=2", "/dir/page?next=2"},
	} {
		for _, async := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/async=%t", test.name, async), func(t *testing.T) {
				var requests atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/redirect" {
						http.Redirect(w, r, "/dir/page", http.StatusFound)
						return
					}
					var body string
					if r.Header.Get("X-Fixture-Fetch") == "yes" {
						requests.Add(1)
						body = "actual-response:" + r.URL.RequestURI()
					} else {
						w.Header().Set("Content-Type", "text/html")
						body = fmt.Sprintf(`<!doctype html><html><head>%s</head><body><p id="out">pending</p><script>
fetch(%q, {headers:{'X-Fixture-Fetch':'yes'}}).then(r=>r.text()).then(value=>{document.getElementById('out').textContent=value});
</script></body></html>`, test.head, test.reference)
					}
					if _, err := fmt.Fprint(w, body); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				engine := testEngineConfig(t, 3*time.Second, server)
				defer closeRouterTestResource(t, "engine", engine.Close)
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				output, err := (RuntimeExecutor{Runtime: engine, RunScripts: true, AsyncFetch: async}).Execute(ctx, ExecutionRequest{URL: server.URL + "/redirect"})
				if err != nil {
					t.Fatal(err)
				}
				defer closeRouterTestResource(t, "transferred page", output.Resource.Close)
				if requests.Load() != 1 || output.Page.URL != server.URL+"/dir/page" || output.Page.Text != "actual-response:"+test.want {
					t.Fatalf("actual redirected fetch: url=%q text=%q requests=%d", output.Page.URL, output.Page.Text, requests.Load())
				}
			})
		}
	}
}

func TestRuntimeFetchURLCannotBypassActualSubresourcePolicy(t *testing.T) {
	var foreignRequests atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprintf(w, `<!doctype html><base href="%s/assets/"><p id="out">pending</p><script>
fetch('api').then(()=>{document.getElementById('out').textContent='escaped'}).catch(()=>{document.getElementById('out').textContent='denied'});
</script>`, foreign.URL); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	engine := testEngineConfig(t, 3*time.Second, server)
	defer closeRouterTestResource(t, "engine", engine.Close)
	for _, async := range []bool{false, true} {
		output, err := (RuntimeExecutor{Runtime: engine, RunScripts: true, AsyncFetch: async}).Execute(t.Context(), ExecutionRequest{URL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		if err := output.Resource.Close(); err != nil {
			t.Fatal(err)
		}
		if output.Page.Text != "denied" || foreignRequests.Load() != 0 {
			t.Fatalf("resolved base bypassed policy: text=%q foreign=%d", output.Page.Text, foreignRequests.Load())
		}
	}
}

func TestRuntimeFetchURLCancellationRetiresActualHTTP(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%t", async), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			retired := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/api") {
					cancel()
					<-r.Context().Done()
					close(retired)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				if _, err := fmt.Fprint(w, `<!doctype html><script>fetch('api');</script>`); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			engine := testEngineConfig(t, 3*time.Second, server)
			defer closeRouterTestResource(t, "engine", engine.Close)
			output, err := (RuntimeExecutor{Runtime: engine, RunScripts: true, AsyncFetch: async}).Execute(ctx, ExecutionRequest{URL: server.URL + "/dir/page"})
			if output.Resource != nil {
				if closeErr := output.Resource.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("actual fetch cancellation lost: %v", err)
			}
			select {
			case <-retired:
			case <-time.After(time.Second):
				t.Fatal("cancelled actual HTTP request remained active")
			}
		})
	}
}
