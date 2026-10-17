package network

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

func TestCookieSessionsUseOwnedJarAcrossRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			http.SetCookie(w, &http.Cookie{Name: "owner", Value: r.URL.Query().Get("owner"), Path: "/"})
			http.Redirect(w, r, "/read", http.StatusFound)
			return
		}
		writeTestHTTPBody(t, w, r.Header.Get("Cookie"))
	}))
	defer srv.Close()
	client := newTestClient(t, HTTPClientConfig{})
	cases := []struct{ session, path, want string }{
		{"alice", "/seed?owner=alice", "owner=alice"},
		{"bob", "/read", ""},
		{"bob", "/seed?owner=bob", "owner=bob"},
		{"alice", "/read", "owner=alice"},
		{"", "/read", ""},
	}
	for _, tc := range cases {
		t.Run(tc.session+tc.path, func(t *testing.T) {
			response, err := client.Do(WithSessionID(context.Background(), tc.session), Request{URL: srv.URL + tc.path})
			if err != nil {
				t.Fatal(err)
			}
			if string(response.Body) != tc.want {
				t.Fatalf("cookie=%q want=%q", response.Body, tc.want)
			}
		})
	}
	if err := client.ReleaseSession("alice"); err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(WithSessionID(context.Background(), "alice"), Request{URL: srv.URL + "/read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Body) != 0 {
		t.Fatalf("released state reappeared: %q", response.Body)
	}
}

func TestCookieSessionAdmissionAndPublicSuffix(t *testing.T) {
	client := newTestClient(t, HTTPClientConfig{})
	for i := 0; i < MaxCookieSessions; i++ {
		if _, err := client.CookieJarForSession(fmt.Sprintf("owner-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.CookieJarForSession("overflow"); err == nil {
		t.Fatal("unbounded session retention")
	}
	if err := client.ReleaseSession("owner-0"); err != nil {
		t.Fatal(err)
	}
	jar, err := client.CookieJarForSession("replacement")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse("https://alice.co.uk/")
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "cross-owner", Value: "denied", Domain: "co.uk", Path: "/"}})
	other, err := url.Parse("https://bob.co.uk/")
	if err != nil {
		t.Fatal(err)
	}
	if len(jar.Cookies(other)) != 0 {
		t.Fatal("public-suffix cookie crossed domains")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CookieJarForSession("replacement"); err == nil {
		t.Fatal("closed client admitted cookie access")
	}
}

func TestConcurrentCookieSessionPartitions(t *testing.T) {
	client := newTestClient(t, HTTPClientConfig{})
	u, err := url.Parse("https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func(owner string) {
			defer workers.Done()
			jar, err := client.CookieJarForSession(owner)
			if err != nil {
				t.Error(err)
				return
			}
			jar.SetCookies(u, []*http.Cookie{{Name: "owner", Value: owner, Path: "/"}})
			cookies := jar.Cookies(u)
			if len(cookies) != 1 || cookies[0].Value != owner {
				t.Errorf("cookie partition %s: %v", owner, cookies)
			}
			if err := client.ReleaseSession(owner); err != nil {
				t.Error(err)
			}
		}(fmt.Sprintf("owner-%d", i))
	}
	workers.Wait()
	if len(client.sessionJars) != 0 {
		t.Fatalf("retained partitions=%d", len(client.sessionJars))
	}
}

func TestConfiguredDefaultCookieSessionIsReleased(t *testing.T) {
	client := newTestClient(t, HTTPClientConfig{SessionID: "owned-default"})
	u, err := url.Parse("https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	jar, err := client.CookieJarForSession("owned-default")
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "owner", Value: "first", Path: "/"}})
	if err := client.ReleaseSession("owned-default"); err != nil {
		t.Fatal(err)
	}
	jar, err = client.CookieJarForSession("owned-default")
	if err != nil {
		t.Fatal(err)
	}
	if len(jar.Cookies(u)) != 0 {
		t.Fatal("default owner state survived release")
	}
}
