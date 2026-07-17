package js

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Christopher-Schulze/Artemis/parser"
)

// TestContextCloseShutsDownWSGoroutines verifies that opening a
// WebSocket inside a Context and then closing the Context (without
// the JS-side calling ws.close()) tears down the per-conn read
// goroutine. Without this, every page that touched a WS would leak
// a goroutine for the lifetime of the process.
func TestContextCloseShutsDownWSGoroutines(t *testing.T) {
	srv := newWSEchoServer(t)
	defer srv.Close()

	baseline := runtime.NumGoroutine()

	rt := NewRuntime()
	defer rt.Close()
	doc, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://e.test/")
	targetURL := wsURL(srv)
	policy := localWebSocketPolicy(t, targetURL, nil)

	for i := 0; i < 10; i++ {
		c, err := rt.NewContext(doc, ContextOpts{Policy: policy})
		if err != nil {
			t.Fatalf("iter %d NewContext: %v", i, err)
		}
		// Open a WebSocket, do not close it explicitly.
		if _, err := c.Eval(context.Background(), `globalThis.lifecycleWS = new WebSocket("`+targetURL+`")`); err != nil {
			t.Fatalf("iter %d open: %v", i, err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			state, err := c.Eval(context.Background(), `globalThis.lifecycleWS.readyState`)
			if err != nil {
				t.Fatalf("iter %d state: %v", i, err)
			}
			if state.Int64() == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("iter %d WebSocket did not reach OPEN; state=%d", i, state.Int64())
			}
			time.Sleep(10 * time.Millisecond)
		}
		c.Close()
	}

	// Allow scheduler to run any pending close logic.
	for tries := 0; tries < 50; tries++ {
		if runtime.NumGoroutine() <= baseline+5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines did not return to baseline: started=%d ended=%d", baseline, runtime.NumGoroutine())
}

// TestContextCloseCancelsAsyncFetch verifies that an in-flight async
// fetch goroutine respects the Context.Close cancellation.
func TestContextCloseCancelsAsyncFetch(t *testing.T) {
	// Slow server that holds the request open until the test cancels.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
			w.Write([]byte("ok"))
		case <-r.Context().Done():
			return
		}
	}))
	defer srv.Close()

	rt := NewRuntime()
	defer rt.Close()
	doc, _ := parser.ParseHTML(strings.NewReader("<html></html>"), "https://e.test/")

	baseline := runtime.NumGoroutine()
	c, err := rt.NewContext(doc, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			httpReq, _ := http.NewRequestWithContext(ctx, req.Method, req.URL, nil)
			resp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			return &FetchResponse{Status: resp.StatusCode, URL: req.URL}, nil
		},
		AsyncFetch: true,
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}

	// Kick off async fetch, don't wait.
	if _, err := c.Eval(context.Background(), `fetch("`+srv.URL+`/slow")`); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// Close before the fetch completes; goroutine must exit.
	c.Close()

	// Wait for the fetch goroutine to notice the cancel and exit.
	for tries := 0; tries < 200; tries++ {
		if runtime.NumGoroutine() <= baseline+5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("async fetch goroutine did not exit: baseline=%d, current=%d", baseline, runtime.NumGoroutine())
}

func newWSEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, _, _ = conn.Read(r.Context())
	}))
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func timeoutCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
