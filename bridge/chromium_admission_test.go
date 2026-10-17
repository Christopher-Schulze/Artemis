package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// silentContextAdmissionServer accepts Target.createBrowserContext and never
// answers, reproducing a browser whose UI thread stopped serving CDP.
func silentContextAdmissionServer(t *testing.T) string {
	t.Helper()
	return cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return nil
			}
			if command.Method != "Target.createBrowserContext" {
				return errors.New("unexpected command " + command.Method)
			}
		}
	})
}

func newAdmissionTestBrowser(t *testing.T, endpoint string, timeout time.Duration) *ChromiumBrowser {
	t.Helper()
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	t.Cleanup(func() { closeTestCDPTransport(t, transport) })
	return &ChromiumBrowser{transport: transport, contexts: make(map[string]*BrowserContext), pages: make(map[string]*Page),
		sessionMap: make(map[string]*Page), admissionTimeout: timeout}
}

func pendingCDPCalls(transport *CDPTransport) int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return len(transport.pending)
}

func TestNewContextFailsClosedWhenBrowserStopsAnswering(t *testing.T) {
	browser := newAdmissionTestBrowser(t, silentContextAdmissionServer(t), 100*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, err := browser.NewContext(context.Background())
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("context admission without a caller deadline hung on an unanswered createBrowserContext")
	}
	if !IsCDPError(err, CDPErrorUnresponsive) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unanswered admission was not classified as an unresponsive browser: %v", err)
	}
	if browser.Healthy() || !IsCDPError(browser.Err(), CDPErrorUnresponsive) || pendingCDPCalls(browser.transport) != 0 {
		t.Fatalf("wedged browser stayed admissible or retained correlation: healthy=%t err=%v pending=%d", browser.Healthy(), browser.Err(), pendingCDPCalls(browser.transport))
	}
	start := time.Now()
	if _, err := browser.NewContext(context.Background()); err == nil || time.Since(start) > time.Second {
		t.Fatalf("follow-up admission on a wedged browser did not fail fast: %v after %s", err, time.Since(start))
	}
}

func TestNewContextCallerDeadlineKeepsBrowserHealthy(t *testing.T) {
	browser := newAdmissionTestBrowser(t, silentContextAdmissionServer(t), 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := browser.NewContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || IsCDPError(err, CDPErrorUnresponsive) {
		t.Fatalf("caller deadline was misclassified: %v", err)
	}
	if !browser.Healthy() || browser.Err() != nil || pendingCDPCalls(browser.transport) != 0 {
		t.Fatalf("caller deadline poisoned the browser or leaked correlation: healthy=%t err=%v pending=%d", browser.Healthy(), browser.Err(), pendingCDPCalls(browser.transport))
	}
}
