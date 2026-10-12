package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type securityGateResolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f securityGateResolverFunc) LookupNetIP(ctx context.Context, networkName, host string) ([]netip.Addr, error) {
	return f(ctx, networkName, host)
}

type securityGateRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f securityGateRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type securityGateUnknownLengthReader struct {
	io.Reader
}

type securityGateCancelledLifecycle struct{}

func (securityGateCancelledLifecycle) BeginRequest(parent context.Context) (context.Context, func(int64) error, error) {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx, func(int64) error { return nil }, nil
}

func TestSecurityGateSSRFCanonicalizationMatrix(t *testing.T) {
	privateResolver := securityGateResolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "private.example" {
			return []netip.Addr{netip.MustParseAddr("10.23.45.67")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	policy := mustPolicy(t, PolicyConfig{}, privateResolver, nil)
	payloads := []string{
		"http://127.1/",
		"http://0177.0.0.1/",
		"http://0x7f000001/",
		"http://2130706433/",
		"http://[::1]/",
		"http://[::ffff:127.0.0.1]/",
		"http://[fe80::1%25lo0]/",
		"http://127。0。0。1/",
		"http://localhost./",
		"http://metadata.google.internal./latest/meta-data/",
		"http://metadata.goog./computeMetadata/v1/",
		"http://service.internal/",
		"http://private.example/",
		"http://user:secret@public.example/",
	}
	for _, payload := range payloads {
		t.Run(payload, func(t *testing.T) {
			if _, err := policy.ResolveURL(context.Background(), payload, TargetNavigation, "security-gate"); !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("ResolveURL(%q) error=%v, want policy denial", payload, err)
			}
		})
	}
}

func TestSecurityGateRenderlessRedirectChainRevalidatesEveryHop(t *testing.T) {
	publicResolver := securityGateResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	var decisions []Decision
	policy := mustPolicy(t, PolicyConfig{}, publicResolver, func(decision Decision) error {
		decisions = append(decisions, decision)
		return nil
	})
	client, err := NewHTTPClient(HTTPClientConfig{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	var roundTrips atomic.Int64
	client.client.Transport = securityGateRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		roundTrips.Add(1)
		var location string
		switch request.URL.Hostname() {
		case "public.example":
			location = "http://next.example/hop"
		case "next.example":
			location = "http://169.254.169.254/latest/meta-data/"
		default:
			return nil, fmt.Errorf("blocked target reached transport: %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{location}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})
	_, err = client.Do(context.Background(), Request{URL: "http://public.example/start"})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("redirect chain error=%v, want policy denial", err)
	}
	if got := roundTrips.Load(); got != 2 {
		t.Fatalf("transport calls=%d, blocked redirect should stop before call 3", got)
	}
	if len(decisions) != 3 || decisions[0].Kind != TargetNavigation || decisions[1].Kind != TargetRedirect || decisions[2].Kind != TargetRedirect || decisions[2].Action != DecisionDeny {
		t.Fatalf("redirect decisions=%+v", decisions)
	}
}

func TestSecurityGateDNSRebindingIsDeniedAtSocketBoundary(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{
		{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}},
		{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
	}}
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	client, err := NewHTTPClient(HTTPClientConfig{Policy: policy, Timeout: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.Do(context.Background(), Request{URL: "http://rebind.example/"})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("rebound request error=%v, want policy denial", err)
	}
	resolver.mu.Lock()
	calls := resolver.calls
	resolver.mu.Unlock()
	if calls != 2 {
		t.Fatalf("resolver calls=%d, want admission plus socket revalidation", calls)
	}
}

func TestSecurityGateUnknownRequestBodyLengthFailsClosed(t *testing.T) {
	resolver := securityGateResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	policy := mustPolicy(t, PolicyConfig{MaxRequestBodyBytes: 4}, resolver, nil)
	client, err := NewHTTPClient(HTTPClientConfig{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	var roundTrips atomic.Int64
	client.client.Transport = securityGateRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		roundTrips.Add(1)
		return nil, errors.New("transport must not be reached")
	})
	_, err = client.Do(context.Background(), Request{
		Method:  http.MethodPost,
		URL:     "https://public.example/upload",
		Body:    securityGateUnknownLengthReader{Reader: strings.NewReader("oversized")},
		Headers: http.Header{"Content-Type": []string{"text/plain"}},
	})
	if !errors.Is(err, ErrPolicyDenied) || roundTrips.Load() != 0 {
		t.Fatalf("unknown-length body error=%v roundTrips=%d", err, roundTrips.Load())
	}
	redirect, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://public.example/redirected-upload", securityGateUnknownLengthReader{Reader: strings.NewReader("oversized")})
	if err != nil {
		t.Fatal(err)
	}
	redirect.Header.Set("Content-Type", "text/plain")
	if err := client.client.CheckRedirect(redirect, []*http.Request{{}}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("unknown-length redirect body error=%v", err)
	}
}

func TestSecurityGateConcurrentPolicyRace(t *testing.T) {
	resolver := securityGateResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	var decisions atomic.Int64
	policy := mustPolicy(t, PolicyConfig{AllowedDomains: []string{"*.example"}}, resolver, func(Decision) error {
		decisions.Add(1)
		return nil
	})
	const workers = 32
	const iterations = 20
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				rawURL := fmt.Sprintf("https://worker-%d.example/item/%d", worker, iteration)
				if _, err := policy.ResolveURL(context.Background(), rawURL, TargetSubresource, "race-gate"); err != nil {
					errorsFound <- err
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent policy evaluation: %v", err)
	}
	if got, want := decisions.Load(), int64(workers*iterations); got != want {
		t.Fatalf("decision count=%d, want %d", got, want)
	}
}

func TestSecurityGateRequestContextCancellation(t *testing.T) {
	resolver := securityGateResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	client, err := NewHTTPClient(HTTPClientConfig{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	client.client.Transport = securityGateRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		close(entered)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, requestErr := client.Do(ctx, Request{URL: "https://public.example/wait"})
		done <- requestErr
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter transport")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request cancellation error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request survived context cancellation")
	}
}

func TestSecurityGateLifecycleCancellationReachesPolicyResolution(t *testing.T) {
	var cancellationObserved atomic.Bool
	resolver := securityGateResolverFunc(func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
		select {
		case <-ctx.Done():
			cancellationObserved.Store(true)
			return nil, ctx.Err()
		default:
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		}
	})
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	client, err := NewHTTPClient(HTTPClientConfig{Policy: policy, RequestLifecycle: securityGateCancelledLifecycle{}})
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = securityGateRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("cancelled request reached transport")
	})
	if _, err := client.Do(context.Background(), Request{URL: "https://public.example/cancelled"}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("lifecycle cancellation error=%v", err)
	}
	if !cancellationObserved.Load() {
		t.Fatal("policy resolution did not receive the lifecycle-owned cancellation context")
	}
}

func FuzzSecurityGatePolicyURL(f *testing.F) {
	for _, seed := range []string{
		"https://example.com/path",
		"http://127.0.0.1/",
		"http://2130706433/",
		"http://[::ffff:127.0.0.1]/",
		"http://user:secret@example.com/",
		"file:///etc/passwd",
	} {
		f.Add(seed)
	}
	resolver := securityGateResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	policy, err := NewPolicy(PolicyConfig{}, resolver, nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, rawURL string) {
		addresses, resolveErr := policy.ResolveURL(context.Background(), rawURL, TargetNavigation, "")
		if resolveErr != nil {
			return
		}
		parsed, parseErr := parsePolicyURL(rawURL)
		if parseErr != nil || parsed.User != nil {
			t.Fatalf("allowed URL failed canonical parse: %q err=%v", rawURL, parseErr)
		}
		host, hostErr := normalizePolicyHost(parsed.Hostname())
		port, portErr := policyPort(parsed)
		if hostErr != nil || blockedHostname(host) || portErr != nil || !containsString(policy.config.AllowedSchemes, parsed.Scheme) || !containsInt(policy.config.AllowedPorts, port) {
			t.Fatalf("allowed URL violated canonical policy: %q host=%q port=%d", rawURL, host, port)
		}
		if len(addresses) == 0 {
			t.Fatalf("allowed URL returned no addresses: %q", rawURL)
		}
		for _, address := range addresses {
			if blockedAddress(address) || alwaysBlockedAddress(address) {
				t.Fatalf("allowed URL returned blocked address %s for %q", address, rawURL)
			}
		}
	})
}

func FuzzSecurityGatePolicyValidation(f *testing.F) {
	f.Add("example.com", uint16(443), uint16(8))
	f.Add("*.example.com", uint16(80), uint16(1))
	f.Add("*", uint16(0), uint16(0))
	f.Fuzz(func(t *testing.T, domain string, port uint16, maxBody uint16) {
		policy, err := NewPolicy(PolicyConfig{
			AllowedDomains:      []string{domain},
			AllowedPorts:        []int{int(port)},
			MaxRequestBodyBytes: int64(maxBody),
		}, nil, nil)
		if err != nil {
			return
		}
		config := policy.Config()
		if !sort.StringsAreSorted(config.AllowedDomains) || !sort.IntsAreSorted(config.AllowedPorts) || config.MaxRequestBodyBytes < 1 {
			t.Fatalf("invalid normalized config: %+v", config)
		}
		for _, allowedPort := range config.AllowedPorts {
			if allowedPort < 1 || allowedPort > 65535 {
				t.Fatalf("invalid port survived validation: %d", allowedPort)
			}
		}
		for _, pattern := range config.AllowedDomains {
			if pattern == "*" || pattern == "*." || strings.ContainsAny(pattern, "/:@") {
				t.Fatalf("invalid domain survived validation: %q", pattern)
			}
		}
		if err := policy.ValidateRequest(context.Background(), (&url.URL{Scheme: "https", Host: "example.com"}).String(), http.MethodPost, "text/plain", -1, TargetNavigation, ""); !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("unknown request body length was allowed: %v", err)
		}
	})
}
