package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestConfiguredRobotsFailureNeverDispatchesActualPage(t *testing.T) {
	for _, test := range []struct {
		name              string
		status            int
		contentType, body string
	}{{"server failure", 503, "text/plain", "unreachable"}, {"protected rules", 403, "text/plain", "denied"}, {"challenge response", 200, "text/html", "<html>challenge</html>"}} {
		t.Run(test.name, func(t *testing.T) {
			var pageHits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					w.Header().Set("Content-Type", test.contentType)
					w.WriteHeader(test.status)
					writeTestBody(t, w, test.body)
					return
				}
				pageHits.Add(1)
				writeTestBody(t, w, "<html>unexpected page</html>")
			}))
			defer server.Close()
			config := testConfig(server)
			config.ObeyRobots = true
			engine, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "engine", engine.Close)
			if page, err := engine.Fetch(t.Context(), server.URL+"/page", FetchOpts{}); page != nil || !errors.Is(err, network.ErrRobotsUnavailable) {
				t.Fatalf("robots failure bypassed: page=%v error=%v", page, err)
			}
			if pageHits.Load() != 0 {
				t.Fatalf("failed rules dispatched %d pages", pageHits.Load())
			}
		})
	}
}

func TestConfiguredRobotsUsesActualCallerAgentAndEncodedQuery(t *testing.T) {
	var pageHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			writeTestBody(t, w, "User-agent: *\nDisallow: /query?token=\nDisallow: /literal%2A\nUser-agent: CallerBot\nDisallow: /caller\n")
			return
		}
		pageHits.Add(1)
		writeTestBody(t, w, "<html>allowed</html>")
	}))
	defer server.Close()
	config := testConfig(server)
	config.ObeyRobots = true
	config.UserAgent = "Artemis/1"
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	for _, test := range []struct {
		path    string
		headers http.Header
	}{{"/query?token=value", nil}, {"/literal%2A", nil}, {"/caller", http.Header{"user-agent": []string{"CallerBot/1"}}}} {
		if page, err := engine.Fetch(t.Context(), server.URL+test.path, FetchOpts{Headers: test.headers}); page != nil || !errors.Is(err, ErrRobotsDisallowed) {
			t.Fatalf("actual agent/path bypass: path=%s page=%v error=%v", test.path, page, err)
		}
	}
	page, err := engine.Fetch(t.Context(), server.URL+"/query?name=value", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, "allowed page", page.Close)
	if pageHits.Load() != 1 {
		t.Fatalf("robots outcome count=%d", pageHits.Load())
	}
}

func TestConfiguredRobotsDelayAndSessionCancellationOwnRealFetches(t *testing.T) {
	var pageHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			writeTestBody(t, w, "User-agent: *\nCrawl-delay: 0.2\nDisallow: /denied\n")
			return
		}
		pageHits.Add(1)
		writeTestBody(t, w, "<html>paced page</html>")
	}))
	defer server.Close()
	config := testConfig(server)
	config.ObeyRobots = true
	config.SessionBudget.MaxConcurrency = 1
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	page, err := engine.Fetch(t.Context(), server.URL+"/first", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, "first", page.Close)
	started := time.Now()
	page, err = engine.Fetch(t.Context(), server.URL+"/second", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, "second", page.Close)
	if elapsed := time.Since(started); elapsed < 175*time.Millisecond {
		t.Fatalf("crawl admission ignored delay: %s", elapsed)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if page, err := engine.Fetch(ctx, server.URL+"/cancelled", FetchOpts{}); page != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delay cancellation lost: %v %v", page, err)
	}
	if pageHits.Load() != 2 || engine.SessionUsage().Concurrent != 0 {
		t.Fatalf("cancelled delay emitted/leaked lease: hits=%d usage=%+v", pageHits.Load(), engine.SessionUsage())
	}
}

func TestConfiguredRobotsRetrievalAndCacheUseActualWireAgent(t *testing.T) {
	var robotsHits, pageHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHits.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			body := "User-agent: *\nAllow: /\n"
			if r.UserAgent() == "CallerBot/1" {
				body = "User-agent: CallerBot\nDisallow: /private\n"
			}
			writeTestBody(t, w, body)
			return
		}
		pageHits.Add(1)
		writeTestBody(t, w, "<html>actual page</html>")
	}))
	defer server.Close()
	config := testConfig(server)
	config.ObeyRobots = true
	config.UserAgent = "Artemis/1"
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	page, err := engine.Fetch(t.Context(), server.URL+"/private", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, "allowed page", page.Close)
	for range 2 {
		headers := http.Header{"user-agent": []string{"CallerBot/1", "IgnoredOnWire"}}
		if page, err := engine.Fetch(t.Context(), server.URL+"/private", FetchOpts{Headers: headers}); page != nil || !errors.Is(err, ErrRobotsDisallowed) {
			t.Fatalf("wire agent bypassed robots: page=%v error=%v", page, err)
		}
	}
	if robotsHits.Load() != 2 || pageHits.Load() != 1 {
		t.Fatalf("agent cache/dispatch mismatch: robots=%d pages=%d", robotsHits.Load(), pageHits.Load())
	}
}
