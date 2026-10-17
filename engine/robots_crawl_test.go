package engine

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func crawlRobotsServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var privateHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/robots-live.txt", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/robots-live.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		writeTestBody(t, w, "User-agent: *\nDisallow: /private\n")
	})
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/private/page", http.StatusFound)
	})
	mux.HandleFunc("/public", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		writeTestBody(t, w, "<html><title>public</title></html>")
	})
	mux.HandleFunc("/private/", func(w http.ResponseWriter, _ *http.Request) {
		privateHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		writeTestBody(t, w, "<html><title>private</title></html>")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &privateHits
}

func TestCrawlFetchesRespectRobotsByDefaultIncludingRedirectHops(t *testing.T) {
	server, privateHits := crawlRobotsServer(t)
	eng, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	if _, err := eng.Fetch(t.Context(), server.URL+"/private/page", FetchOpts{Crawl: true}); !errors.Is(err, network.ErrRobotsDisallowed) {
		t.Fatalf("crawl fetch ignored robots.txt: %v", err)
	}
	if _, err := eng.Fetch(t.Context(), server.URL+"/start", FetchOpts{Crawl: true}); !errors.Is(err, network.ErrRobotsDisallowed) {
		t.Fatalf("crawl redirect into a disallowed path was admitted: %v", err)
	}
	if privateHits.Load() != 0 {
		t.Fatalf("disallowed pages were fetched during crawls: %d", privateHits.Load())
	}
	page, err := eng.Fetch(t.Context(), server.URL+"/public", FetchOpts{Crawl: true})
	if err != nil {
		t.Fatalf("allowed crawl fetch failed: %v", err)
	}
	closeTestResource(t, "public page close", page.Close)
	agentPage, err := eng.Fetch(t.Context(), server.URL+"/private/page", FetchOpts{})
	if err != nil {
		t.Fatalf("ordinary agent fetch must behave like a user browser: %v", err)
	}
	closeTestResource(t, "agent page close", agentPage.Close)
}

func TestOperatorCanOptCrawlsOutOfRobots(t *testing.T) {
	server, _ := crawlRobotsServer(t)
	config := testConfig(server)
	config.IgnoreRobotsForCrawls = true
	eng, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(t.Context(), server.URL+"/start", FetchOpts{Crawl: true})
	if err != nil {
		t.Fatalf("operator opt-out was ignored: %v", err)
	}
	closeTestResource(t, "page close", page.Close)
}
