package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRobotsCacheHitAndExpiry(t *testing.T) {
	rc := newRobotsCache()
	p := &RobotsPolicy{}
	rc.put("a.test", p)
	if got, ok := rc.get("a.test"); !ok || got == nil || got == p || !got.Allowed("test", "/public") {
		t.Fatal("fresh entry should hit")
	}
	if _, ok := rc.get("missing.test"); ok {
		t.Fatal("unknown host must miss")
	}
	// Stale entry evicts on read.
	rc.mu.Lock()
	e := rc.by["a.test"]
	e.fetchedAt = time.Now().Add(-robotsCacheTTL - time.Second)
	rc.by["a.test"] = e
	rc.mu.Unlock()
	if _, ok := rc.get("a.test"); ok {
		t.Fatal("expired entry must miss and self-evict")
	}
	if _, ok := rc.by["a.test"]; ok {
		t.Fatal("expired entry should be deleted from map")
	}
}

func TestRobotsCacheBoundsGrowth(t *testing.T) {
	rc := newRobotsCache()
	for i := 0; i < robotsCacheMaxHosts+64; i++ {
		rc.put("h"+strconv.Itoa(i)+".test", &RobotsPolicy{})
	}
	if len(rc.by) > robotsCacheMaxHosts {
		t.Fatalf("cache grew beyond cap: %d", len(rc.by))
	}
}

func TestRobotsCacheEvictsStalest(t *testing.T) {
	rc := newRobotsCache()
	for i := 0; i < robotsCacheMaxHosts; i++ {
		rc.put("h"+strconv.Itoa(i)+".test", &RobotsPolicy{})
	}
	rc.mu.Lock()
	e := rc.by["h0.test"]
	e.fetchedAt = time.Now().Add(-2 * robotsCacheTTL)
	rc.by["h0.test"] = e
	rc.mu.Unlock()
	rc.put("new.test", &RobotsPolicy{})
	if _, ok := rc.by["h0.test"]; ok {
		t.Fatal("stalest entry should be evicted first")
	}
	if _, ok := rc.by["new.test"]; !ok {
		t.Fatal("new entry should be present")
	}
}

func TestFetchRobotsCachesPerHost(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	u2, _ := url.Parse(srv.URL + "/page")
	p1, err := c.FetchRobots(context.Background(), u2)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := c.FetchRobots(context.Background(), u2)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 robots fetch, got %d", hits.Load())
	}
	if p1 == nil || p2 == nil || p1 == p2 || p1.Allowed("test", "/private/page") || p2.Allowed("test", "/private/page") {
		t.Fatal("detached cached policy should retain the actual rules")
	}
}

func TestFetchRobots404AllowsAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, _ := NewHTTPClient(HTTPClientConfig{Policy: policy})
	defer c.Close()
	u2, _ := url.Parse(srv.URL + "/x")
	p, err := c.FetchRobots(context.Background(), u2)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || !p.Allowed("test", "/private") || len(p.Groups) != 0 {
		t.Fatalf("missing robots did not admit an explicit empty policy: %+v", p)
	}
}

func TestFetchRobotsRejectsBadInput(t *testing.T) {
	c, _ := NewHTTPClient(HTTPClientConfig{})
	defer c.Close()
	if _, err := c.FetchRobots(context.Background(), nil); err == nil {
		t.Fatal("nil URL must fail")
	}
	if _, err := c.FetchRobots(context.Background(), &url.URL{Path: "/x"}); err == nil {
		t.Fatal("relative URL must fail")
	}
	//lint:ignore SA1012 Deliberately test rejection of a nil context.
	if _, err := c.FetchRobots(nil, &url.URL{Scheme: "http", Host: "h"}); err == nil {
		t.Fatal("nil ctx must fail")
	}
}

func TestRobotsCachePayloadBudgetEvictsWithoutKeepingOversizePolicy(t *testing.T) {
	cache := newRobotsCache()
	policy := &RobotsPolicy{Groups: map[string][]RobotsRule{"*": {{Path: "/" + strings.Repeat("x", 3<<20)}}}}
	cache.put("oldest", policy)
	cache.mu.Lock()
	entry := cache.by["oldest"]
	entry.fetchedAt = time.Now().Add(-time.Hour)
	cache.by["oldest"] = entry
	cache.mu.Unlock()
	cache.put("second", policy)
	cache.put("third", policy)
	if _, exists := cache.get("oldest"); exists || len(cache.by) != 2 || cache.payloadBytes > robotsCacheMaxBytes {
		t.Fatalf("payload budget failed: entries=%d bytes=%d oldest=%t", len(cache.by), cache.payloadBytes, exists)
	}
	oversize := &RobotsPolicy{Sitemaps: []string{strings.Repeat("x", robotsCacheMaxBytes+1)}}
	cache.put("oversize", oversize)
	if _, exists := cache.get("oversize"); exists {
		t.Fatal("oversize policy remained cached")
	}
	cache.releaseSession("unrelated")
	cache.close()
	if len(cache.by) != 0 || cache.payloadBytes != 0 {
		t.Fatalf("close retained policy payload: %+v", cache)
	}
}

func TestRobotsFlightAndOwnerAdmissionBounds(t *testing.T) {
	for _, test := range []struct {
		name   string
		limit  int
		owners bool
	}{{"in-flight origins", 128, false}, {"session owners", MaxCookieSessions + 1, true}} {
		t.Run(test.name, func(t *testing.T) {
			cache := newRobotsCache()
			defer cache.close()
			for index := range test.limit {
				owner := "shared"
				if test.owners {
					owner = strconv.Itoa(index)
				}
				_, flight, leader, err := cache.begin(owner, robotsCacheIdentity(owner, strconv.Itoa(index)))
				if err != nil || !leader || flight == nil {
					t.Fatalf("valid admission %d: flight=%v leader=%t error=%v", index, flight, leader, err)
				}
				if test.owners {
					if _, err := cache.complete(flight, &RobotsPolicy{}, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			owner := "shared"
			if test.owners {
				owner = "overflow"
			}
			if policy, flight, _, err := cache.begin(owner, robotsCacheIdentity(owner, "overflow")); err == nil || policy != nil || flight != nil {
				t.Fatalf("overflow admitted: policy=%v flight=%v error=%v", policy, flight, err)
			}
		})
	}
}
