package bridge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

// announcingBrowserScript writes DevToolsActivePort for port and then runs
// tail, the shell body after the announcement.
func announcingBrowserScript(t *testing.T, port int, tail string) string {
	t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '%d\n/devtools/browser/handshake\n' > "$profile/DevToolsActivePort"
%s
`, port, tail)
	path := filepath.Join(t.TempDir(), "browser-fixture")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func unusedLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func launchAnnouncingBrowser(t *testing.T, port int, tail string) *browserprocess.Browser {
	t.Helper()
	owner, err := browserprocess.Launch(t.Context(), browserprocess.LaunchConfig{
		BinaryPath: announcingBrowserScript(t, port, tail), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil && !browserprocess.IsCode(err, browserprocess.ErrorBrowserCrash) {
			t.Errorf("close fixture browser: %v", err)
		}
	})
	return owner
}

func TestHandshakeReportsBrowserExitAfterAnnouncementAsTypedCrash(t *testing.T) {
	owner := launchAnnouncingBrowser(t, unusedLoopbackPort(t), "echo 'renderer startup failed' >&2\nsleep 0.3\nexit 9")
	started := time.Now()
	_, _, _, err := handshakeChromium(t.Context(), owner.Endpoint(), owner)
	if !browserprocess.IsCode(err, browserprocess.ErrorBrowserCrash) {
		t.Fatalf("dead browser was not a typed crash: %v", err)
	}
	if !strings.Contains(err.Error(), "renderer startup failed") || !strings.Contains(err.Error(), "exit status 9") {
		t.Fatalf("crash lacks process evidence: %v", err)
	}
	if elapsed := time.Since(started); elapsed > connectHandshakeBudget/2 {
		t.Fatalf("crash waited %s instead of ending at process exit", elapsed)
	}
}

func TestHandshakeWaitsForLiveBrowserWithDelayedListener(t *testing.T) {
	port := unusedLoopbackPort(t)
	owner := launchAnnouncingBrowser(t, port, "trap 'exit 0' TERM INT\nwhile :; do sleep 1; done")
	// The listener appears after the old fixed retry window (~500ms).
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() {
			if err := conn.CloseNow(); err != nil && websocket.CloseStatus(err) == -1 && !errors.Is(err, net.ErrClosed) {
				t.Errorf("close fake DevTools connection: %v", err)
			}
		}()
		for {
			command, err := readCDPCommand(r.Context(), conn)
			if err != nil {
				return
			}
			result := map[string]any{}
			if command.Method == "Browser.getVersion" {
				result = map[string]any{"protocolVersion": "1.3", "product": "HeadlessChrome/fixture"}
			}
			if err := writeCDPMessage(r.Context(), conn, map[string]any{"id": command.ID, "result": result}); err != nil {
				return
			}
		}
	})}
	served := make(chan error, 1)
	timer := time.AfterFunc(900*time.Millisecond, func() {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			served <- err
			return
		}
		served <- server.Serve(listener)
	})
	t.Cleanup(func() {
		timer.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("shutdown fake DevTools: %v", err)
		}
		select {
		case err := <-served:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("fake DevTools listener: %v", err)
			}
		default:
		}
	})
	transport, monitor, version, err := handshakeChromium(t.Context(), owner.Endpoint(), owner)
	if err != nil {
		t.Fatalf("live browser with a delayed listener failed: %v", err)
	}
	monitor.Close()
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if version.Product != "HeadlessChrome/fixture" {
		t.Fatalf("handshake returned wrong identity: %+v", version)
	}
}

func TestHandshakeKeepsExternalAttachShort(t *testing.T) {
	started := time.Now()
	_, _, _, err := handshakeChromium(t.Context(), fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/x", unusedLoopbackPort(t)), nil)
	if err == nil || !IsCDPError(err, CDPErrorDial) {
		t.Fatalf("external attach to a dead endpoint: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("external attach waited %s", elapsed)
	}
}
