package router

import (
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

// testEngineConfig returns an engine.Config with timeout and a policy that
// allows the provided httptest fixture servers. It is used by router tests
// that exercise the real renderless engine against loopback fixtures.
func testEngineConfig(t *testing.T, timeout time.Duration, srvs ...*httptest.Server) *engine.Engine {
	t.Helper()
	cfg := engine.Config{
		Timeout: timeout,
		PolicyConfig: network.PolicyConfig{
			AllowPrivateNetworks: true,
		},
	}
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
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	return eng
}
