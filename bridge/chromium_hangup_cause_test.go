package bridge

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/coder/websocket"
)

func TestCDPTransportExplainsAnUnexpectedHangUp(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		_, err := readCDPCommand(ctx, conn)
		return err
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	consulted := 0
	transport.setCloseCause(func() error {
		consulted++
		return errors.New("chromium exited: signal: segmentation fault")
	})
	err := transport.Call(context.Background(), "Target.getTargets", nil, nil)
	if !IsCDPError(err, CDPErrorClosed) || !strings.Contains(err.Error(), "segmentation fault") {
		t.Fatalf("hang-up error does not carry the cause: %v", err)
	}
	if !strings.Contains(transport.Err().Error(), "segmentation fault") || consulted != 1 {
		t.Fatalf("terminal error %v, cause consulted %d times", transport.Err(), consulted)
	}
}

func TestCDPTransportExplicitCloseSkipsTheHangUpCause(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, _ *websocket.Conn) error {
		<-ctx.Done()
		return nil
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	consulted := 0
	transport.setCloseCause(func() error {
		consulted++
		return errors.New("must not be consulted")
	})
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if consulted != 0 || strings.Contains(transport.Err().Error(), "must not be consulted") {
		t.Fatalf("explicit close consulted the hang-up cause: %d %v", consulted, transport.Err())
	}
}

func TestChromiumKilledProcessExplainsTheClosedTransport(t *testing.T) {
	binary := requireChromium(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := browser.Close(); closeErr != nil && !strings.Contains(closeErr.Error(), "chromium exited") {
			t.Logf("close killed Chromium: %v", closeErr)
		}
	}()
	if err := browser.process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.Transport().Done():
	case <-ctx.Done():
		t.Fatal("transport stayed open after the Chromium process was killed")
	}
	_, listErr := browser.listTargetInfos(ctx)
	if listErr == nil || !strings.Contains(listErr.Error(), "chromium exited") || !strings.Contains(listErr.Error(), "killed") {
		t.Fatalf("closed transport does not explain the killed process: %v", listErr)
	}
}

// TestChromiumStoppedByArtemisNamesTheBudget reproduces the CI failure mode:
// Artemis' resource supervisor ends Chromium with SIGTERM, Chromium exits 0,
// and every later call must say that Artemis stopped it and why.
func TestChromiumStoppedByArtemisNamesTheBudget(t *testing.T) {
	binary := requireChromium(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second,
		ResourceBudget: browserprocess.ResourceBudget{MaxMemoryBytes: 1, SampleInterval: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := browser.Close(); closeErr != nil && !strings.Contains(closeErr.Error(), "memory bytes") {
			t.Logf("close stopped Chromium: %v", closeErr)
		}
	}()
	select {
	case <-browser.Transport().Done():
	case <-ctx.Done():
		t.Fatal("the resource budget did not stop Chromium")
	}
	_, listErr := browser.listTargetInfos(ctx)
	if listErr == nil || !strings.Contains(listErr.Error(), "terminated by Artemis") || !strings.Contains(listErr.Error(), "memory bytes") {
		t.Fatalf("closed transport does not name the Artemis budget stop: %v", listErr)
	}
}
