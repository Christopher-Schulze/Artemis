package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChromeTransportNormalizesHTTPSAuthorityWithRealTLS(t *testing.T) {
	for _, test := range []struct {
		name, url, address string
		h2                 bool
	}{
		{"implicit h1", "https://artemis.example/content", "artemis.example:443", false},
		{"implicit h2", "https://artemis.example/content", "artemis.example:443", true},
		{"explicit port", "https://artemis.example:8443/content", "artemis.example:8443", false},
		{"IPv6 authority", "https://[2001:db8::1]/content", "[2001:db8::1]:443", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/content" {
					http.Error(w, "wrong path", http.StatusBadRequest)
					return
				}
				if _, err := io.WriteString(w, "Artemis TLS fixture"); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			server.EnableHTTP2 = test.h2
			server.StartTLS()
			defer server.Close()
			dialer := &net.Dialer{}
			transport := newChromeTransport(func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != test.address {
					return nil, fmt.Errorf("wrong HTTPS authority: %q, want %q", address, test.address)
				}
				return dialer.DialContext(ctx, network, server.Listener.Addr().String())
			}, nil, true)
			defer transport.CloseIdleConnections()
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, test.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil || string(body) != "Artemis TLS fixture" {
				t.Fatalf("real response=%q read=%v close=%v", body, readErr, closeErr)
			}
			wantMajor := 1
			if test.h2 {
				wantMajor = 2
			}
			if response.ProtoMajor != wantMajor {
				t.Fatalf("negotiated protocol=%s", response.Proto)
			}
		})
	}
}

func TestChromeTransportPreservesPolicyDenial(t *testing.T) {
	policy, err := NewPolicy(DefaultPolicyConfig(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := newChromeTransport(policy.DialContextFor("denial-test"), nil, false)
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("private destination was not denied: %v", err)
	}
}
