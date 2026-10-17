package bridge

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

type shutdownPolicyLookup struct {
	entered, cancelled, release chan struct{}
}

func (r *shutdownPolicyLookup) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	close(r.entered)
	<-ctx.Done()
	close(r.cancelled)
	<-r.release
	return nil, ctx.Err()
}

func TestPolicyProxyCloseCancelsAndJoinsActualHTTPPolicyWork(t *testing.T) {
	resolver := &shutdownPolicyLookup{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	policy, err := network.NewPolicy(network.PolicyConfig{}, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "proxy", proxy.Close)
	defer proxy.cancel()
	defer close(resolver.release)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://shutdown.invalid/blocked", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := proxyHTTPClient(t, proxy.URL())
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := client.Do(request)
		if err == nil && response != nil {
			if err := response.Body.Close(); err != nil {
				t.Errorf("response close: %v", err)
			}
		}
	}()
	select {
	case <-resolver.entered:
	case <-time.After(time.Second):
		t.Fatal("actual HTTP proxy request did not reach its policy resolver")
	}
	finished := make(chan error, 1)
	go func() { finished <- proxy.Close() }()
	select {
	case <-resolver.cancelled:
	case <-time.After(time.Second):
		t.Fatal("proxy close did not cancel the active resolver")
	}
	select {
	case err := <-finished:
		t.Fatalf("proxy close bypassed unfinished handler: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	resolver.release <- struct{}{}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy close did not join the released handler")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("actual proxy client remained pending after close")
	}
}

func TestClosedPolicyProxyCannotTrackOrAdmitNewWork(t *testing.T) {
	policy, err := network.NewPolicy(network.PolicyConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := proxy.Close(); err != nil {
		t.Fatal(err)
	}
	client, upstream := net.Pipe()
	defer closeBridgeTestResource(t, "client", client.Close)
	defer closeBridgeTestResource(t, "upstream", upstream.Close)
	if proxy.track(client, upstream) {
		t.Fatal("closed proxy retained late tunnel sockets")
	}
	proxy.mu.Lock()
	count := len(proxy.conns)
	proxy.mu.Unlock()
	if count != 0 {
		t.Fatalf("closed proxy has %d retained sockets", count)
	}
}

type delayedTunnelRead struct {
	net.Conn
	exited, release chan struct{}
	once            sync.Once
}

func (c *delayedTunnelRead) Read(buffer []byte) (int, error) {
	count, err := c.Conn.Read(buffer)
	if err != nil {
		c.once.Do(func() { close(c.exited) })
		<-c.release
	}
	return count, err
}

type shutdownTunnelWriter struct {
	*httptest.ResponseRecorder
	connection net.Conn
}

func (w *shutdownTunnelWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.connection, bufio.NewReadWriter(bufio.NewReader(w.connection), bufio.NewWriter(w.connection)), nil
}

func TestPolicyProxyCloseJoinsHijackedTunnelAfterActualSocketClosure(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer backend.Close()
	proxy, err := newChromiumPolicyProxy(context.Background(), newProxyTestPolicy(t, backend.URL, true))
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "proxy", proxy.Close)
	client, peer := net.Pipe()
	defer closeBridgeTestResource(t, "client", client.Close)
	defer closeBridgeTestResource(t, "peer", peer.Close)
	connection := &delayedTunnelRead{Conn: client, exited: make(chan struct{}), release: make(chan struct{})}
	defer close(connection.release)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		proxy.ServeHTTP(&shutdownTunnelWriter{ResponseRecorder: httptest.NewRecorder(), connection: connection}, request)
	}()
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(peer)
	if status, err := reader.ReadString('\n'); err != nil || !strings.Contains(status, " 200 ") {
		t.Fatalf("actual tunnel was not established: status=%q error=%v", status, err)
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "\r\n" {
		t.Fatalf("actual tunnel header termination: line=%q error=%v", line, err)
	}
	finished := make(chan error, 1)
	go func() { finished <- proxy.Close() }()
	select {
	case <-connection.exited:
	case <-time.After(time.Second):
		t.Fatal("proxy shutdown did not close the actual tunnel socket")
	}
	select {
	case err := <-finished:
		t.Fatalf("proxy shutdown returned before hijacked handler completion: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	connection.release <- struct{}{}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy shutdown did not join the released tunnel")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("actual tunnel handler survived proxy shutdown")
	}
}

type failedCloseConnection struct {
	net.Conn
	failed  atomic.Bool
	failure error
}

func (c *failedCloseConnection) Close() error {
	if c.failed.CompareAndSwap(false, true) {
		return c.failure
	}
	return c.Conn.Close()
}

func TestProxyConnectionCleanupFailureRetainsActualSocketAndError(t *testing.T) {
	policy, err := network.NewPolicy(network.PolicyConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	client, peer := net.Pipe()
	defer closeBridgeTestResource(t, "socket", client.Close)
	defer closeBridgeTestResource(t, "peer", peer.Close)
	failure := errors.New("controlled socket-close failure")
	defer func() {
		if err := proxy.Close(); err != nil && !errors.Is(err, failure) {
			t.Errorf("close socket-failure fixture: %v", err)
		}
	}()
	connection := &failedCloseConnection{Conn: client, failure: failure}
	if !proxy.track(connection) {
		t.Fatal("live proxy rejected the owned socket")
	}
	proxy.untrackAndClose(connection)
	proxy.mu.Lock()
	_, retained := proxy.conns[connection]
	proxy.mu.Unlock()
	if !retained {
		t.Fatal("failed socket cleanup lost the actual owner")
	}
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := proxy.Close(); !errors.Is(err, failure) {
		t.Fatalf("actual cleanup failure was erased: %v", err)
	}
	var buffer [1]byte
	if _, err := peer.Read(buffer[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("owned shutdown failed to close the retained real socket: %v", err)
	}
	proxy.mu.Lock()
	count := len(proxy.conns)
	proxy.mu.Unlock()
	if count != 0 {
		t.Fatalf("successful socket close retained %d handles", count)
	}
}
