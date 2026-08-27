package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

func TestTargetIdentityValidation(t *testing.T) {
	for _, target := range []TargetIdentity{{}, {TargetID: "target"}, {SessionID: "session"}} {
		if err := target.Validate(); err == nil {
			t.Fatalf("identity %+v should fail validation", target)
		}
	}
	if err := (TargetIdentity{TargetID: "target", SessionID: "session"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTraceEventCollectorBindsAndSanitizesEvidence(t *testing.T) {
	collector := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session", BrowserContextID: "context"})
	collector.record(testTraceEvent("session", "Runtime.consoleAPICalled", map[string]any{
		"type": "warning", "timestamp": 1000.0, "args": []any{map[string]any{"value": "warn"}},
		"stackTrace": map[string]any{"callFrames": []any{map[string]any{"url": "https://example.test/app?token=secret"}}},
	}))
	collector.record(testTraceEvent("session", "Runtime.exceptionThrown", map[string]any{
		"timestamp": 1001.0, "exceptionDetails": map[string]any{
			"text": "boom", "url": "https://example.test/app?token=secret",
			"exception": map[string]any{"name": "Error", "description": "Error: boom"},
		},
	}))
	collector.record(testTraceEvent("session", "Network.requestWillBeSent", map[string]any{
		"requestId": "request-1", "timestamp": 1002.0, "type": "Document",
		"request": map[string]any{"url": "https://example.test/page?token=secret", "method": "GET"},
	}))
	collector.record(testTraceEvent("session", "Network.responseReceived", map[string]any{
		"requestId": "request-1", "response": map[string]any{"url": "https://example.test/page", "status": 200},
	}))
	evidence := collector.snapshot()
	if len(evidence.Console) != 1 || len(evidence.PageErrors) != 1 || len(evidence.Network) != 1 {
		t.Fatalf("evidence=%+v", evidence)
	}
	if evidence.Console[0].URL != "https://example.test/app" || evidence.Network[0].URL != "https://example.test/page" {
		t.Fatalf("sensitive URL query was retained: %+v", evidence)
	}
	if !evidence.Network[0].OK || evidence.Network[0].Status != 200 {
		t.Fatalf("network response was not applied: %+v", evidence.Network[0])
	}
}

func TestCaptureTraceArtifacts(t *testing.T) {
	page := &tracePageStub{}
	screenshot, err := captureTraceScreenshot(context.Background(), page)
	if err != nil || string(screenshot) != "png" {
		t.Fatalf("screenshot=%q err=%v", screenshot, err)
	}
	snapshot, err := captureTraceSnapshot(context.Background(), page)
	if err != nil || string(snapshot) != "<html>snapshot</html>" {
		t.Fatalf("snapshot=%q err=%v", snapshot, err)
	}
	sources, truncated, err := captureTraceSources(context.Background(), page)
	if err != nil || truncated || len(sources) != 2 {
		t.Fatalf("sources=%+v truncated=%v err=%v", sources, truncated, err)
	}
	if sources[0].URL > sources[1].URL || string(sources[0].Data) == "" || string(sources[1].Data) == "" {
		t.Fatalf("sources are not deterministic: %+v", sources)
	}
}

func TestTraceRecorderArchiveCarriesIdentityAndDebug(t *testing.T) {
	config := DefaultTraceRecordConfig(t.TempDir())
	config.Sources = true
	recorder, err := NewTraceRecorderWithTarget(config, TargetIdentity{
		TargetID: "target", SessionID: "session", BrowserContextID: "context",
	})
	if err != nil {
		t.Fatal(err)
	}
	if startErr := recorder.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	if sourceErr := recorder.AddNamedSource(TraceSource{URL: "https://example.test/app.js", Data: []byte("source")}); sourceErr != nil {
		t.Fatal(sourceErr)
	}
	if evidenceErr := recorder.SetDebugEvidence(TraceDebugEvidence{
		Target:  TargetIdentity{TargetID: "target", SessionID: "session", BrowserContextID: "context"},
		Console: []TraceConsoleEntry{{Type: "log", Text: "ready", Timestamp: time.Unix(0, 0).UTC()}},
	}); evidenceErr != nil {
		t.Fatal(evidenceErr)
	}
	path, err := recorder.Stop()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ReadTraceZip(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"trace.meta.json", "debug/console.json", "debug/page-errors.json", "debug/network.json", "sources/source-0001-example.test_app.js.txt"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("archive missing %s; entries=%v", name, entries)
		}
	}
	if !strings.Contains(string(entries["trace.meta.json"]), `"target_id":"target"`) {
		t.Fatalf("archive metadata lacks target identity: %s", entries["trace.meta.json"])
	}
}

func TestBrowserTraceRecorderRejectsNilSubscription(t *testing.T) {
	recorder, err := NewBrowserTraceRecorder(&tracePageStub{}, DefaultTraceRecordConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Start(context.Background()); err == nil {
		t.Fatal("expected nil subscription failure")
	}
	if recorder.IsActive() {
		t.Fatal("recorder must remain idle after start failure")
	}
}

type tracePageStub struct{}

func (tracePageStub) TargetID() string         { return "target" }
func (tracePageStub) SessionID() string        { return "session" }
func (tracePageStub) BrowserContextID() string { return "context" }
func (tracePageStub) SubscribeBrowserEvents(int) (*bridge.CDPSubscription, error) {
	var subscription *bridge.CDPSubscription
	return subscription, nil
}

func (tracePageStub) Call(_ context.Context, method string, _ any, result any) error {
	var value any
	switch method {
	case "Page.captureScreenshot":
		value = map[string]string{"data": base64.StdEncoding.EncodeToString([]byte("png"))}
	case "Runtime.evaluate":
		value = map[string]any{"result": map[string]any{"type": "string", "value": "<html>snapshot</html>"}}
	case "Page.getResourceTree":
		value = map[string]any{"frameTree": map[string]any{
			"frame": map[string]string{"id": "frame"},
			"resources": []any{
				map[string]string{"url": "https://example.test/b.js", "type": "Script"},
				map[string]string{"url": "https://example.test/a.css", "type": "Stylesheet"},
			},
		}}
	case "Page.getResourceContent":
		value = map[string]string{"content": "resource"}
	case "Page.enable", "Runtime.enable", "Network.enable", "Log.enable":
		return nil
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func testTraceEvent(sessionID, method string, params any) bridge.CDPEvent {
	raw, _ := json.Marshal(params)
	return bridge.CDPEvent{SessionID: sessionID, Method: method, Params: raw}
}
