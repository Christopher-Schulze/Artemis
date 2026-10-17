package serve

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestControlClientRejectsCredentialAndNonNumericEndpoints(t *testing.T) {
	for _, endpoint := range []string{"ws://public.example/", "ws://localhost:9333/", "ws://127.0.0.1/?token=secret", "ws://user:secret@127.0.0.1/", "https://127.0.0.1/"} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := Dial(t.Context(), endpoint, "token", ""); err == nil {
				t.Fatal("unsafe endpoint admitted")
			}
		})
	}
	//lint:ignore SA1012 Deliberately test rejection of a nil context.
	if _, err := Dial(nil, "ws://127.0.0.1/", "token", ""); err == nil {
		t.Fatal("nil context admitted")
	}
	if _, err := Dial(t.Context(), "ws://127.0.0.1/", "", ""); err == nil {
		t.Fatal("empty token admitted")
	}
}

func TestClientLeaseCancellationAndOwnedServerClose(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			writeTestResponse(t, w, "<!doctype html><title>Released</title>")
		case <-r.Context().Done():
		}
	}))
	defer fixture.Close()
	agent := testChromiumAgent(t)
	defer stopTestAgent(t, agent)
	server := New(agent, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	host := httptest.NewServer(http.HandlerFunc(server.HandleWSForTest))
	defer host.Close()
	client, err := Dial(t.Context(), "ws"+strings.TrimPrefix(host.URL, "http")+"/", testAuthToken, "")
	if err != nil {
		t.Fatal(err)
	}
	var session SessionNewResult
	if err := client.Call(t.Context(), CmdSessionNew, SessionNewParams{}, &session); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- client.Call(t.Context(), CmdPageOpen, PageOpenParams{SessionID: session.SessionID, URL: fixture.URL}, nil)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("real fetch never reached fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := client.Call(ctx, CmdHeartbeat, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued call ignored its deadline: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("in-flight call succeeded after owned shutdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned shutdown left call running")
	}
	if len(agent.ListSessions()) != 0 {
		t.Fatal("server-owned session survived shutdown")
	}
	if _, err := Dial(t.Context(), "ws"+strings.TrimPrefix(host.URL, "http")+"/", testAuthToken, client.ClientID()); err == nil {
		t.Fatal("closed server admitted reconnect")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("repeat close: %v", err)
	}
	close(release)
}

func TestClientReconnectAndServerRestartDoNotRebindIdentity(t *testing.T) {
	addr, cleanup := startServer(t)
	client, err := Dial(t.Context(), "ws://"+addr+"/", testAuthToken, "")
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	identity := client.ClientID()
	var session SessionNewResult
	if err := client.Call(t.Context(), CmdSessionNew, SessionNewParams{}, &session); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		cleanup()
		t.Fatal(err)
	}
	reconnected, err := Dial(t.Context(), "ws://"+addr+"/", testAuthToken, identity)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	var sessions SessionListResult
	if err := reconnected.Call(t.Context(), CmdSessionList, nil, &sessions); err != nil || len(sessions.Sessions) != 1 || sessions.Sessions[0].SessionID != session.SessionID {
		cleanup()
		t.Fatalf("reconnect lost explicit identity: %+v %v", sessions, err)
	}
	if err := reconnected.Close(); err != nil {
		cleanup()
		t.Fatal(err)
	}
	cleanup()
	restarted, stop := startServer(t)
	defer stop()
	if old, err := Dial(t.Context(), "ws://"+restarted+"/", testAuthToken, identity); err == nil {
		closeTestWebsocketNow(t, old.connection)
		t.Fatal("new owner accepted previous server's capability")
	}
	fresh, err := Dial(t.Context(), "ws://"+restarted+"/", testAuthToken, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fresh.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := fresh.Call(t.Context(), CmdSessionList, nil, &sessions); err != nil || len(sessions.Sessions) != 0 {
		t.Fatalf("restart silently rebound old state: %+v %v", sessions, err)
	}
}

func TestClientReturnsConnectionRateDenialInsteadOfWaitingForDeadline(t *testing.T) {
	agent := testChromiumAgent(t)
	defer stopTestAgent(t, agent)
	server := New(agent, Opts{AuthToken: testAuthToken, RateLimit: RateLimit{ClientRequestsPerMinute: 1, ClientBurst: 2}})
	host := httptest.NewServer(http.HandlerFunc(server.HandleWSForTest))
	defer host.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	client, err := Dial(ctx, "ws"+strings.TrimPrefix(host.URL, "http")+"/", testAuthToken, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := client.Call(ctx, CmdHeartbeat, nil, nil); err == nil || !strings.Contains(err.Error(), string(ErrRateExceeded)) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connection denial was lost: %v", err)
	}
}
