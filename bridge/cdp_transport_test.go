package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
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
		if err := writeCDPMessage(ctx, conn, map[string]any{
			"method": "Target.attachedToTarget", "sessionId": "session-1", "params": map[string]any{"waitingForDebugger": false},
		}); err != nil {
			return err
		}
		// Keep the connection open: returning right after the write races
		// with the client's Subscribe on fast/loaded runners.
		<-ctx.Done()
		return ctx.Err()
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
	var invalidContext context.Context
	_, err := DialCDPTransport(invalidContext, CDPTransportConfig{URL: "ws://localhost:9222"})
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
	var invalidContext context.Context
	if err := transport.Call(invalidContext, "Test.call", nil, nil); !IsCDPError(err, CDPErrorInvalidConfig) {
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
			// The client closing its transport at the end of a test is a
			// normal hang-up, observed as EOF or a closed connection when it
			// races the cleanup cancellation; protocol errors still fail.
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && websocket.CloseStatus(err) == -1 {
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

func TestPageCloseRequiresSuccessfulReceiptAndCanRetry(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for attempt, success := range []bool{false, true} {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if command.Method != "Target.closeTarget" {
				return errors.New("page close did not use Target.closeTarget")
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"success": success}}); err != nil {
				return err
			}
			if attempt == 0 {
				list, err := readCDPCommand(ctx, conn)
				if err != nil {
					return err
				}
				if list.Method != "Target.getTargets" {
					return errors.New("failed close receipt was not reconciled with Target.getTargets")
				}
				if err := writeCDPMessage(ctx, conn, map[string]any{"id": list.ID, "result": map[string]any{"targetInfos": []any{map[string]any{"targetId": "target-1"}}}}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page

	if err := page.CloseContext(context.Background()); err == nil {
		t.Fatal("false close receipt reported success")
	}
	if page.State() != TargetStateAttached || len(page.ContextPages()) != 1 || browser.pageSlots != 1 {
		t.Fatalf("false close receipt mutated local ownership: state=%s pages=%d slots=%d", page.State(), len(page.ContextPages()), browser.pageSlots)
	}
	if err := page.CloseContext(context.Background()); err != nil {
		t.Fatalf("retry close: %v", err)
	}
	if page.State() != TargetStateClosed || len(page.ContextPages()) != 0 || browser.pageSlots != 0 {
		t.Fatalf("confirmed close was not committed locally: state=%s pages=%d slots=%d", page.State(), len(page.ContextPages()), browser.pageSlots)
	}
}

func TestPageCloseReconciliationRespectsCallerCancellation(t *testing.T) {
	listRequested := make(chan struct{})
	releaseList := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		closeCommand, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if closeCommand.Method != "Target.closeTarget" {
			return errors.New("page close did not use Target.closeTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": closeCommand.ID, "result": map[string]any{"success": false}}); err != nil {
			return err
		}
		list, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if list.Method != "Target.getTargets" {
			return errors.New("failed close receipt was not reconciled with Target.getTargets")
		}
		close(listRequested)
		select {
		case <-releaseList:
		case <-ctx.Done():
			return ctx.Err()
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": list.ID, "result": map[string]any{"targetInfos": []any{map[string]any{"targetId": "target-1"}}}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	defer close(releaseList)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- page.CloseContext(ctx) }()
	select {
	case <-listRequested:
	case <-time.After(time.Second):
		t.Fatal("page close did not begin target reconciliation")
	}
	cancel()
	select {
	case err := <-closed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("close error=%v, want caller cancellation during reconciliation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("target reconciliation ignored caller cancellation")
	}
	if page.State() != TargetStateAttached || len(page.ContextPages()) != 1 || browser.pageSlots != 1 {
		t.Fatalf("canceled reconciliation changed ownership: state=%s pages=%d slots=%d", page.State(), len(page.ContextPages()), browser.pageSlots)
	}
}

type lifecycleWaitObservedContext struct {
	context.Context
	doneRead chan struct{}
	once     sync.Once
}

func (c *lifecycleWaitObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.doneRead) })
	return c.Context.Done()
}

func TestPageCloseLifecycleLockWaitHonorsCallerCancellation(t *testing.T) {
	page := &Page{}
	page.lifecycleMu.Lock()
	defer page.lifecycleMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &lifecycleWaitObservedContext{Context: ctx, doneRead: make(chan struct{})}
	closed := make(chan error, 1)
	go func() {
		closed <- page.CloseContext(observed)
	}()
	select {
	case <-observed.doneRead:
	case <-time.After(time.Second):
		t.Fatal("close did not enter context-aware lifecycle lock wait")
	}
	cancel()
	select {
	case err := <-closed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("close error=%v, want cancellation while waiting for lifecycle lock", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close remained blocked on lifecycle lock after caller cancellation")
	}
}

func TestPageCloseReconcilesDestroyedTargetWithoutEvent(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		closeCommand, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if closeCommand.Method != "Target.closeTarget" {
			return errors.New("page close did not use Target.closeTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": closeCommand.ID, "result": map[string]any{"success": false}}); err != nil {
			return err
		}
		list, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if list.Method != "Target.getTargets" {
			return errors.New("failed close receipt was not reconciled with Target.getTargets")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": list.ID, "result": map[string]any{"targetInfos": []any{}}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page

	if err := page.CloseContext(context.Background()); err != nil {
		t.Fatalf("reconciled target close: %v", err)
	}
	if page.State() != TargetStateClosed || len(page.ContextPages()) != 0 || browser.pageSlots != 0 || len(browser.pages) != 0 {
		t.Fatalf("reconciled target close retained ownership: state=%s pages=%d slots=%d browserPages=%d", page.State(), len(page.ContextPages()), browser.pageSlots, len(browser.pages))
	}
}

func TestBrowserContextCloseRetainsPagesWhenDisposeFailsAndRetries(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for attempt := 0; attempt < 2; attempt++ {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if command.Method != "Target.disposeBrowserContext" {
				return errors.New("context close did not use Target.disposeBrowserContext")
			}
			if attempt == 0 {
				if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "error": map[string]any{"code": -32000, "message": "context disposal refused"}}); err != nil {
					return err
				}
				reconcile, err := readCDPCommand(ctx, conn)
				if err != nil {
					return err
				}
				if reconcile.Method != "Target.getBrowserContexts" {
					return errors.New("failed context disposal was not reconciled with Target.getBrowserContexts")
				}
				if err := writeCDPMessage(ctx, conn, map[string]any{"id": reconcile.ID, "result": map[string]any{"browserContextIds": []string{"context-1"}}}); err != nil {
					return err
				}
				continue
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}}); err != nil {
				return err
			}
		}
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	page.lifecycle = tabs.NewTabLifecycle(tabs.DefaultProcessSpec(tabs.ProcessSpecStream, "P3"), tabs.OwnerRefTurn, nil)
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	browser.contexts[owner.id] = owner

	if err := owner.Close(); err == nil {
		t.Fatal("failed context disposal reported success")
	}
	select {
	case <-page.DownSignal():
	default:
		t.Fatal("failed owner disposal did not signal lifecycle loss")
	}
	// Disposal failure revokes execution without discarding the retry identity.
	if page.State() != TargetStateDetached || len(page.ContextPages()) != 1 || browser.pageSlots != 1 || len(browser.contexts) != 1 {
		t.Fatalf("failed context disposal released local ownership: state=%s pages=%d slots=%d contexts=%d", page.State(), len(page.ContextPages()), browser.pageSlots, len(browser.contexts))
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("context close retry: %v", err)
	}
	if page.State() != TargetStateClosed || len(page.ContextPages()) != 0 || browser.pageSlots != 0 || len(browser.contexts) != 0 {
		t.Fatalf("confirmed context close retained ownership: state=%s pages=%d slots=%d contexts=%d", page.State(), len(page.ContextPages()), browser.pageSlots, len(browser.contexts))
	}
}

func TestBrowserContextCloseReconcilesMissingRemoteContext(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		dispose, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if dispose.Method != "Target.disposeBrowserContext" {
			return errors.New("context close did not use Target.disposeBrowserContext")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": dispose.ID, "error": map[string]any{"code": -32000, "message": "context already disposed"}}); err != nil {
			return err
		}
		reconcile, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if reconcile.Method != "Target.getBrowserContexts" {
			return errors.New("context disposal error was not reconciled")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": reconcile.ID, "result": map[string]any{"browserContextIds": []string{}}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	browser.contexts[owner.id] = owner

	if err := owner.Close(); err != nil {
		t.Fatalf("reconciled context close: %v", err)
	}
	if page.State() != TargetStateClosed || len(page.ContextPages()) != 0 || browser.pageSlots != 0 || len(browser.contexts) != 0 {
		t.Fatalf("reconciled context disposal retained ownership: state=%s pages=%d slots=%d contexts=%d", page.State(), len(page.ContextPages()), browser.pageSlots, len(browser.contexts))
	}
}

func TestDefaultBrowserContextCloseUsesConfirmedTargetClose(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for attempt, success := range []bool{false, true} {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if command.Method != "Target.closeTarget" {
				return errors.New("default context close did not use Target.closeTarget")
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"success": success}}); err != nil {
				return err
			}
			if attempt == 0 {
				list, err := readCDPCommand(ctx, conn)
				if err != nil {
					return err
				}
				if list.Method != "Target.getTargets" {
					return errors.New("failed default-target close was not reconciled with Target.getTargets")
				}
				if err := writeCDPMessage(ctx, conn, map[string]any{"id": list.ID, "result": map[string]any{"targetInfos": []any{map[string]any{"targetId": "target-1"}}}}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	browser.contexts[owner.id] = owner

	if err := owner.Close(); err == nil {
		t.Fatal("false default-target close receipt reported success")
	}
	if page.State() != TargetStateAttached || len(page.ContextPages()) != 1 || browser.pageSlots != 1 {
		t.Fatalf("false default-target receipt released ownership: state=%s pages=%d slots=%d", page.State(), len(page.ContextPages()), browser.pageSlots)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("default context close retry: %v", err)
	}
	if page.State() != TargetStateClosed || len(page.ContextPages()) != 0 || browser.pageSlots != 0 || len(browser.contexts) != 0 {
		t.Fatalf("confirmed default-target close retained ownership: state=%s pages=%d slots=%d contexts=%d", page.State(), len(page.ContextPages()), browser.pageSlots, len(browser.contexts))
	}
}

func TestTargetDestroyedEventReleasesPageOwnership(t *testing.T) {
	browser := &ChromiumBrowser{pageSlots: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	browser.contexts[owner.id] = owner

	browser.applyTargetEvent(CDPEvent{Method: "Target.targetDestroyed", Params: json.RawMessage(`{"targetId":"target-1"}`)})
	if page.State() != TargetStateClosed || len(page.ContextPages()) != 0 || len(browser.pages) != 0 || len(browser.sessionMap) != 0 || browser.pageSlots != 0 {
		t.Fatalf("target-destroyed event retained local ownership: state=%s contextPages=%d browserPages=%d sessions=%d slots=%d", page.State(), len(page.ContextPages()), len(browser.pages), len(browser.sessionMap), browser.pageSlots)
	}
}

func TestNewPageReportsUnknownCreateOutcomeWhenReceiptCannotBeDecoded(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if command.Method != "Target.createTarget" {
			return errors.New("page creation did not use Target.createTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"targetId": 42}}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, maxPages: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}

	_, err := owner.NewPage(context.Background(), "about:blank")
	var targetErr *TargetError
	if !errors.As(err, &targetErr) || !targetErr.OutcomeUnknown || targetErr.TargetID != "" {
		t.Fatalf("create error=%#v, want typed unknown outcome without a fabricated ID", err)
	}
	if strings.Contains(err.Error(), "detached") || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("unknown create outcome claimed a target state: %v", err)
	}
	if browser.pageSlots != 0 || len(owner.pages) != 0 {
		t.Fatalf("unknown create retained false local ownership: slots=%d pages=%d", browser.pageSlots, len(owner.pages))
	}
}

func TestNewPageReportsUnknownCreateOutcomeWhenReceiptOmitsID(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if command.Method != "Target.createTarget" {
			return errors.New("page creation did not use Target.createTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, maxPages: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}

	_, err := owner.NewPage(context.Background(), "about:blank")
	var targetErr *TargetError
	if !errors.As(err, &targetErr) || !targetErr.OutcomeUnknown || targetErr.TargetID != "" {
		t.Fatalf("create error=%#v, want unknown effect with missing target ID", err)
	}
	if strings.Contains(err.Error(), "detached") || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("unknown create outcome claimed a target state: %v", err)
	}
	if browser.pageSlots != 0 || len(owner.pages) != 0 {
		t.Fatalf("unknown create retained false local ownership: slots=%d pages=%d", browser.pageSlots, len(owner.pages))
	}
}

func TestNewPageRejectsTargetDestroyedBeforeRegistration(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		create, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if create.Method != "Target.createTarget" {
			return errors.New("page creation did not use Target.createTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": create.ID, "result": map[string]any{"targetId": "destroyed-before-register"}}); err != nil {
			return err
		}
		attach, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if attach.Method != "Target.attachToTarget" {
			return errors.New("page creation did not use Target.attachToTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"method": "Target.targetDestroyed", "params": map[string]any{"targetId": "destroyed-before-register"}}); err != nil {
			return err
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": attach.ID, "result": map[string]any{"sessionId": "session-1"}}); err != nil {
			return err
		}
		list, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if list.Method != "Target.getTargets" {
			return errors.New("registered target was not checked against Target.getTargets")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": list.ID, "result": map[string]any{"targetInfos": []any{}}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	monitor, err := transport.Subscribe(16)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	browser := &ChromiumBrowser{transport: transport, monitor: monitor, maxPages: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	go browser.monitorTargets(monitor)
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}

	_, err = owner.NewPage(context.Background(), "about:blank")
	var targetErr *TargetError
	if !errors.As(err, &targetErr) || !targetErr.Compensated || targetErr.TargetID != "destroyed-before-register" {
		t.Fatalf("destroyed target error=%#v", err)
	}
	if browser.pageSlots != 0 || len(owner.pages) != 0 || len(browser.pages) != 0 || len(browser.sessionMap) != 0 {
		t.Fatalf("destroyed target retained ownership: slots=%d contextPages=%d browserPages=%d sessions=%d", browser.pageSlots, len(owner.pages), len(browser.pages), len(browser.sessionMap))
	}
}

func TestNewPageRejectsTargetDestroyedDuringInitialization(t *testing.T) {
	var browser *ChromiumBrowser
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			switch command.Method {
			case "Target.createTarget":
				err = writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"targetId": "destroyed-during-init"}})
			case "Target.attachToTarget":
				err = writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"sessionId": "session-1"}})
			case "Target.getTargets":
				err = writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"targetInfos": []any{
					map[string]any{"targetId": "destroyed-during-init", "type": "page", "browserContextId": "context-1"},
				}}})
			case "Page.enable", "Network.enable", "Fetch.enable":
				err = writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}})
			case "Target.setAutoAttach":
				browser.applyTargetEvent(CDPEvent{Method: "Target.targetDestroyed", Params: json.RawMessage(`{"targetId":"destroyed-during-init"}`)})
				err = writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}})
			default:
				return fmt.Errorf("unexpected page initialization command %q", command.Method)
			}
			if err != nil {
				return err
			}
		}
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser = &ChromiumBrowser{transport: transport, maxPages: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}

	_, err := owner.NewPage(context.Background(), "about:blank")
	var targetErr *TargetError
	if !errors.As(err, &targetErr) || !targetErr.Compensated || targetErr.TargetID != "destroyed-during-init" {
		t.Fatalf("target destroyed during initialization error=%#v, want identified compensated failure", err)
	}
	if browser.pageSlots != 0 || len(owner.pages) != 0 || len(browser.pages) != 0 || len(browser.sessionMap) != 0 {
		t.Fatalf("destroyed-during-init target retained ownership: slots=%d contextPages=%d browserPages=%d sessions=%d", browser.pageSlots, len(owner.pages), len(browser.pages), len(browser.sessionMap))
	}
}

func TestNewPageCancellationDuringLivenessCheckCompensatesTarget(t *testing.T) {
	livenessChecked := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		create, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if create.Method != "Target.createTarget" {
			return errors.New("page creation did not use Target.createTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": create.ID, "result": map[string]any{"targetId": "cancelled-liveness-check"}}); err != nil {
			return err
		}
		attach, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if attach.Method != "Target.attachToTarget" {
			return errors.New("page creation did not use Target.attachToTarget")
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": attach.ID, "result": map[string]any{"sessionId": "session-1"}}); err != nil {
			return err
		}
		list, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if list.Method != "Target.getTargets" {
			return errors.New("page creation did not verify target liveness")
		}
		close(livenessChecked)
		closeCommand, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if closeCommand.Method != "Target.closeTarget" {
			return errors.New("canceled liveness check did not compensate the created target")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": closeCommand.ID, "result": map[string]any{"success": true}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, maxPages: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	created := make(chan error, 1)
	go func() {
		_, err := owner.NewPage(requestCtx, "about:blank")
		created <- err
	}()
	select {
	case <-livenessChecked:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for target liveness check")
	}
	var createErr error
	select {
	case createErr = <-created:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for canceled page creation")
	}
	var targetErr *TargetError
	if !errors.As(createErr, &targetErr) || !targetErr.Compensated || targetErr.TargetID != "cancelled-liveness-check" {
		t.Fatalf("canceled liveness error=%#v, want identified compensated target", createErr)
	}
	if browser.pageSlots != 0 || len(owner.pages) != 0 || len(browser.pages) != 0 || len(browser.sessionMap) != 0 {
		t.Fatalf("canceled liveness check retained ownership: slots=%d contextPages=%d browserPages=%d sessions=%d", browser.pageSlots, len(owner.pages), len(browser.pages), len(browser.sessionMap))
	}
}

func TestContextPagesContextRetiresTargetMissingFromAuthoritativeSnapshot(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if command.Method != "Target.getTargets" {
			return errors.New("context page listing did not use Target.getTargets")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{"targetInfos": []any{}}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "lost-without-destroyed-event", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	root := &Page{targetID: "root", owner: owner}

	pages, err := root.ContextPagesContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 0 || page.State() != TargetStateClosed || len(owner.pages) != 0 || len(browser.pages) != 0 || len(browser.sessionMap) != 0 || browser.pageSlots != 0 {
		t.Fatalf("absent target ownership was not retired: returned=%d state=%s contextPages=%d browserPages=%d sessions=%d slots=%d", len(pages), page.State(), len(owner.pages), len(browser.pages), len(browser.sessionMap), browser.pageSlots)
	}
}

func TestContextPagesContextRetainsOwnershipWhenTargetSnapshotIsMalformed(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if command.Method != "Target.getTargets" {
			return errors.New("context page listing did not use Target.getTargets")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
	page := &Page{targetID: "target-1", sessionID: "session-1", owner: owner, state: TargetStateAttached,
		frameSessions: make(map[string]string), childSessions: make(map[string]string)}
	owner.pages[page.targetID] = page
	browser.pages[page.targetID] = page
	browser.sessionMap[page.sessionID] = page
	root := &Page{targetID: "root", owner: owner}

	if _, err := root.ContextPagesContext(context.Background()); err == nil {
		t.Fatal("malformed target snapshot reported a complete context page list")
	}
	if page.State() != TargetStateAttached || len(owner.pages) != 1 || len(browser.pages) != 1 || len(browser.sessionMap) != 1 || browser.pageSlots != 1 {
		t.Fatalf("malformed snapshot changed ownership: state=%s contextPages=%d browserPages=%d sessions=%d slots=%d", page.State(), len(owner.pages), len(browser.pages), len(browser.sessionMap), browser.pageSlots)
	}
}

func TestNewPageCancellationPreservesAndCompensatesCreatedTarget(t *testing.T) {
	createReceived := make(chan struct{})
	returnCreate := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		create, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if create.Method != "Target.createTarget" {
			return errors.New("page creation did not use Target.createTarget")
		}
		close(createReceived)
		select {
		case <-returnCreate:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := writeCDPMessage(ctx, conn, map[string]any{"id": create.ID, "result": map[string]any{"targetId": "created-before-cancel"}}); err != nil {
			return err
		}
		closeCommand, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if closeCommand.Method != "Target.closeTarget" {
			return errors.New("canceled page creation did not compensate the target")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": closeCommand.ID, "result": map[string]any{"success": true}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, maxPages: 1, pages: make(map[string]*Page), contexts: make(map[string]*BrowserContext), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
	requestCtx, cancel := context.WithCancel(context.Background())
	created := make(chan error, 1)
	go func() {
		_, err := owner.NewPage(requestCtx, "https://example.com")
		created <- err
	}()
	select {
	case <-createReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Target.createTarget")
	}
	cancel()
	close(returnCreate)
	err := <-created
	var targetErr *TargetError
	if !errors.As(err, &targetErr) || targetErr.TargetID != "created-before-cancel" || !targetErr.Compensated {
		t.Fatalf("canceled creation error=%v, want identified compensated target", err)
	}
	if browser.pageSlots != 0 || len(owner.pages) != 0 {
		t.Fatalf("canceled creation retained local ownership: slots=%d pages=%d", browser.pageSlots, len(owner.pages))
	}
}

func TestPageActivationRecordsTimestampAfterProtocolSuccess(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		if command.Method != "Target.activateTarget" {
			return errors.New("page activation did not use Target.activateTarget")
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}})
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	createdAt := time.Unix(100, 0)
	page := &Page{targetID: "target-1", owner: owner, state: TargetStateAttached, createdAt: createdAt}
	if _, lastActive := page.LifecycleTimes(); !lastActive.IsZero() {
		t.Fatalf("initial activity timestamp=%v, want zero", lastActive)
	}
	if err := page.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	created, lastActive := page.LifecycleTimes()
	if !created.Equal(createdAt) || lastActive.IsZero() {
		t.Fatalf("lifecycle times=(%v,%v)", created, lastActive)
	}
}

func TestCloseTargetInContextVerifiesOrphanOwnership(t *testing.T) {
	t.Run("same context closes target", func(t *testing.T) {
		endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
			lookup, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if lookup.Method != "Target.getTargetInfo" {
				return errors.New("orphan close did not verify target context")
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": lookup.ID, "result": map[string]any{"targetInfo": map[string]any{"targetId": "orphan-1", "browserContextId": "context-1"}}}); err != nil {
				return err
			}
			closeCommand, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if closeCommand.Method != "Target.closeTarget" {
				return errors.New("owned orphan was not closed")
			}
			return writeCDPMessage(ctx, conn, map[string]any{"id": closeCommand.ID, "result": map[string]any{"success": true}})
		})
		transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
		defer closeTestCDPTransport(t, transport)
		browser := &ChromiumBrowser{transport: transport}
		owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
		root := &Page{targetID: "root-1", owner: owner, state: TargetStateAttached}
		if err := root.CloseTargetInContext(context.Background(), "orphan-1"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("foreign context is rejected", func(t *testing.T) {
		endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
			lookup, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			if lookup.Method != "Target.getTargetInfo" {
				return errors.New("orphan close did not verify target context")
			}
			return writeCDPMessage(ctx, conn, map[string]any{"id": lookup.ID, "result": map[string]any{"targetInfo": map[string]any{"targetId": "orphan-2", "browserContextId": "foreign-context"}}})
		})
		transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
		defer closeTestCDPTransport(t, transport)
		browser := &ChromiumBrowser{transport: transport}
		owner := &BrowserContext{id: "context-1", browser: browser, pages: make(map[string]*Page)}
		root := &Page{targetID: "root-1", owner: owner, state: TargetStateAttached}
		err := root.CloseTargetInContext(context.Background(), "orphan-2")
		var targetErr *TargetError
		if !errors.As(err, &targetErr) || targetErr.Message != "target is not owned by this browser context" {
			t.Fatalf("foreign target error=%v", err)
		}
	})
}
