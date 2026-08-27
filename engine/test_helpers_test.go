package engine

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func closeTestResource(t testing.TB, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("%s: %v", label, err)
	}
}

func writeTestBody(t testing.TB, w io.Writer, body string) {
	t.Helper()
	if _, err := fmt.Fprint(w, body); err != nil {
		t.Errorf("write test body: %v", err)
	}
}

// testConfig returns an engine.Config that allows the provided httptest
// servers in addition to the default public ports 80/443. It is used by
// tests that need to fetch from loopback fixtures; it explicitly opts in
// to private networks and the fixture ports.
func testConfig(srvs ...*httptest.Server) Config {
	cfg := Config{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true}}
	ports := []int{80, 443}
	for _, srv := range srvs {
		if srv == nil {
			continue
		}
		u, err := url.Parse(srv.URL)
		if err != nil {
			continue
		}
		p := u.Port()
		if p == "" {
			continue
		}
		port, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		ports = append(ports, port)
	}
	sort.Ints(ports)
	cfg.PolicyConfig.AllowedPorts = ports
	return cfg
}

// mustNewTest builds an engine with testConfig(srvs...) and fails the test
// on error.
func mustNewTest(t testing.TB, srvs ...*httptest.Server) *Engine {
	t.Helper()
	eng, err := New(testConfig(srvs...))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return eng
}

// mustFetch fetches rawURL with opts and fails the test on error.
func mustFetch(t testing.TB, eng *Engine, rawURL string, opts FetchOpts) *Page {
	t.Helper()
	page, err := eng.Fetch(context.Background(), rawURL, opts)
	if err != nil {
		t.Fatalf("Fetch %s: %v", rawURL, err)
	}
	return page
}
