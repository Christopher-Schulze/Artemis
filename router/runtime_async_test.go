package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeExecutorAsyncFetchSettlesRealConcurrentResponses(t *testing.T) {
	for _, test := range []struct {
		name, prefix           string
		perRequest, renderless bool
	}{
		{name: "relative"}, {name: "absolute", prefix: "location.origin + "},
		{name: "relative request", perRequest: true}, {name: "absolute request", prefix: "location.origin + ", perRequest: true},
		{name: "relative renderless request", perRequest: true, renderless: true},
		{name: "absolute renderless request", prefix: "location.origin + ", perRequest: true, renderless: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			released := make(chan struct{})
			var release sync.Once
			var apiCalls, releaseCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body string
				switch r.URL.Path {
				case "/api":
					apiCalls.Add(1)
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
					body = "actual-concurrent-response"
				case "/release":
					releaseCalls.Add(1)
					release.Do(func() { close(released) })
					body = "released"
				default:
					w.Header().Set("Content-Type", "text/html")
					body = fmt.Sprintf(`<!doctype html><p id="out">pending</p><script>
fetch(%s'/api').then(r => r.text()).then(value => document.getElementById('out').textContent = value);
fetch(%s'/release');
</script>`, test.prefix, test.prefix)
				}
				if _, err := fmt.Fprint(w, body); err != nil {
					t.Errorf("actual HTTP response: %v", err)
				}
			}))
			defer server.Close()
			engine := testEngineConfig(t, 3*time.Second, server)
			defer closeRouterTestResource(t, "engine", engine.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var executor Executor = RuntimeExecutor{Runtime: engine, RunScripts: true, AsyncFetch: !test.perRequest}
			if test.renderless {
				executor = RenderlessExecutor{Engine: engine, RunScripts: true}
			}
			output, err := executor.Execute(ctx, ExecutionRequest{URL: server.URL, AsyncFetch: test.perRequest})
			if err != nil {
				t.Fatal(err)
			}
			defer closeRouterTestResource(t, "transferred page", output.Resource.Close)
			if !output.Verified || output.Page.StatusCode != 200 || !strings.Contains(output.Page.Text, "actual-concurrent-response") || strings.Contains(output.Page.Text, "pending") || apiCalls.Load() != 1 || releaseCalls.Load() != 1 {
				t.Fatalf("router returned before actual concurrent completion: status=%d text=%q api=%d release=%d", output.Page.StatusCode, output.Page.Text, apiCalls.Load(), releaseCalls.Load())
			}
		})
	}
}
