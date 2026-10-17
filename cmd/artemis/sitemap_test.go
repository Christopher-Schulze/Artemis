package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSitemapCommandEmitsDiscoveredURLs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			writeSitemapFixture(t, w, "User-agent: *\nSitemap: "+server.URL+"/index.xml\n")
		case "/index.xml":
			w.Header().Set("Content-Type", "application/xml")
			writeSitemapFixture(t, w, `<sitemapindex><sitemap><loc>/pages.xml</loc></sitemap></sitemapindex>`)
		case "/pages.xml":
			w.Header().Set("Content-Type", "application/xml")
			writeSitemapFixture(t, w, `<urlset><url><loc>/one</loc><priority>0.5</priority></url></urlset>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "sitemap output reader close", r.Close)
	oldStdout := os.Stdout
	os.Stdout = w
	code := cmdSitemap([]string{"--allow-private-networks", "--allow-port", portOf(server.URL), server.URL})
	if err := w.Close(); err != nil {
		t.Errorf("sitemap output writer close: %v", err)
	}
	os.Stdout = oldStdout
	if code != 0 {
		t.Fatalf("sitemap exit code = %d, want 0", code)
	}
	var result struct {
		URLs []struct {
			Loc      string `json:"loc"`
			Priority string `json:"priority"`
		} `json:"urls"`
		Sitemaps []string `json:"sitemaps"`
	}
	if err := json.NewDecoder(r).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.URLs) != 1 || !strings.HasSuffix(result.URLs[0].Loc, "/one") || result.URLs[0].Priority != "0.5" || len(result.Sitemaps) != 2 {
		t.Fatalf("sitemap command output = %+v", result)
	}
}

func writeSitemapFixture(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write sitemap fixture: %v", err)
	}
}
