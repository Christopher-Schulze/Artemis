package network

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func robotsURLForTest(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestRobotsRetrievalUsesRealBoundedTransportAndNeverCachesFailure(t *testing.T) {
	for _, test := range []struct {
		name              string
		status            int
		contentType, body string
		wantError         bool
	}{
		{"rules", 200, "text/plain", "User-agent: *\nDisallow: /private\n", false},
		{"missing", 404, "text/plain", "missing", false}, {"gone", 410, "text/plain", "gone", false},
		{"server error", 500, "text/plain", "failure", true}, {"denied rules", 403, "text/plain", "denied", true},
		{"rate limited", 429, "text/plain", "slow", true}, {"HTML challenge", 200, "text/html", "<html>challenge</html>", true},
		{"body cap", 200, "text/plain", strings.Repeat("x", int(MaxRobotsDocumentBytes)+1), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path != "/robots.txt" {
					t.Errorf("wrong robots target: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				writeTestHTTPBody(t, w, test.body)
			}))
			defer server.Close()
			client := newTestClient(t, HTTPClientConfig{})
			for range 2 {
				policy, err := client.FetchRobots(t.Context(), robotsURLForTest(t, server.URL+"/page?token=value"))
				if test.wantError {
					if err == nil || policy != nil {
						t.Fatalf("failed rules became allow state: policy=%v error=%v", policy, err)
					}
					continue
				}
				if err != nil || policy == nil {
					t.Fatalf("valid retrieval: %v %v", policy, err)
				}
				if policy.Allowed("bot", "/private") == (test.status == 200) {
					t.Fatalf("wrong status/rule outcome: %+v", policy)
				}
			}
			want := int64(1)
			if test.wantError {
				want = 2
			}
			if hits.Load() != want {
				t.Fatalf("cache effect requests=%d want=%d", hits.Load(), want)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "gzip")
		body := encodeResponseFixture(t, []byte(strings.Repeat("x", int(MaxRobotsDocumentBytes)+1)), "gzip")
		if _, err := w.Write(body); err != nil {
			t.Errorf("encoded body: %v", err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, HTTPClientConfig{ChromeLike: true})
	if policy, err := client.FetchRobots(t.Context(), robotsURLForTest(t, server.URL)); policy != nil || !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("decompression limit bypass: %v %v", policy, err)
	}
}

func TestRobotsPolicyDenialAndSchemeDifferenceCannotBorrowCachedAllow(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(404) }))
	defer server.Close()
	secure, err := NewHTTPClient(HTTPClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer secure.Close()
	if policy, err := secure.FetchRobots(t.Context(), robotsURLForTest(t, server.URL)); policy != nil || !errors.Is(err, ErrPolicyDenied) || hits.Load() != 0 {
		t.Fatalf("denied robots became permission: policy=%v error=%v hits=%d", policy, err, hits.Load())
	}
	client := newTestClient(t, HTTPClientConfig{Timeout: time.Second})
	if _, err := client.FetchRobots(t.Context(), robotsURLForTest(t, server.URL)); err != nil {
		t.Fatal(err)
	}
	https := strings.Replace(server.URL, "http://", "https://", 1)
	if policy, err := client.FetchRobots(t.Context(), robotsURLForTest(t, https)); policy != nil || err == nil {
		t.Fatalf("HTTPS reused HTTP permission: policy=%v error=%v", policy, err)
	}
}

func TestRobotsSessionCookiesDetachedCacheAndReleaseAreActual(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		value := "empty"
		if cookie, err := r.Cookie("owner"); err == nil {
			value = cookie.Value
		}
		w.Header().Set("Content-Type", "text/plain")
		writeTestHTTPBody(t, w, "User-agent: *\nDisallow: /"+value+"\nCrawl-delay: 0.125\nSitemap: https://example.com/map.xml\n")
	}))
	defer server.Close()
	client := newTestClient(t, HTTPClientConfig{})
	u := robotsURLForTest(t, server.URL)
	for _, owner := range []string{"alpha", "beta"} {
		jar, err := client.CookieJarForSession(owner)
		if err != nil {
			t.Fatal(err)
		}
		jar.SetCookies(u, []*http.Cookie{{Name: "owner", Value: owner, Path: "/"}})
		policy, err := client.FetchRobots(WithSessionID(t.Context(), owner), u)
		if err != nil || policy.Allowed("bot", "/"+owner) || !policy.Allowed("bot", "/empty") {
			t.Fatalf("cookie identity not isolated: %v %v", policy, err)
		}
	}
	ctx := WithSessionID(t.Context(), "alpha")
	first, err := client.FetchRobots(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	first.Groups["*"][0].Allow = true
	first.CrawlDelays["*"] = 0
	first.Sitemaps[0] = "https://changed.example/"
	second, err := client.FetchRobots(ctx, u)
	if err != nil || second == first || second.Allowed("bot", "/alpha") || second.CrawlDelayFor("bot") != 125*time.Millisecond || second.Sitemaps[0] != "https://example.com/map.xml" {
		t.Fatalf("caller mutation reached cache: %v %v", second, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("session cache hits=%d", hits.Load())
	}
	if err := client.ReleaseSession("alpha"); err != nil {
		t.Fatal(err)
	}
	fresh, err := client.FetchRobots(ctx, u)
	if err != nil || fresh.Allowed("bot", "/empty") || !fresh.Allowed("bot", "/alpha") || hits.Load() != 3 {
		t.Fatalf("released owner recovered cookies/rules: %v %v hits=%d", fresh, err, hits.Load())
	}
}

func TestRobotsConcurrentFetchHasOneWireEffectAndOwnedCancellation(t *testing.T) {
	var hits atomic.Int64
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var unblock sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		writeTestHTTPBody(t, w, "User-agent: *\nDisallow: /private\n")
	}))
	defer func() { unblock.Do(func() { close(release) }); server.Close() }()
	client := newTestClient(t, HTTPClientConfig{})
	u := robotsURLForTest(t, server.URL)
	results := make(chan error, 12)
	var callers sync.WaitGroup
	callers.Add(1)
	go func() { defer callers.Done(); _, err := client.FetchRobots(t.Context(), u); results <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual robots request did not start")
	}
	waiterCtx, waiterCancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	_, waiterErr := client.FetchRobots(waiterCtx, u)
	waiterCancel()
	if !errors.Is(waiterErr, context.DeadlineExceeded) {
		t.Fatalf("waiter cancellation lost: %v", waiterErr)
	}
	for range 11 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			policy, err := client.FetchRobots(t.Context(), u)
			if err == nil && policy.Allowed("bot", "/private") {
				err = errors.New("concurrent permission changed")
			}
			results <- err
		}()
	}
	unblock.Do(func() { close(release) })
	callers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("duplicate actual robots fetches=%d", hits.Load())
	}
}

func TestRobotsCloseAndReleaseCancelOutstandingFetchWithoutPublishing(t *testing.T) {
	for _, test := range []struct {
		name        string
		closeClient bool
	}{{"close", true}, {"release", false}} {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{})
			cancelled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) }))
			defer server.Close()
			client := newTestClient(t, HTTPClientConfig{})
			defer client.Close()
			result := make(chan error, 1)
			go func() {
				policy, err := client.FetchRobots(WithSessionID(t.Context(), "owner"), robotsURLForTest(t, server.URL))
				if policy != nil {
					err = errors.New("retired owner published policy")
				}
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			if test.closeClient {
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := client.ReleaseSession("owner"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-result:
				if !errors.Is(err, ErrRobotsOwnerClosed) {
					t.Fatalf("retired owner error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("fetch survived owner retirement")
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("actual HTTP request was not cancelled")
			}
			client.robots.mu.Lock()
			count := len(client.robots.by)
			client.robots.mu.Unlock()
			if count != 0 {
				t.Fatalf("retired owner retained %d policies", count)
			}
		})
	}
}

func TestRobotsOriginIdentityAnd24HourBoundary(t *testing.T) {
	if robotsCacheTTL != 24*time.Hour {
		t.Fatalf("cache TTL=%s", robotsCacheTTL)
	}
	for _, test := range []struct{ input, want string }{{"https://EXAMPLE.com/path?token=value", "https://example.com:443/robots.txt"}, {"http://example.com:80/path", "http://example.com:80/robots.txt"}, {"http://[::1]:8080/path", "http://[::1]:8080/robots.txt"}} {
		t.Run(test.input, func(t *testing.T) {
			actual, err := robotsOriginURL(robotsURLForTest(t, test.input))
			if err != nil || actual != test.want {
				t.Fatalf("origin=%s want=%s error=%v", actual, test.want, err)
			}
		})
	}
	cache := newRobotsCache()
	cache.put("key", &RobotsPolicy{})
	cache.mu.Lock()
	entry := cache.by["key"]
	entry.fetchedAt = time.Now().Add(-23 * time.Hour)
	cache.by["key"] = entry
	cache.mu.Unlock()
	if _, ok := cache.get("key"); !ok {
		t.Fatal("valid 23-hour policy expired")
	}
	cache.mu.Lock()
	entry.fetchedAt = time.Now().Add(-24 * time.Hour)
	cache.by["key"] = entry
	cache.mu.Unlock()
	if _, ok := cache.get("key"); ok {
		t.Fatal("24-hour policy remained valid")
	}
	if robotsCacheIdentity("a", "b") == robotsCacheIdentity("ab", "") {
		t.Fatal("ambiguous session/origin identity")
	}
}

func TestRobotsCacheExpiryRevalidatesActualPresentAndMissingPolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		missing bool
	}{{"present", false}, {"missing", true}} {
		t.Run(test.name, func(t *testing.T) {
			var hits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hit := hits.Add(1)
				if test.missing && hit == 1 {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				writeTestHTTPBody(t, w, "User-agent: *\nDisallow: /fresh\n")
			}))
			defer server.Close()
			client := newTestClient(t, HTTPClientConfig{})
			target := robotsURLForTest(t, server.URL+"/fresh")
			if policy, err := client.FetchRobots(t.Context(), target); err != nil || policy == nil || policy.Allowed("bot", "/fresh") != test.missing {
				t.Fatalf("initial policy=%v error=%v", policy, err)
			}
			age := func(elapsed time.Duration) {
				client.robots.mu.Lock()
				defer client.robots.mu.Unlock()
				for key, entry := range client.robots.by {
					entry.fetchedAt = time.Now().Add(-elapsed)
					client.robots.by[key] = entry
				}
			}
			age(23 * time.Hour)
			if policy, err := client.FetchRobots(t.Context(), target); err != nil || policy == nil || hits.Load() != 1 {
				t.Fatalf("23-hour cache missed: policy=%v error=%v hits=%d", policy, err, hits.Load())
			}
			age(24 * time.Hour)
			policy, err := client.FetchRobots(t.Context(), target)
			if err != nil || policy == nil || policy.Allowed("bot", "/fresh") || hits.Load() != 2 {
				t.Fatalf("expired policy was not actually reloaded: policy=%v error=%v hits=%d", policy, err, hits.Load())
			}
		})
	}
}
