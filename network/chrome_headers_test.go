package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestChromeLikeHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, ChromeOS: "macos", UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Do(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Sec-Ch-Ua", "Sec-Ch-Ua-Platform", "Sec-Fetch-Site", "Accept-Language", "Upgrade-Insecure-Requests"} {
		if got.Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
	if got.Get("Sec-Ch-Ua-Platform") != `"macOS"` {
		t.Errorf("platform: %q", got.Get("Sec-Ch-Ua-Platform"))
	}
	if got.Get("User-Agent") == "" {
		t.Error("UA missing")
	}
}
