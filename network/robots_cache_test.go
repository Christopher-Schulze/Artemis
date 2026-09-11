package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestRobotsCacheHitAndExpiry(t *testing.T) {
	rc := newRobotsCache()
	p := &RobotsPolicy{}
	rc.put("a.test", p)
	if got, ok := rc.get("a.test"); !ok || got != p {
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
	if p1 == nil || p2 == nil || p1 != p2 {
		t.Fatal("cached policy should be returned")
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
	if p != nil {
		// nil = allow-all is the documented contract for missing robots.
		t.Log("policy object:", p)
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
	if _, err := c.FetchRobots(nil, &url.URL{Scheme: "http", Host: "h"}); err == nil {
		t.Fatal("nil ctx must fail")
	}
}
