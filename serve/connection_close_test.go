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
)

func TestServerCloseAcceptsAlreadyRetiredTrackedWebsocket(t *testing.T) {
	owner := New(nil, Opts{AuthToken: testAuthToken})
	retired, release := make(chan struct{}), make(chan struct{})
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(w, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			if err := connection.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}()
		if !owner.trackConnection(connection, owner.authGeneration.Load(), true) {
			t.Error("live owner rejected the actual connection")
			return
		}
		defer owner.trackConnection(connection, owner.authGeneration.Load(), false)
		if err := connection.CloseNow(); err != nil {
			t.Error(err)
			return
		}
		close(retired)
		// The server's snapshot can retain a connection while its handler is
		// still finishing after the actual WebSocket goroutines have joined.
		<-release
	}))
	defer host.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(host.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	}()
	select {
	case <-retired:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("already joined connection became a shutdown failure: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("repeat owner close failed: %v", err)
	}
}
