package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFetchCrawlFlagRespectsRobotsAndDefaultFetchDoesNot(t *testing.T) {
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", writeCLIActionPolicy(t, `{"default":"allow"}`))
	var pageHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			writeTestBody(t, w, "User-agent: *\nDisallow: /private\n")
			return
		}
		pageHits.Add(1)
		writeTestBody(t, w, `<!doctype html><title>Private</title>`)
	}))
	defer server.Close()
	base := []string{"--allow-private-networks", "--allow-port", portOf(server.URL), "--dump", "title"}
	code, _ := captureCLIOutput(t, func() int {
		return cmdFetch(append(append([]string{}, base...), "--crawl", server.URL+"/private/page"))
	})
	if code != 1 || pageHits.Load() != 0 {
		t.Fatalf("--crawl fetched a robots-disallowed page: code=%d hits=%d", code, pageHits.Load())
	}
	code, raw := captureCLIOutput(t, func() int { return cmdFetch(append(append([]string{}, base...), server.URL+"/private/page")) })
	if code != 0 || pageHits.Load() != 1 || string(raw) != "Private\n" {
		t.Fatalf("ordinary fetch must not apply robots.txt: code=%d hits=%d out=%q", code, pageHits.Load(), raw)
	}
}
