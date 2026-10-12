package cdpops

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCDPOpsParameterShapes(t *testing.T) {
	deltaX := 3.0
	deltaY := 4.0
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "navigate", value: navigateParams{URL: "https://example.com", Referrer: "https://ref.example"}, want: `{"url":"https://example.com","referrer":"https://ref.example"}`},
		{name: "reload preserves false", value: reloadParams{IgnoreCache: false}, want: `{"ignoreCache":false}`},
		{name: "history", value: navigationHistoryParams{}, want: `{}`},
		{name: "history entry", value: navigateToHistoryEntryParams{EntryID: 7}, want: `{"entryId":7}`},
		{name: "ready state", value: runtimeEvaluateParams{Expression: "document.readyState", ReturnByValue: true}, want: `{"expression":"document.readyState","returnByValue":true}`},
		{name: "mouse press", value: dispatchMouseEventParams{Type: "mousePressed", X: 1.5, Y: 2.5, Button: "left", ClickCount: 1}, want: `{"type":"mousePressed","x":1.5,"y":2.5,"button":"left","clickCount":1}`},
		{name: "mouse wheel", value: dispatchMouseEventParams{Type: "mouseWheel", X: 1, Y: 2, DeltaX: &deltaX, DeltaY: &deltaY}, want: `{"type":"mouseWheel","x":1,"y":2,"deltaX":3,"deltaY":4}`},
		{name: "touch start", value: dispatchTouchEventParams{Type: "touchStart", TouchPoints: []touchPoint{{X: 1.5, Y: 2.5}}}, want: `{"type":"touchStart","touchPoints":[{"x":1.5,"y":2.5}]}`},
		{name: "touch end", value: dispatchTouchEventParams{Type: "touchEnd", TouchPoints: []touchPoint{}}, want: `{"type":"touchEnd","touchPoints":[]}`},
		{name: "get document", value: getDocumentParams{Depth: -1, Pierce: true}, want: `{"depth":-1,"pierce":true}`},
		{name: "query selector", value: querySelectorAllParams{NodeID: 9, Selector: "button"}, want: `{"nodeId":9,"selector":"button"}`},
		{name: "describe node", value: describeNodeParams{NodeID: 9}, want: `{"nodeId":9}`},
		{name: "box model", value: getBoxModelParams{BackendNodeID: 42}, want: `{"backendNodeId":42}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != test.want {
				t.Fatalf("payload=%s, want %s", raw, test.want)
			}
		})
	}
}

type parameterCaptureCaller struct {
	methods []string
	params  []json.RawMessage
}

func (c *parameterCaptureCaller) Call(_ context.Context, method string, params any, _ any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.methods = append(c.methods, method)
	c.params = append(c.params, raw)
	return nil
}

func TestTouchTapUsesProtocolValidEndPayload(t *testing.T) {
	caller := &parameterCaptureCaller{}
	dispatcher := NewPointerDispatcher(caller)
	if err := dispatcher.TouchTapContext(context.Background(), 12.5, 24.5); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != 2 || caller.methods[0] != "Input.dispatchTouchEvent" || caller.methods[1] != "Input.dispatchTouchEvent" {
		t.Fatalf("methods=%v", caller.methods)
	}
	if string(caller.params[0]) != `{"type":"touchStart","touchPoints":[{"x":12.5,"y":24.5}]}` {
		t.Fatalf("touch start=%s", caller.params[0])
	}
	if string(caller.params[1]) != `{"type":"touchEnd","touchPoints":[]}` {
		t.Fatalf("touch end=%s", caller.params[1])
	}
	if dispatcher.EventCount() != 2 {
		t.Fatalf("event count=%d", dispatcher.EventCount())
	}
}
