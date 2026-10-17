package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTargetDownRetainsCleanupAndRetryOwnershipOnCloseFailure(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		closes := 0
		for {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return err
			}
			var result any
			switch command.Method {
			case "Target.closeTarget":
				closes++
				result = map[string]bool{"success": closes > 1}
			case "Target.getTargets":
				result = map[string]any{"targetInfos": []any{map[string]string{"targetId": "owned"}}}
			default:
				return errors.New("unexpected cleanup protocol call")
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": result}); err != nil {
				return err
			}
		}
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, pageSlots: 1, pages: make(map[string]*Page), sessionMap: make(map[string]*Page)}
	owner := &BrowserContext{browser: browser, pages: make(map[string]*Page)}
	page, err := owner.registerPage("owned", "renderer", TargetScriptConfig{})
	if err != nil {
		t.Fatal(err)
	}
	cleaned := make(chan struct{})
	if err := page.AddCleanup(func() error { close(cleaned); return nil }); err != nil {
		t.Fatal(err)
	}
	page.setState(TargetStateCrashed, "fault injected renderer loss")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for page.RetirementError() == nil {
		select {
		case <-ctx.Done():
			t.Fatal("failed automatic close lost its error")
		case <-ticker.C:
		}
	}
	select {
	case <-cleaned:
		t.Fatal("failed close discarded the session retry resource")
	default:
	}
	if len(page.ContextPages()) != 1 {
		t.Fatal("failed close discarded native identity")
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("confirmed retry did not retire resources")
	}
	if len(page.ContextPages()) != 0 || browser.pageSlots != 0 {
		t.Fatal("confirmed retry retained target/quota")
	}
}
