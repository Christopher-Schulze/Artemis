package serve

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func controlPeer(t *testing.T, script func(context.Context, *websocket.Conn)) (*Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(ClientIDHeader, "peer-test")
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			if err := conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}()
		var req Request
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			t.Error(err)
			return
		}
		if err := wsjson.Write(ctx, conn, okResp(req.ID, VersionResponse{Protocol: ProtocolVersion})); err != nil {
			t.Error(err)
			return
		}
		script(ctx, conn)
	}))
	t.Cleanup(host.Close)
	t.Cleanup(cancel)
	client, err := Dial(ctx, "ws"+strings.TrimPrefix(host.URL, "http"), testAuthToken, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client, ctx
}

func TestControlClientRoutesReversedResponsesAndDiscardsCancelledOutcome(t *testing.T) {
	firstRead := make(chan struct{})
	client, ctx := controlPeer(t, func(ctx context.Context, peer *websocket.Conn) {
		var first, second, third Request
		if err := wsjson.Read(ctx, peer, &first); err != nil {
			t.Error(err)
			return
		}
		close(firstRead)
		if err := wsjson.Read(ctx, peer, &second); err != nil {
			t.Error(err)
			return
		}
		if err := wsjson.Read(ctx, peer, &third); err != nil {
			t.Error(err)
			return
		}
		if first.ID == second.ID || first.ID == third.ID || second.ID == third.ID {
			t.Error("request IDs are not unique")
			return
		}
		for _, message := range []any{
			okResp(first.ID, "cancelled"), Event{Event: "progress"},
			okResp(third.ID, third.Cmd), okResp(second.ID, second.Cmd),
		} {
			if err := wsjson.Write(ctx, peer, message); err != nil {
				t.Error(err)
				return
			}
		}
	})
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	abandoned := make(chan error, 1)
	go func() { abandoned <- client.Call(cancelCtx, CmdHeartbeat, nil, nil) }()
	select {
	case <-firstRead:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	if err := <-abandoned; !errors.Is(err, context.Canceled) || !errors.Is(err, ErrControlOutcomeUnknown) {
		t.Fatalf("cancelled result: %v", err)
	}
	results := make(chan error, 2)
	for _, cmd := range []Command{CmdSessionList, CmdHeartbeat} {
		go func() {
			var result string
			if err := client.Call(ctx, cmd, nil, &result); err != nil || result != string(cmd) {
				results <- errors.Join(errors.New("outcome does not match "+string(cmd)+": "+result), err)
				return
			}
			results <- nil
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}

func TestControlClientSettlesPeersOnMalformedOrUnownedResponse(t *testing.T) {
	for _, kind := range []string{"broken-json", "missing-ok", "unknown-id", "duplicate-id", "remote-close", "client-close"} {
		t.Run(kind, func(t *testing.T) {
			read := make(chan struct{})
			client, ctx := controlPeer(t, func(ctx context.Context, peer *websocket.Conn) {
				var first, second Request
				if err := errors.Join(wsjson.Read(ctx, peer, &first), wsjson.Read(ctx, peer, &second)); err != nil {
					t.Error(err)
					return
				}
				close(read)
				switch kind {
				case "broken-json":
					if err := peer.Write(ctx, websocket.MessageText, []byte("{")); err != nil {
						t.Error(err)
					}
				case "missing-ok":
					if err := wsjson.Write(ctx, peer, Request{ID: first.ID}); err != nil {
						t.Error(err)
					}
				case "unknown-id":
					if err := wsjson.Write(ctx, peer, okResp("absent", "wrong outcome")); err != nil {
						t.Error(err)
					}
				case "duplicate-id":
					for range 2 {
						if err := wsjson.Write(ctx, peer, okResp(first.ID, "first outcome")); err != nil {
							t.Error(err)
							return
						}
					}
				case "client-close":
					if _, _, err := peer.Read(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("peer remained open: %v", err)
					}
				}
			})
			outcomes := make(chan error, 2)
			for range 2 {
				go func() { outcomes <- client.Call(ctx, CmdHeartbeat, nil, nil) }()
			}
			want := ErrControlProtocol
			if kind == "remote-close" {
				want = ErrControlTransport
			}
			if kind == "client-close" {
				select {
				case <-read:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
				want = ErrControlClosed
			}
			failures := 0
			for range 2 {
				err := <-outcomes
				if errors.Is(err, want) {
					failures++
				} else if err != nil || kind != "duplicate-id" {
					t.Fatalf("outcome: %v, want %v", err, want)
				}
			}
			if failures == 0 || (kind != "duplicate-id" && failures != 2) {
				t.Fatalf("received %d typed failures", failures)
			}
			if err := client.Call(ctx, CmdHeartbeat, nil, nil); !errors.Is(err, want) {
				t.Fatalf("terminal error changed: %v, want %v", err, want)
			}
		})
	}
}
