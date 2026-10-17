package network

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPolicyRejectsInvalidSuppliedConfigurationBeforeResolution(t *testing.T) {
	for _, test := range []struct {
		name   string
		config PolicyConfig
	}{
		{"blank domain", PolicyConfig{AllowedDomains: []string{" "}}},
		{"empty label", PolicyConfig{AllowedDomains: []string{"api..example"}}},
		{"wildcard IP", PolicyConfig{AllowedDomains: []string{"*.127.0.0.1"}}},
		{"label hyphen", PolicyConfig{AllowedDomains: []string{"-api.example"}}},
		{"blank method", PolicyConfig{AllowedMethods: []string{" "}}},
		{"method newline", PolicyConfig{AllowedMethods: []string{"GET\nPOST"}}},
		{"scheme", PolicyConfig{AllowedSchemes: []string{"file"}}},
		{"blank scheme", PolicyConfig{AllowedSchemes: []string{""}}},
		{"request media", PolicyConfig{AllowedContentTypes: []string{"text/*"}}},
		{"media parameter", PolicyConfig{AllowedContentTypes: []string{"text/plain; charset=utf-8"}}},
		{"blank download media", PolicyConfig{AllowedDownloadTypes: []string{""}}},
		{"negative request limit", PolicyConfig{MaxRequestBodyBytes: -1}},
		{"negative response limit", PolicyConfig{MaxResponseBodyBytes: -1}},
		{"negative download limit", PolicyConfig{MaxDownloadBytes: -1}},
		{"overflow response limit", PolicyConfig{MaxResponseBodyBytes: math.MaxInt64}},
		{"negative redirects", PolicyConfig{MaxRedirects: -1}},
		{"negative timeout", PolicyConfig{DialTimeout: -time.Second}},
		{"domain count", PolicyConfig{AllowedDomains: make([]string, 257)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &sequenceResolver{}
			if policy, err := NewPolicy(test.config, resolver, nil); err == nil || policy != nil || resolver.calls != 0 {
				t.Fatalf("invalid config admitted: policy=%v error=%v lookups=%d", policy, err, resolver.calls)
			}
		})
	}
}

func TestPolicyNormalizesAndDetachesEveryInputList(t *testing.T) {
	config := PolicyConfig{
		AllowedSchemes: []string{" HTTPS "}, AllowedDomains: []string{"*.Bücher.example."},
		AllowedPorts: []int{443}, AllowedMethods: []string{" get "},
		AllowedContentTypes: []string{"Text/Plain"}, AllowedDownloadTypes: []string{"Text/*"},
	}
	resolver := &sequenceResolver{results: []resolverResult{{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}}}
	policy := mustPolicy(t, config, resolver, nil)
	config.AllowedSchemes[0], config.AllowedDomains[0], config.AllowedMethods[0] = "file", "evil.example", "DELETE"
	config.AllowedPorts[0], config.AllowedContentTypes[0], config.AllowedDownloadTypes[0] = 1, "text/html", "*/*"
	if err := policy.ValidateRequest(t.Context(), "https://api.bücher.example/", "GET", "text/plain; charset=utf-8", 1, TargetNavigation, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateDownload("text/plain", 1, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateDownload("text/html", 1, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateDownload("application/json", 1, "owner"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("input slice mutation granted download type: %v", err)
	}
}

type deadlineResolver struct{ calls atomic.Int64 }

func (r *deadlineResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	r.calls.Add(1)
	<-ctx.Done()
	return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
}

func TestPolicyBoundsDNSAndPreservesCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		dial bool
	}{{"resolve", false}, {"dial", true}} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &deadlineResolver{}
			policy := mustPolicy(t, PolicyConfig{DialTimeout: 20 * time.Millisecond}, resolver, nil)
			started := time.Now()
			var err error
			if test.dial {
				_, err = policy.DialContext(t.Context(), "tcp", "bounded.example:443")
			} else {
				_, err = policy.ResolveURL(t.Context(), "https://bounded.example/", TargetNavigation, "owner")
			}
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrPolicyDenied) || resolver.calls.Load() != 1 || time.Since(started) > time.Second {
				t.Fatalf("DNS deadline/cause lost: %v lookups=%d duration=%s", err, resolver.calls.Load(), time.Since(started))
			}
		})
	}
	var typedNil *deadlineResolver
	if _, err := NewPolicy(PolicyConfig{}, typedNil, nil); err == nil {
		t.Fatal("typed nil resolver admitted")
	}
}

func TestPolicyRejectsUnownedMalformedAndCancelledInputsWithoutDNS(t *testing.T) {
	resolver := &sequenceResolver{}
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	var absent context.Context
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name   string
		owner  *Policy
		ctx    context.Context
		target string
		kind   TargetKind
	}{
		{"nil owner", nil, t.Context(), "https://example.com/", TargetNavigation},
		{"zero owner", &Policy{}, t.Context(), "https://example.com/", TargetNavigation},
		{"nil context", policy, absent, "https://example.com/", TargetNavigation},
		{"cancelled", policy, cancelled, "https://example.com/", TargetNavigation},
		{"kind", policy, t.Context(), "https://example.com/", TargetKind("invented")},
		{"whitespace", policy, t.Context(), " https://example.com/", TargetNavigation},
		{"empty port", policy, t.Context(), "https://example.com:/", TargetNavigation},
		{"empty label", policy, t.Context(), "https://api..example/", TargetNavigation},
		{"empty numeric label", policy, t.Context(), "https://1..1/", TargetNavigation},
		{"oversize", policy, t.Context(), "https://example.com/" + strings.Repeat("x", 8192), TargetNavigation},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.owner.ResolveURL(test.ctx, test.target, test.kind, "owner"); !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("admission error=%v", err)
			}
		})
	}
	if _, err := policy.DialContext(t.Context(), "udp", "1.1.1.1:443"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("UDP escaped TCP owner: %v", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("invalid admission performed %d DNS lookups", resolver.calls)
	}
}

func TestHTTPActualCallerHeadersAndMethodAreAdmittedOnce(t *testing.T) {
	type requestEvidence struct {
		method         string
		agents, accept []string
	}
	observed := make(chan requestEvidence, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- requestEvidence{r.Method, r.Header.Values("User-Agent"), r.Header.Values("Accept")}
		writeTestHTTPBody(t, w, "observed")
	}))
	defer server.Close()
	client := newTestClient(t, HTTPClientConfig{ChromeLike: true, UserAgent: "configured/1"})
	headers := http.Header{"user-agent": []string{"caller/2"}, "accept": []string{"application/json"}}
	response, err := client.Do(t.Context(), Request{Method: "get", URL: server.URL, Headers: headers})
	if err != nil || string(response.Body) != "observed" {
		t.Fatalf("actual request: response=%v error=%v", response, err)
	}
	evidence := <-observed
	if evidence.method != "GET" || len(evidence.agents) != 1 || evidence.agents[0] != "caller/2" || len(evidence.accept) != 1 || evidence.accept[0] != "application/json" {
		t.Fatalf("wire request diverged: %+v", evidence)
	}
	if headers["User-Agent"] != nil || headers.Get("Accept") != "" {
		t.Fatal("caller header map mutated")
	}
}

func TestHTTPInvalidIdentityHasZeroActualDispatches(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); writeTestHTTPBody(t, w, "unexpected") }))
	defer server.Close()
	client := newTestClient(t, HTTPClientConfig{})
	for _, test := range []struct {
		name    string
		headers http.Header
		body    string
	}{
		{"missing type", nil, "payload"},
		{"invalid type", http.Header{"Content-Type": []string{"bogus"}}, "payload"},
		{"duplicate type", http.Header{"Content-Type": []string{"text/plain", "application/json"}}, "payload"},
		{"case collision", http.Header{"Content-Type": []string{"text/plain"}, "content-type": []string{"text/plain"}}, "payload"},
		{"forged length", http.Header{"Content-Type": []string{"text/plain"}, "Content-Length": []string{"1"}}, "payload"},
		{"transport host", http.Header{"Host": []string{"other.example"}}, ""},
		{"proxy credentials", http.Header{"Proxy-Authorization": []string{"operator-value"}}, ""},
		{"newline", http.Header{"X-Custom": []string{"value\r\nX-Injected: true"}}, ""},
		{"header size", http.Header{"X-Custom": []string{strings.Repeat("x", 4097)}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := Request{URL: server.URL, Headers: test.headers}
			if test.body != "" {
				request.Method = "POST"
				request.Body = strings.NewReader(test.body)
			}
			if response, err := client.Do(t.Context(), request); response != nil || !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("invalid identity admitted: response=%v error=%v", response, err)
			}
			if hits.Load() != 0 {
				t.Fatalf("denied request reached fixture %d times", hits.Load())
			}
		})
	}
}

func TestHTTPProxyAndLimitAdmissionAvoidsNetworkEffects(t *testing.T) {
	for _, proxy := range []string{"ws://127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?secret=value", "http://user:secret@127.0.0.1", "http://127.0.0.1:"} {
		if client, err := NewHTTPClient(HTTPClientConfig{ProxyURL: proxy}); client != nil || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid proxy admitted/leaked: %v %v", client, err)
		}
	}
	for _, config := range []HTTPClientConfig{{Timeout: -1}, {MaxBodyBytes: -1}, {ChromeLike: true, ProxyURL: "https://127.0.0.1"}} {
		if client, err := NewHTTPClient(config); client != nil || err == nil {
			t.Fatalf("invalid transport admitted: %v %v", client, err)
		}
	}
}

func TestHTTPActualBodyRedirectAuthority(t *testing.T) {
	var deniedHits atomic.Int64
	denied := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { deniedHits.Add(1) }))
	defer denied.Close()
	var finalHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
		case "/cross":
			http.Redirect(w, r, denied.URL, http.StatusTemporaryRedirect)
		case "/final":
			finalHits.Add(1)
			if (r.Method != "POST" && r.Method != "GET") || r.Header.Get("Content-Type") != "text/plain" {
				t.Error("redirect lost body authority")
			}
			if _, err := io.Copy(w, r.Body); err != nil {
				t.Errorf("actual redirected body: %v", err)
			}
		}
	}))
	defer server.Close()
	client := newTestClient(t, HTTPClientConfig{})
	for _, test := range []struct {
		path, method string
		deny         bool
	}{{"/same", "POST", false}, {"/cross", "POST", true}, {"/same", "GET", false}, {"/cross", "GET", true}} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response, err := client.Do(t.Context(), Request{URL: server.URL + test.path, Method: test.method, Body: strings.NewReader("actual-body"), Headers: http.Header{"Content-Type": []string{"text/plain"}}})
			if test.deny {
				if response != nil || !errors.Is(err, ErrPolicyDenied) {
					t.Fatalf("cross-origin body escaped: %v %v", response, err)
				}
				return
			}
			if err != nil || response == nil || string(response.Body) != "actual-body" || response.FinalURL != server.URL+"/final" {
				t.Fatalf("same-origin actual redirect: %v %v", response, err)
			}
		})
	}
	if finalHits.Load() != 2 || deniedHits.Load() != 0 {
		t.Fatalf("redirect effects allowed=%d denied=%d", finalHits.Load(), deniedHits.Load())
	}
}
