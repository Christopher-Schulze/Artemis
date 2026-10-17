package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/coder/websocket"
)

func TestChromiumPopupGuardClosesRealOwnedPopupAndKeepsExplicitTabs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: requireChromium(t).Path, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "popup browser", browser.Close)
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	var initialized struct {
		Result json.RawMessage `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{`document.body.innerHTML='<button style="position:fixed;left:0;top:0;width:200px;height:200px" onclick="window.open(\'about:blank\',\'_blank\')">popup</button>'; true`, true}, &initialized); err != nil {
		t.Fatal(err)
	}
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		params := struct {
			Type       string `json:"type"`
			X          int    `json:"x"`
			Y          int    `json:"y"`
			Button     string `json:"button"`
			ClickCount int    `json:"clickCount"`
		}{eventType, 50, 50, "left", 1}
		if err := page.Call(ctx, "Input.dispatchMouseEvent", params, nil); err != nil {
			t.Fatal(err)
		}
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		count, err := owner.PopupStatus()
		if err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned popup never received a confirmed close")
		case <-ticker.C:
		}
	}
	targets, err := browser.listTargetInfos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var live []string
	for _, target := range targets {
		if target.BrowserContextID == owner.ID() && target.Type == "page" {
			live = append(live, target.TargetID)
		}
	}
	if len(live) != 2 || page.State() != TargetStateAttached || sibling.State() != TargetStateAttached {
		t.Fatalf("popup cleanup damaged explicit tabs or leaked target: %v", live)
	}
	for _, raw := range []string{
		`{"targetInfo":{"targetId":"foreign","type":"page","openerId":"unknown","browserContextId":""}}`,
		`{"targetInfo":{"targetId":"foreign","type":"page","openerId":"` + page.TargetID() + `","browserContextId":"foreign"}}`,
	} {
		if _, closeTarget := browser.popupCloseForEvent(CDPEvent{Method: "Target.targetCreated", Params: json.RawMessage(raw)}); closeTarget {
			t.Fatal("popup policy claimed a foreign opener or context")
		}
	}
	if _, owned := browser.popupCloseForEvent(CDPEvent{Method: "Target.targetCreated", Params: json.RawMessage(`{"targetInfo":{"targetId":"late-popup","type":"page","openerId":"retired-opener","browserContextId":"` + owner.ID() + `"}}`)}); !owned {
		t.Fatal("retired opener lost the authority of its still-owned isolated context")
	}
}

func TestPopupFailureDisposesOwnedContextWhenMonitorStops(t *testing.T) {
	requested := make(chan struct{})
	disposed := make(chan struct{})
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		for {
			command, err := readCDPCommand(ctx, conn)
			if err != nil {
				return nil // The client owns connection teardown; its EOF ends this fixture.
			}
			var result any
			switch command.Method {
			case "Target.closeTarget":
				close(requested)
				result = map[string]bool{"success": false}
			case "Target.getTargets":
				result = map[string]any{"targetInfos": []any{map[string]string{"targetId": "popup"}}}
			case "Target.disposeBrowserContext":
				close(disposed)
				result = struct{}{}
			default:
				return errors.New("unexpected popup cleanup call")
			}
			if err := writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": result}); err != nil {
				return err
			}
		}
	})
	transport := dialTestTransport(t, endpoint, CDPTransportConfig{})
	defer closeTestCDPTransport(t, transport)
	browser := &ChromiumBrowser{transport: transport, contexts: make(map[string]*BrowserContext), pages: make(map[string]*Page)}
	owner := &BrowserContext{id: "owned", browser: browser, pages: make(map[string]*Page)}
	browser.contexts[owner.id] = owner
	events, failures := make(chan CDPEvent, 1), make(chan error, 1)
	events <- CDPEvent{Method: "Target.targetCreated", Params: json.RawMessage(`{"targetInfo":{"targetId":"popup","type":"page","openerId":"retired-opener","browserContextId":"owned"}}`)}
	done := make(chan struct{})
	go func() { defer close(done); browser.monitorTargets(&CDPSubscription{Events: events, Errors: failures}) }()
	defer func() { close(failures); <-done }()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	select {
	case <-requested:
	case <-ctx.Done():
		t.Fatal("popup was not sent to the real close worker")
	}
	failures <- errors.New("fault injected target stream loss")
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("monitor cleanup did not finish under its shared deadline")
	}
	select {
	case <-disposed:
	default:
		t.Fatal("failed popup escaped owned context disposal")
	}
	count, popupErr := owner.PopupStatus()
	if count != 0 || popupErr == nil || len(browser.contexts) != 0 {
		t.Fatalf("failed close invented success or leaked context: count=%d err=%v contexts=%d", count, popupErr, len(browser.contexts))
	}
}
