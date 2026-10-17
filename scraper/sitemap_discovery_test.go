package scraper

import (
	"bytes"
	"compress/gzip"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func sitemapTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, contentType, body string) {
		w.Header().Set("Content-Type", contentType)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write: %v", err)
		}
	}
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		write(w, "text/plain", "User-agent: *\nDisallow: /private\nSitemap: "+server.URL+"/maps/index.xml\n")
	})
	mux.HandleFunc("/maps/index.xml", func(w http.ResponseWriter, _ *http.Request) {
		write(w, "application/xml", `<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
			<sitemap><loc>/maps/pages.xml</loc></sitemap>
			<sitemap><loc>`+server.URL+`/maps/more.xml.gz</loc></sitemap>
			<sitemap><loc>https://foreign.example/sitemap.xml</loc></sitemap>
			<sitemap><loc>/maps/index.xml</loc></sitemap>
			<sitemap><loc>/maps/broken.xml</loc></sitemap>
		</sitemapindex>`)
	})
	mux.HandleFunc("/maps/pages.xml", func(w http.ResponseWriter, _ *http.Request) {
		write(w, "application/xml", `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
			<url><loc>`+server.URL+`/a</loc><lastmod>2026-10-01</lastmod><changefreq>daily</changefreq><priority>0.8</priority></url>
			<url><loc>/b</loc></url>
			<url><loc>https://foreign.example/c</loc></url>
		</urlset>`)
	})
	mux.HandleFunc("/maps/more.xml.gz", func(w http.ResponseWriter, _ *http.Request) {
		var compressed bytes.Buffer
		zipper := gzip.NewWriter(&compressed)
		if _, err := zipper.Write([]byte(`<urlset><url><loc>/b</loc></url><url><loc>/d</loc></url></urlset>`)); err != nil {
			t.Fatal(err)
		}
		if err := zipper.Close(); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/gzip")
		if _, err := w.Write(compressed.Bytes()); err != nil {
			t.Errorf("write gzip: %v", err)
		}
	})
	mux.HandleFunc("/maps/broken.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func sitemapTestClient(t *testing.T, server *httptest.Server) *network.HTTPClient {
	t.Helper()
	policy, err := network.NewPolicy(network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{server.Listener.Addr().(*net.TCPAddr).Port}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := network.NewHTTPClient(network.HTTPClientConfig{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func TestDiscoverSitemapURLsFollowsIndexesWithinTheOriginHost(t *testing.T) {
	server := sitemapTestServer(t)
	discovery, err := DiscoverSitemapURLs(t.Context(), sitemapTestClient(t, server), server.URL+"/any/page", SitemapLimits{})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(discovery.URLs))
	for _, entry := range discovery.URLs {
		got = append(got, strings.TrimPrefix(entry.Loc, server.URL))
	}
	if strings.Join(got, ",") != "/a,/b,/d" {
		t.Fatalf("discovered URL set = %v", got)
	}
	if first := discovery.URLs[0]; first.LastMod != "2026-10-01" || first.ChangeFreq != "daily" || first.Priority != "0.8" {
		t.Fatalf("sitemap metadata lost: %+v", first)
	}
	if len(discovery.Sitemaps) != 3 || len(discovery.Failures) != 1 || !strings.HasSuffix(discovery.Failures[0].URL, "/maps/broken.xml") || discovery.Truncated {
		t.Fatalf("sitemap traversal/failures wrong: sitemaps=%v failures=%+v truncated=%t", discovery.Sitemaps, discovery.Failures, discovery.Truncated)
	}
}

func TestDiscoverSitemapURLsHonoursLimits(t *testing.T) {
	server := sitemapTestServer(t)
	discovery, err := DiscoverSitemapURLs(t.Context(), sitemapTestClient(t, server), server.URL, SitemapLimits{MaxURLs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.URLs) != 2 || !discovery.Truncated {
		t.Fatalf("URL limit not enforced: %d urls truncated=%t", len(discovery.URLs), discovery.Truncated)
	}
	shallow, err := DiscoverSitemapURLs(t.Context(), sitemapTestClient(t, server), server.URL, SitemapLimits{MaxDepth: 1, MaxSitemaps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(shallow.Sitemaps) != 1 || len(shallow.URLs) != 0 || !shallow.Truncated {
		t.Fatalf("sitemap count limit not enforced: %+v", shallow)
	}
}
