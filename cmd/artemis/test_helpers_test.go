package main

import (
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

func closeTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("%s: %v", label, err)
	}
}

func writeTestBody(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := fmt.Fprint(w, body); err != nil {
		t.Errorf("write test response: %v", err)
	}
}

// testConfig returns an engine.Config that allows the provided httptest
// fixture servers in addition to the default public ports 80/443. It is
// used by cmd/artemis tests that exercise the real engine against loopback.
func testConfig(srvs ...*httptest.Server) engine.Config {
	cfg := engine.Config{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true}}
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

// srvPort returns the numeric port of an httptest server as a string.
func srvPort(srv *httptest.Server) string {
	u, err := url.Parse(srv.URL)
	if err != nil {
		return ""
	}
	return u.Port()
}
