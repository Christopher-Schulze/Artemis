package actions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActionCDPParameterShapes(t *testing.T) {
	quality := 83
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "empty", value: emptyParams{}, want: `{}`},
		{name: "key", value: dispatchKeyEventParams{Type: "keyDown", Key: "Enter", Text: "x"}, want: `{"type":"keyDown","key":"Enter","text":"x"}`},
		{name: "mouse", value: dispatchMouseEventParams{Type: "mousePressed", X: 1.5, Y: 2.5, Button: "left", Buttons: 1, ClickCount: 1}, want: `{"type":"mousePressed","x":1.5,"y":2.5,"button":"left","buttons":1,"clickCount":1}`},
		{name: "upload", value: setFileInputFilesParams{Files: []string{"/tmp/a"}, BackendNodeID: 42}, want: `{"files":["/tmp/a"],"backendNodeId":42}`},
		{name: "download allow", value: setDownloadBehaviorParams{Behavior: "allow", BrowserContextID: "ctx", DownloadPath: "/tmp/download", EventsEnabled: true}, want: `{"behavior":"allow","browserContextId":"ctx","downloadPath":"/tmp/download","eventsEnabled":true}`},
		{name: "download deny preserves false", value: setDownloadBehaviorParams{Behavior: "deny", EventsEnabled: false}, want: `{"behavior":"deny","eventsEnabled":false}`},
		{name: "cancel download", value: cancelDownloadParams{GUID: "guid", BrowserContextID: "ctx"}, want: `{"guid":"guid","browserContextId":"ctx"}`},
		{name: "screenshot", value: captureScreenshotParams{Format: "jpeg", Quality: &quality, FromSurface: true, CaptureBeyondViewport: true}, want: `{"format":"jpeg","quality":83,"fromSurface":true,"captureBeyondViewport":true}`},
		{name: "pdf", value: printToPDFParams{PrintBackground: true, TransferMode: "ReturnAsBase64"}, want: `{"printBackground":true,"transferMode":"ReturnAsBase64"}`},
		{name: "dialog", value: handleJavaScriptDialogParams{Accept: false, PromptText: "answer"}, want: `{"accept":false,"promptText":"answer"}`},
		{name: "viewport", value: setDeviceMetricsOverrideParams{Width: 1280, Height: 720, DeviceScaleFactor: 1, Mobile: false}, want: `{"width":1280,"height":720,"deviceScaleFactor":1,"mobile":false}`},
		{name: "isolated world wire spelling", value: createIsolatedWorldParams{FrameID: "frame", WorldName: "artemis-action", GrantUniversalAccess: false}, want: `{"frameId":"frame","worldName":"artemis-action","grantUniveralAccess":false}`},
		{name: "evaluate", value: runtimeEvaluateParams{Expression: "1+1", ContextID: 7, ReturnByValue: true, AwaitPromise: true}, want: `{"expression":"1+1","contextId":7,"returnByValue":true,"awaitPromise":true}`},
		{name: "resolve node", value: resolveNodeParams{BackendNodeID: 42}, want: `{"backendNodeId":42}`},
		{name: "call function", value: callFunctionOnParams{FunctionDeclaration: "function(v){return v}", ObjectID: "object", Arguments: []runtimeCallArgument{{Value: json.RawMessage(`true`)}}, ReturnByValue: true, AwaitPromise: true}, want: `{"functionDeclaration":"function(v){return v}","objectId":"object","arguments":[{"value":true}],"returnByValue":true,"awaitPromise":true}`},
		{name: "frame owner", value: getFrameOwnerParams{FrameID: "frame"}, want: `{"frameId":"frame"}`},
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

func TestRuntimeValueBoundaryRejectsProtocolDrift(t *testing.T) {
	valid := runtimeCallResult{Result: runtimeRemoteObject{Type: "object", Value: json.RawMessage(`{"width":1280,"height":720}`)}}
	var dimensions viewportDimensions
	if err := valid.valueInto(&dimensions); err != nil {
		t.Fatal(err)
	}
	if dimensions.Width != 1280 || dimensions.Height != 720 {
		t.Fatalf("dimensions=%+v", dimensions)
	}

	wrongNumericKind := runtimeCallResult{Result: runtimeRemoteObject{Type: "object", Value: json.RawMessage(`{"width":"1280","height":720}`)}}
	if err := wrongNumericKind.valueInto(&dimensions); err == nil {
		t.Fatal("string width accepted as an integer")
	}
	missingValue := runtimeCallResult{Result: runtimeRemoteObject{Type: "object"}}
	if err := missingValue.valueInto(&dimensions); err == nil {
		t.Fatal("missing by-value payload accepted")
	}
	exception := runtimeCallResult{ExceptionDetails: &runtimeExceptionDetails{Text: "boom"}}
	if _, err := exception.value(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("exception boundary error=%v", err)
	}
	undefined := runtimeCallResult{Result: runtimeRemoteObject{Type: "undefined"}}
	value, err := undefined.value()
	if err != nil || value != nil {
		t.Fatalf("undefined value=%v error=%v", value, err)
	}
	if _, err := encodeCallArguments([]any{make(chan struct{})}); err == nil {
		t.Fatal("non-JSON runtime argument accepted")
	}
}
