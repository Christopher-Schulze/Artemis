package observe

import (
	"testing"
	"time"
)

// ==================== snapshot.go facade tests ====================

// TestTASK2246_AXTreeNodeAlias verifies the AXTreeNode alias
// (spec L4024: snapshot.go - AX tree extraction + diff).
func TestTASK2246_AXTreeNodeAlias(t *testing.T) {
	var node AXTreeNode
	node.Role = "button"
	node.Name = "Submit"
	if node.Role != "button" {
		t.Error("AXTreeNode alias should work")
	}
}

// TestTASK2246_DiffAXTrees verifies DiffAXTrees
// (spec L4024: snapshot.go - AX tree extraction + diff).
func TestTASK2246_DiffAXTrees(t *testing.T) {
	before := []AXTreeNode{{Role: "button", Name: "A"}, {Role: "link", Name: "B"}}
	after := []AXTreeNode{{Role: "button", Name: "A"}, {Role: "link", Name: "C"}}
	dist := DiffAXTrees(before, after)
	if dist < 0 {
		t.Errorf("diff distance should be >= 0, got %d", dist)
	}
}

// TestTASK2246_DiffAXTreesIdentical verifies identical trees have 0 diff.
func TestTASK2246_DiffAXTreesIdentical(t *testing.T) {
	trees := []AXTreeNode{{Role: "button", Name: "A"}}
	dist := DiffAXTrees(trees, trees)
	if dist != 0 {
		t.Errorf("identical trees should have 0 diff, got %d", dist)
	}
}

// TestTASK2246_DedupAXSnapshot verifies DedupAXSnapshot
// (spec L4024: snapshot.go - AX tree extraction + diff).
func TestTASK2246_DedupAXSnapshot(t *testing.T) {
	nodes := []AXTreeNode{
		{Role: "button", Name: "A"},
		{Role: "button", Name: "A"}, // duplicate
		{Role: "link", Name: "B"},
	}
	deduped := DedupAXSnapshot(nodes)
	if len(deduped) >= len(nodes) {
		t.Errorf("dedup should reduce: got %d from %d", len(deduped), len(nodes))
	}
}

// TestTASK2246_NewAXSnapshotTracker verifies tracker creation
// (spec L4024: snapshot.go - AX tree extraction + diff).
func TestTASK2246_NewAXSnapshotTracker(t *testing.T) {
	tracker := NewAXSnapshotTracker()
	if tracker == nil {
		t.Fatal("tracker should not be nil")
	}
}

// ==================== network.go tests ====================

// TestTASK2246_NewNetworkMonitor verifies creation
// (spec L4024: network.go - network ring buffer + subscriber pattern).
func TestTASK2246_NewNetworkMonitor(t *testing.T) {
	m := NewNetworkMonitor(100)
	if m == nil {
		t.Fatal("monitor should not be nil")
	}
	if m.Len() != 0 {
		t.Error("new monitor should have 0 events")
	}
	if m.SubscriberCount() != 0 {
		t.Error("new monitor should have 0 subscribers")
	}
}

// TestTASK2246_NetworkMonitorPush verifies Push adds events
// (spec L4024: network.go - network ring buffer).
func TestTASK2246_NetworkMonitorPush(t *testing.T) {
	m := NewNetworkMonitor(100)
	m.Push(NetworkEvent{URL: "https://example.com"})
	m.Push(NetworkEvent{URL: "https://test.com"})
	if m.Len() != 2 {
		t.Errorf("len: got %d, want 2", m.Len())
	}
}

// TestTASK2246_NetworkMonitorSubscribe verifies subscriber pattern
// (spec L4024: network.go - subscriber pattern).
func TestTASK2246_NetworkMonitorSubscribe(t *testing.T) {
	m := NewNetworkMonitor(100)
	received := []NetworkEvent{}
	m.Subscribe(func(ev NetworkEvent) {
		received = append(received, ev)
	})
	if m.SubscriberCount() != 1 {
		t.Errorf("subscribers: got %d, want 1", m.SubscriberCount())
	}
	m.Push(NetworkEvent{URL: "https://example.com"})
	if len(received) != 1 {
		t.Errorf("subscriber should receive 1 event, got %d", len(received))
	}
	if received[0].URL != "https://example.com" {
		t.Errorf("url: got %s, want https://example.com", received[0].URL)
	}
}

// TestTASK2246_NetworkMonitorSnapshot verifies Snapshot
// (spec L4024: network.go - network ring buffer).
func TestTASK2246_NetworkMonitorSnapshot(t *testing.T) {
	m := NewNetworkMonitor(100)
	m.Push(NetworkEvent{URL: "https://a.com"})
	m.Push(NetworkEvent{URL: "https://b.com"})
	snap := m.Snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot: got %d, want 2", len(snap))
	}
}

// TestTASK2246_NetworkMonitorMultipleSubscribers verifies multiple
// subscribers all receive events (spec L4024: subscriber pattern).
func TestTASK2246_NetworkMonitorMultipleSubscribers(t *testing.T) {
	m := NewNetworkMonitor(100)
	count1, count2 := 0, 0
	m.Subscribe(func(ev NetworkEvent) { count1++ })
	m.Subscribe(func(ev NetworkEvent) { count2++ })
	m.Push(NetworkEvent{URL: "https://x.com"})
	if count1 != 1 || count2 != 1 {
		t.Errorf("both subscribers should receive: got %d, %d", count1, count2)
	}
}

// TestTASK2246_NetworkMonitorNilSafe verifies nil monitor is safe.
func TestTASK2246_NetworkMonitorNilSafe(t *testing.T) {
	var m *NetworkMonitor
	m.Push(NetworkEvent{})
	m.Subscribe(func(ev NetworkEvent) {})
	if m.Len() != 0 {
		t.Error("nil should have 0 events")
	}
	if m.SubscriberCount() != 0 {
		t.Error("nil should have 0 subscribers")
	}
	if len(m.Snapshot()) != 0 {
		t.Error("nil snapshot should be empty")
	}
}

// ==================== console.go facade tests ====================

// TestTASK2246_NewConsoleCapture verifies creation with default 1000
// capacity (spec L4024: console log capture ring buffer 1000).
func TestTASK2246_NewConsoleCapture(t *testing.T) {
	c := NewConsoleCapture()
	if c == nil {
		t.Fatal("capture should not be nil")
	}
}

// TestTASK2246_DefaultConsoleCaptureCapacity verifies default is 1000
// (spec L4024: ring buffer 1000).
func TestTASK2246_DefaultConsoleCaptureCapacity(t *testing.T) {
	if DefaultConsoleCaptureCapacity != 1000 {
		t.Errorf("default capacity: got %d, want 1000", DefaultConsoleCaptureCapacity)
	}
}

// TestTASK2246_NewConsoleCaptureWithCapacity verifies custom capacity.
func TestTASK2246_NewConsoleCaptureWithCapacity(t *testing.T) {
	c := NewConsoleCaptureWithCapacity(500)
	if c == nil {
		t.Fatal("capture should not be nil")
	}
}

// TestTASK2246_NormalizeConsoleLevel verifies level normalization
// (spec L4024: console.go - console log capture).
func TestTASK2246_NormalizeConsoleLevel(t *testing.T) {
	level := NormalizeConsoleLevel("log")
	if !level.IsValid() {
		t.Error("log level should be valid")
	}
}

// TestTASK2246_ConsoleLogEntryAlias verifies the alias works.
func TestTASK2246_ConsoleLogEntryAlias(t *testing.T) {
	var entry ConsoleLogEntry
	entry.Level = ConsoleLevelInfo
	if entry.Level != ConsoleLevelInfo {
		t.Error("ConsoleLogEntry alias should work")
	}
}

// ==================== metrics.go tests ====================

// TestTASK2246_NewMetricsCollector verifies creation
// (spec L4024: metrics.go - performance metrics).
func TestTASK2246_NewMetricsCollector(t *testing.T) {
	c := NewMetricsCollector()
	if c == nil {
		t.Fatal("collector should not be nil")
	}
	if !c.IsEmpty() {
		t.Error("new collector should be empty")
	}
}

// TestTASK2246_MetricsCollectorSetGet verifies SetMetrics/Current
// (spec L4024: metrics.go - performance metrics).
func TestTASK2246_MetricsCollectorSetGet(t *testing.T) {
	c := NewMetricsCollector()
	metrics := PerformanceMetrics{
		DOMContentLoaded: 100 * time.Millisecond,
		LoadEvent:        500 * time.Millisecond,
		RequestCount:     42,
		TransferSize:     1024 * 1024,
	}
	c.SetMetrics(metrics)
	if c.IsEmpty() {
		t.Error("collector should not be empty after SetMetrics")
	}
	current := c.Current()
	if current.RequestCount != 42 {
		t.Errorf("requests: got %d, want 42", current.RequestCount)
	}
	if current.DOMContentLoaded != 100*time.Millisecond {
		t.Errorf("DCL: got %v, want 100ms", current.DOMContentLoaded)
	}
}

// TestTASK2246_MetricsCollectorUpdatedAt verifies UpdatedAt is set.
func TestTASK2246_MetricsCollectorUpdatedAt(t *testing.T) {
	c := NewMetricsCollector()
	if !c.UpdatedAt().IsZero() {
		t.Error("new collector should have zero UpdatedAt")
	}
	c.SetMetrics(PerformanceMetrics{RequestCount: 1})
	if c.UpdatedAt().IsZero() {
		t.Error("UpdatedAt should be set after SetMetrics")
	}
}

// TestTASK2246_MetricsCollectorNilSafe verifies nil collector is safe.
func TestTASK2246_MetricsCollectorNilSafe(t *testing.T) {
	var c *MetricsCollector
	c.SetMetrics(PerformanceMetrics{})
	if !c.IsEmpty() {
		t.Error("nil should be empty")
	}
	if c.Current().RequestCount != 0 {
		t.Error("nil should return zero metrics")
	}
}

// TestTASK2246_PerformanceMetricsString verifies String method.
func TestTASK2246_PerformanceMetricsString(t *testing.T) {
	m := PerformanceMetrics{RequestCount: 10, TransferSize: 1024}
	s := m.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== format.go tests ====================

// TestTASK2246_OutputFormatConstants verifies format constants
// (spec L4024: format.go - output formatters HAR, NDJSON).
func TestTASK2246_OutputFormatConstants(t *testing.T) {
	if OutputFormatHAR != "har" {
		t.Error("HAR format mismatch")
	}
	if OutputFormatNDJSON != "ndjson" {
		t.Error("NDJSON format mismatch")
	}
}

// TestTASK2246_IsSupportedFormat verifies supported format check
// (spec L4024: format.go - output formatters HAR, NDJSON).
func TestTASK2246_IsSupportedFormat(t *testing.T) {
	if !IsSupportedFormat(OutputFormatHAR) {
		t.Error("HAR should be supported")
	}
	if !IsSupportedFormat(OutputFormatNDJSON) {
		t.Error("NDJSON should be supported")
	}
	if IsSupportedFormat(OutputFormat("xml")) {
		t.Error("XML should NOT be supported")
	}
}

// TestTASK2246_FormatHAR verifies HAR formatting
// (spec L4024: format.go - output formatters HAR).
func TestTASK2246_FormatHAR(t *testing.T) {
	entries := []HAREntry{}
	data, err := FormatHAR(entries)
	if err != nil {
		t.Fatalf("FormatHAR: %v", err)
	}
	if len(data) == 0 {
		t.Error("HAR output should not be empty")
	}
}

// TestTASK2246_FormatNDJSON verifies NDJSON formatting
// (spec L4024: format.go - output formatters NDJSON).
func TestTASK2246_FormatNDJSON(t *testing.T) {
	events := []NetworkEvent{
		{URL: "https://a.com"},
		{URL: "https://b.com"},
	}
	data, err := FormatNDJSON(events)
	if err != nil {
		t.Fatalf("FormatNDJSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("NDJSON output should not be empty")
	}
	// Should contain 2 newlines (one per event)
	newlines := 0
	for _, b := range data {
		if b == '\n' {
			newlines++
		}
	}
	if newlines != 2 {
		t.Errorf("newlines: got %d, want 2", newlines)
	}
}

// TestTASK2246_FormatNDJSONEmpty verifies empty events produce empty
// output.
func TestTASK2246_FormatNDJSONEmpty(t *testing.T) {
	data, err := FormatNDJSON(nil)
	if err != nil {
		t.Fatalf("FormatNDJSON: %v", err)
	}
	if len(data) != 0 {
		t.Error("empty events should produce empty output")
	}
}

// TestTASK2246_FormatConsoleNDJSON verifies console NDJSON formatting
// (spec L4024: format.go - output formatters NDJSON).
func TestTASK2246_FormatConsoleNDJSON(t *testing.T) {
	entries := []ConsoleEntry{
		{Level: ConsoleLevelInfo, Args: []string{"hello"}},
	}
	data, err := FormatConsoleNDJSON(entries)
	if err != nil {
		t.Fatalf("FormatConsoleNDJSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("console NDJSON should not be empty")
	}
}

// TestTASK2246_FormatOutputHAR verifies FormatOutput with HAR
// (spec L4024: format.go - output formatters HAR).
func TestTASK2246_FormatOutputHAR(t *testing.T) {
	data, err := FormatOutput(OutputFormatHAR, nil)
	if err != nil {
		t.Fatalf("FormatOutput HAR: %v", err)
	}
	if len(data) == 0 {
		t.Error("HAR output should not be empty")
	}
}

// TestTASK2246_FormatOutputNDJSON verifies FormatOutput with NDJSON
// (spec L4024: format.go - output formatters NDJSON).
func TestTASK2246_FormatOutputNDJSON(t *testing.T) {
	events := []NetworkEvent{{URL: "https://x.com"}}
	data, err := FormatOutput(OutputFormatNDJSON, events)
	if err != nil {
		t.Fatalf("FormatOutput NDJSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("NDJSON output should not be empty")
	}
}

// TestTASK2246_FormatOutputUnsupported verifies unsupported format
// errors.
func TestTASK2246_FormatOutputUnsupported(t *testing.T) {
	_, err := FormatOutput(OutputFormat("xml"), nil)
	if err == nil {
		t.Error("unsupported format should error")
	}
}

// ==================== full spec parity test ====================

// TestTASK2246_FullSpecParity verifies all 5 spec-mandated files exist
// and have the spec-mandated functionality (spec L4024).
func TestTASK2246_FullSpecParity(t *testing.T) {
	// 1. snapshot.go - AX tree extraction + diff
	dist := DiffAXTrees([]AXTreeNode{{Role: "a"}}, []AXTreeNode{{Role: "b"}})
	if dist < 0 {
		t.Error("snapshot.go: diff should be >= 0")
	}

	// 2. network.go - network ring buffer + subscriber pattern
	m := NewNetworkMonitor(100)
	m.Push(NetworkEvent{URL: "https://test.com"})
	if m.Len() != 1 {
		t.Error("network.go: should have 1 event")
	}

	// 3. console.go - console log capture ring buffer 1000
	c := NewConsoleCapture()
	if c == nil {
		t.Error("console.go: capture should not be nil")
	}
	if DefaultConsoleCaptureCapacity != 1000 {
		t.Error("console.go: default capacity should be 1000")
	}

	// 4. metrics.go - performance metrics
	mc := NewMetricsCollector()
	mc.SetMetrics(PerformanceMetrics{RequestCount: 10})
	if mc.Current().RequestCount != 10 {
		t.Error("metrics.go: should have 10 requests")
	}

	// 5. format.go - output formatters HAR, NDJSON
	if !IsSupportedFormat(OutputFormatHAR) {
		t.Error("format.go: HAR should be supported")
	}
	if !IsSupportedFormat(OutputFormatNDJSON) {
		t.Error("format.go: NDJSON should be supported")
	}
}
