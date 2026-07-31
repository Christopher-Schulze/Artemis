package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

type liveCaller struct{}

func (liveCaller) Call(_ context.Context, method string, _ any, result any) error {
	var value any
	switch method {
	case "Accessibility.enable", "DOM.enable":
		value = map[string]any{}
	case "Page.getFrameTree":
		value = map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main"}}}
	case "Accessibility.getFullAXTree":
		value = map[string]any{"nodes": []any{map[string]any{
			"backendDOMNodeId": 4,
			"role":             map[string]any{"value": "button"},
			"name":             map[string]any{"value": "Continue"},
		}}}
	case "DOMSnapshot.captureSnapshot":
		value = map[string]any{
			"strings": []string{"main", "#document", "", "html", "body", "button", "display", "visibility", "opacity", "pointer-events", "Continue"},
			"documents": []any{map[string]any{
				"frameId": 0,
				"nodes": map[string]any{
					"parentIndex":   []int{-1, 0, 1, 2},
					"nodeType":      []int{1, 1, 1, 1},
					"nodeName":      []int{1, 3, 4, 5},
					"nodeValue":     []int{0, 0, 0, 0},
					"backendNodeId": []int64{1, 2, 3, 4},
					"attributes":    [][]int{{}, {}, {}, {}},
				},
				"layout": map[string]any{
					"nodeIndex": []int{0, 1, 2, 3},
					"styles":    [][]int{{6, 7, 8, 9}, {6, 7, 8, 9}, {6, 7, 8, 9}, {6, 7, 8, 9}},
					"bounds":    [][]float64{{0, 0, 800, 600}, {0, 0, 800, 600}, {0, 0, 800, 600}, {10, 10, 100, 30}},
				},
			}},
		}
	case "DOM.getDocument":
		value = map[string]any{"root": map[string]any{"backendNodeId": 1}}
	case "DOM.getNodeForLocation":
		value = map[string]any{"backendNodeId": 4}
	case "Performance.getMetrics":
		value = map[string]any{"metrics": []any{
			map[string]any{"name": "ResourceCount", "value": 4},
			map[string]any{"name": "TransferSize", "value": 128},
			map[string]any{"name": "FirstContentfulPaint", "value": 0.2},
		}}
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func TestLiveCollectorOwnsCompleteBoundedEvidence(t *testing.T) {
	config := DefaultLiveConfig()
	config.MaxEvidenceBytes = 4096
	collector, err := NewLiveCollector(liveCaller{}, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	collector.RecordNetwork(NetworkEvent{RequestID: "manual", URL: "https://fixture.test", Status: 200})
	collector.RecordConsole(ConsoleEntry{Level: ConsoleLevelInfo, Args: []string{"ready"}, Timestamp: time.Now().UTC()})
	collector.recordEvent(bridgeEvent("Network.requestWillBeSent", map[string]any{
		"requestId": "r1", "type": "Document", "request": map[string]any{
			"url": "https://fixture.test/index", "method": "GET",
			"headers": map[string]string{"Authorization": "secret", "Accept": "text/html"},
		},
	}))
	collector.recordEvent(bridgeEvent("Network.responseReceived", map[string]any{
		"requestId": "r1", "response": map[string]any{"url": "https://fixture.test/index", "status": 200, "mimeType": "text/html"},
	}))
	collector.recordEvent(bridgeEvent("Network.loadingFinished", map[string]any{"requestId": "r1", "encodedDataLength": 64}))
	collector.recordEvent(bridgeEvent("Runtime.consoleAPICalled", map[string]any{
		"type": "warning", "args": []any{map[string]any{"value": "challenge-free"}},
	}))

	evidence, err := collector.CaptureEvidence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != "artemis.observation.v1" || evidence.Snapshot.Schema != evidence.Schema {
		t.Fatalf("schema=%q snapshot=%q", evidence.Schema, evidence.Snapshot.Schema)
	}
	if len(evidence.Snapshot.Nodes) == 0 || len(evidence.Network) < 2 || len(evidence.Console) != 2 {
		t.Fatalf("evidence snapshot=%d network=%d console=%d", len(evidence.Snapshot.Nodes), len(evidence.Network), len(evidence.Console))
	}
	if evidence.Metrics.RequestCount != 4 || evidence.Metrics.FirstContentfulPaint != 200*time.Millisecond {
		t.Fatalf("metrics=%+v", evidence.Metrics)
	}
	for _, event := range evidence.Network {
		if event.RequestID == "r1" && event.Headers["Authorization"] != "" {
			t.Fatal("authorization header leaked into network evidence")
		}
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > config.MaxEvidenceBytes {
		t.Fatalf("evidence exceeded byte budget: %d", len(raw))
	}
}

func bridgeEvent(method string, params any) bridge.CDPEvent {
	raw, _ := json.Marshal(params)
	return bridge.CDPEvent{Method: method, Params: raw}
}
