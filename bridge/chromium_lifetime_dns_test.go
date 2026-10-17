package bridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

type cancelledNativeLookup struct {
	entered, cancelled chan struct{}
	start, finish      sync.Once
	calls              atomic.Int64
}

func (r *cancelledNativeLookup) LookupNetIP(ctx context.Context, family, host string) ([]netip.Addr, error) {
	if family != "ip" || host != "cancelled-owner.invalid" {
		return nil, errors.New("unexpected native policy DNS target")
	}
	r.calls.Add(1)
	r.start.Do(func() { close(r.entered) })
	<-ctx.Done()
	r.finish.Do(func() { close(r.cancelled) })
	return nil, ctx.Err()
}

func TestChromiumPageCloseCancelsActualPolicyDNSBeforeReturning(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	resolver := &cancelledNativeLookup{entered: make(chan struct{}), cancelled: make(chan struct{})}
	port := testURLPort(t, server.URL)
	policy, err := network.NewPolicy(network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}, DialTimeout: 10 * time.Second}, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newChromiumPolicyProxy(t.Context(), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "policy proxy", proxy.Close)
	binary := requireChromium(t)
	process, err := browserprocess.Launch(t.Context(), browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second, PolicyProxyURL: proxy.URL()})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "owned process", process.Close)
	browser, err := connectChromium(t.Context(), process.Endpoint(), process, policy, proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "browser", browser.Close)
	owner, err := browser.NewContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://cancelled-owner.invalid:" + strconv.Itoa(port) + "/blocked"
	if err := page.Call(t.Context(), "Runtime.evaluate", map[string]any{"expression": "fetch('" + endpoint + "').catch(()=>{});"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resolver.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual native request never reached its policy resolver")
	}
	finished := make(chan error, 1)
	go func() { finished <- page.Close() }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("page close did not cancel/join native policy DNS")
	}
	select {
	case <-resolver.cancelled:
	default:
		t.Fatal("close returned before the actual resolver completed")
	}
	if resolver.calls.Load() != 1 || hits.Load() != 0 || !browser.Healthy() {
		t.Fatalf("wrong DNS/dispatch outcome: lookups=%d hits=%d healthy=%t", resolver.calls.Load(), hits.Load(), browser.Healthy())
	}
}
