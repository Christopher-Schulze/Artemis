package main

import (
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

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
