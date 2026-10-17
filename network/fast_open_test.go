package network

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestHTTPFastOpenOwnedSocketEvidenceAndOptOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestHTTPBody(t, w, "actual HTTP peer")
	}))
	defer server.Close()
	for _, tc := range []struct {
		name                        string
		enabled, eligible, admitted bool
		wantReceipt                 bool
	}{
		{"attempt", true, true, true, true},
		{"disabled", false, true, true, false},
		{"read first or non-idempotent", true, false, true, false},
		{"host unsupported", true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var receipts []Decision
			config := DefaultPolicyConfig()
			config.AllowPrivateNetworks, config.AllowedPorts = true, allTestPorts()
			policy := mustPolicy(t, config, nil, func(decision Decision) error {
				mu.Lock()
				defer mu.Unlock()
				if decision.TCPFastOpen != nil {
					receipts = append(receipts, decision)
				}
				return nil
			})
			caps := platform.PlatformCapabilities{HasTCPFastOpen: tc.admitted}
			caps.Evidence.TCPFastOpen.State = platform.StateAvailable
			transport := &http.Transport{DialContext: policy.httpDialContextFor("owned-session", caps, tc.enabled)}
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			ctx = context.WithValue(ctx, fastOpenRequestKey{}, tc.eligible)
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := (&http.Client{Transport: transport}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil || string(body) != "actual HTTP peer" {
				t.Fatalf("actual peer failed: %q %v %v", body, readErr, closeErr)
			}
			mu.Lock()
			defer mu.Unlock()
			if (len(receipts) == 1) != tc.wantReceipt {
				t.Fatalf("socket activation receipt count = %d", len(receipts))
			}
			if tc.wantReceipt {
				receipt := receipts[0]
				if receipt.SessionID != "owned-session" || receipt.Host != "127.0.0.1" || receipt.Port == 0 || receipt.TCPFastOpen.Source == "" || receipt.TCPFastOpen.Reason == "" {
					t.Fatalf("socket receipt lost owner or execution evidence: %+v", receipt)
				}
				if receipt.TCPFastOpen.State != platform.StateAvailable && receipt.TCPFastOpen.Value != 0 {
					t.Fatalf("fallback forged activation: %+v", receipt)
				}
			}
		})
	}
}

func TestHTTPFastOpenCannotBypassPolicyOrFailedAudit(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeTestHTTPBody(t, w, "unauthorized")
	}))
	defer server.Close()
	for _, allowPrivate := range []bool{false, true} {
		config := DefaultPolicyConfig()
		config.AllowPrivateNetworks, config.AllowedPorts = allowPrivate, allTestPorts()
		policy := mustPolicy(t, config, nil, func(decision Decision) error {
			if decision.TCPFastOpen != nil {
				return errors.New("activation audit rejected")
			}
			return nil
		})
		caps := platform.PlatformCapabilities{HasTCPFastOpen: true}
		caps.Evidence.TCPFastOpen.State = platform.StateAvailable
		transport := &http.Transport{DialContext: policy.httpDialContextFor("owned-session", caps, true)}
		ctx := context.WithValue(t.Context(), fastOpenRequestKey{}, true)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := (&http.Client{Transport: transport, Timeout: time.Second}).Do(request)
		transport.CloseIdleConnections()
		if response != nil || !errors.Is(err, ErrPolicyDenied) || hits.Load() != 0 {
			t.Fatalf("policy/audit rejection sent a request: %+v %v hits=%d", response, err, hits.Load())
		}
	}
}
