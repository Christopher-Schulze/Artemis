package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestCDPTransportCorrelatesConcurrentResponses(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		first, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		second, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": second.ID, "result": map[string]any{"value": second.Method}}); err != nil {
			return err
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": first.ID, "result": map[string]any{"value": first.Method}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)

	type result struct {
		Value string `json:"value"`
	}
	results := make(chan result, 2)
	errs := make(chan error, 2)
	for _, method := range []string{"First.call", "Second.call"} {
		method := method
		go func() {
			var got result
			err := transport.Call(context.Background(), method, nil, &got)
			results <- got
			errs <- err
		}()
	}
	seen := map[string]bool{}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		seen[(<-results).Value] = true
	}
	if !seen["First.call"] || !seen["Second.call"] {
		t.Fatalf("responses were not correlated: %v", seen)
	}
}

func TestCDPTransportPropagatesProtocolError(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "error": map[string]any{"code": -32601, "message": "unknown method"}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	err := transport.Call(context.Background(), "Missing.method", nil, nil)
	if !IsCDPError(err, CDPErrorProtocol) {
		t.Fatalf("error=%v", err)
	}
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != -32601 {
		t.Fatalf("protocol error=%v", err)
	}
}

func TestCDPTransportDispatchesSessionEvent(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		return writeCDPMessage(ctx, conn, map[string]any{
			"method": "Target.attachedToTarget", "sessionId": "session-1", "params": map[string]any{"waitingForDebugger": false},
		})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	subscription, err := transport.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	select {
	case event := <-subscription.Events:
		if event.Method != "Target.attachedToTarget" || event.SessionID != "session-1" {
			t.Fatalf("event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
}

func TestCDPTransportSubscriptionOverflowIsExplicit(t *testing.T) {
	release := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		<-release
		for i := range 2 {
			if err := writeCDPMessage(ctx, conn, map[string]any{"method": "Test.event", "params": map[string]any{"i": i}}); err != nil {
				return err
			}
		}
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	subscription, err := transport.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-subscription.Errors:
		if !IsCDPError(err, CDPErrorEventOverflow) {
			t.Fatalf("overflow error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow was silent")
	}
}

func TestCDPTransportBoundsPendingCallsAndCancellation(t *testing.T) {
	received := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		if _, err := readCDPCommand(ctx, conn); err != nil {
			return err
		}
		close(received)
		<-ctx.Done()
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{MaxPendingCalls: 1})
	defer closeTestCDPTransport(t, transport)
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- transport.Call(ctx, "First.call", nil, nil) }()
	<-received
	err := transport.Call(context.Background(), "Second.call", nil, nil)
	if !IsCDPError(err, CDPErrorOverloaded) {
		t.Fatalf("pending bound error=%v", err)
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	if pending := transport.PendingCalls(); pending != 0 {
		t.Fatalf("pending calls leaked: %d", pending)
	}
}

func TestCDPTransportCallerWriteCancellationKeepsTransportUsable(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if command.Method != "Survives.call" {
				continue
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}}); err != nil {
				return err
			}
			<-ctx.Done()
			return nil
		}
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transport.Call(cancelled, "Canceled.call", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation error=%v", err)
	}
	if err := transport.Call(context.Background(), "Survives.call", nil, nil); err != nil {
		t.Fatalf("transport did not survive caller cancellation: %v", err)
	}
}

func TestCDPTransportCloseUnblocksPendingCall(t *testing.T) {
	received := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		if _, err := readCDPCommand(ctx, conn); err != nil {
			return err
		}
		close(received)
		<-ctx.Done()
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	callDone := make(chan error, 1)
	go func() { callDone <- transport.Call(context.Background(), "Blocked.call", nil, nil) }()
	<-received
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-callDone:
		if !IsCDPError(err, CDPErrorClosed) {
			t.Fatalf("close error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending call remained blocked")
	}
}

func TestCDPTransportRejectsInvalidConfig(t *testing.T) {
	for _, config := range []CDPTransportConfig{
		{}, {URL: "http://localhost:9222"}, {URL: "ws://localhost:9222", MaxPendingCalls: -1},
	} {
		_, err := DialCDPTransport(context.Background(), config)
		if !IsCDPError(err, CDPErrorInvalidConfig) {
			t.Fatalf("config=%+v error=%v", config, err)
		}
	}
	//lint:ignore SA1012 nil context is the invalid input under test.
	_, err := DialCDPTransport(nil, CDPTransportConfig{URL: "ws://localhost:9222"})
	if !IsCDPError(err, CDPErrorInvalidConfig) {
		t.Fatalf("nil context error=%v", err)
	}
}

func TestCDPTransportRejectsNilCallContextAndBadResult(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": "wrong-shape"})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	//lint:ignore SA1012 nil context is the invalid input under test.
	if err := transport.Call(nil, "Test.call", nil, nil); !IsCDPError(err, CDPErrorInvalidConfig) {
		t.Fatalf("nil call context error=%v", err)
	}
	var result struct {
		Value string `json:"value"`
	}
	if err := transport.Call(context.Background(), "Test.call", nil, &result); !IsCDPError(err, CDPErrorProtocol) {
		t.Fatalf("bad result error=%v", err)
	}
}

func TestCDPTransportMalformedMessageTerminatesTransport(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		return conn.Write(ctx, websocket.MessageText, []byte("not-json"))
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	select {
	case <-transport.Done():
		if !IsCDPError(transport.Err(), CDPErrorProtocol) {
			t.Fatalf("terminal error=%v", transport.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("malformed message did not terminate transport")
	}
	if _, err := transport.Subscribe(1); !IsCDPError(err, CDPErrorProtocol) {
		t.Fatalf("closed subscription error=%v", err)
	}
}

func TestCDPTransportCarriesFlattenedSessionID(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if command.SessionID != "session-9" {
			return errors.New("missing flattened session id")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	if err := transport.CallSession(context.Background(), "session-9", "Runtime.enable", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCDPTransportConcurrentCallsRace(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for range 32 {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}}); err != nil {
				return err
			}
		}
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	var wait sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errs <- transport.Call(context.Background(), "Test.call", nil, nil)
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

type testCDPCommand struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId"`
}

func cdpTestServer(t *testing.T, run func(context.Context, *websocket.Conn) error) string {
	t.Helper()
	serverCtx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			errs <- err
			return
		}
		runErr := run(serverCtx, conn)
		if closeErr := conn.CloseNow(); closeErr != nil && websocket.CloseStatus(closeErr) == -1 {
			runErr = errors.Join(runErr, closeErr)
		}
		errs <- runErr
	}))
	t.Cleanup(func() {
		cancel()
		server.Close()
		select {
		case err := <-errs:
			if err != nil && !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
				t.Errorf("CDP test server: %v", err)
			}
		default:
		}
	})
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func dialTestTransport(t *testing.T, endpoint string, config CDPTransportConfig) *CDPTransport {
	t.Helper()
	config.URL = endpoint
	transport, err := DialCDPTransport(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func readCDPCommand(ctx context.Context, conn *websocket.Conn) (testCDPCommand, error) {
	_, payload, err := conn.Read(ctx)
	if err != nil {
		return testCDPCommand{}, err
	}
	var command testCDPCommand
	if err := json.Unmarshal(payload, &command); err != nil {
		return testCDPCommand{}, err
	}
	return command, nil
}

func writeCDPMessage(ctx context.Context, conn *websocket.Conn, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, payload)
}

func closeTestCDPTransport(t *testing.T, transport *CDPTransport) {
	t.Helper()
	if err := transport.Close(); err != nil {
		t.Errorf("close CDP transport: %v", err)
	}
}
