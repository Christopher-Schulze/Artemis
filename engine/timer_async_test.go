package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWaitIdlePumpsActualDelayedTimerFetchAndPropagatesFailure(t *testing.T) {
	for _, mode := range []string{"settled", "callback error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			released, entered, retired := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var release sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/held":
					close(entered)
					defer close(retired)
					select {
					case <-released:
						writeTestBody(t, w, "actual-timer-released-response")
					case <-r.Context().Done():
					}
				case "/release":
					release.Do(func() { close(released) })
					w.WriteHeader(http.StatusNoContent)
				default:
					callback := "fetch('/release')"
					if mode == "callback error" {
						callback = "throw new Error('actual-timer-failure')"
					}
					writeTestBody(t, w, "<!doctype html><p id='out'>pending</p><script>fetch('/held').then(r=>r.text()).then(v=>document.getElementById('out').textContent=v);setTimeout(()=>{"+callback+"},80);</script>")
				}
			}))
			defer server.Close()
			owner, err := New(testConfig(server))
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "engine", owner.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			started := time.Now()
			page, err := owner.Fetch(ctx, server.URL, FetchOpts{RunScripts: true, AsyncFetch: true})
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "page", page.Close)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("real pending HTTP request never entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			err = page.WaitIdle(ctx)
			switch mode {
			case "settled":
				if err != nil || page.Text() != "actual-timer-released-response" || time.Since(started) < 80*time.Millisecond {
					t.Fatalf("timer did not release and settle actual HTTP: text=%q err=%v", page.Text(), err)
				}
			case "callback error":
				if err == nil || !strings.Contains(err.Error(), "actual-timer-failure") {
					t.Fatalf("WaitIdle discarded the actual timer failure: %v", err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("WaitIdle discarded owner cancellation: %v", err)
				}
			}
			if err := page.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-retired:
			case <-time.After(time.Second):
				t.Fatal("owned page close did not retire actual pending HTTP")
			}
		})
	}
}
