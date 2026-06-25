package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultTracingConfig(t *testing.T) {
	c := DefaultTracingConfig
	if len(c.Categories) != 15 {
		t.Fatalf("categories=%d want 15", len(c.Categories))
	}
	if c.TransferMode != TransferModeReturnAsStream {
		t.Fatalf("transferMode=%s", c.TransferMode)
	}
	if c.MaxEvents != 5000000 {
		t.Fatalf("maxEvents=%d", c.MaxEvents)
	}
	if !c.Enabled {
		t.Fatal("should be enabled")
	}
}

func TestStartStop(t *testing.T) {
	s := NewTracingSession()
	if err := s.Start(DefaultTracingConfig); err != nil {
		t.Fatal(err)
	}
	if !s.IsRunning() {
		t.Fatal("should be running")
	}
	events, err := s.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if s.IsRunning() {
		t.Fatal("should not be running after stop")
	}
	if events != nil && len(events) != 0 {
		t.Fatalf("events=%d", len(events))
	}
}

func TestStartAlreadyStarted(t *testing.T) {
	s := NewTracingSession()
	if err := s.Start(DefaultTracingConfig); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if err := s.Start(DefaultTracingConfig); err == nil {
		t.Fatal("expected error on double start")
	}
}

func TestStopWithoutStart(t *testing.T) {
	s := NewTracingSession()
	if _, err := s.Stop(); err == nil {
		t.Fatal("expected error stopping idle session")
	}
}

func TestStartDisabled(t *testing.T) {
	s := NewTracingSession()
	cfg := DefaultTracingConfig
	cfg.Enabled = false
	if err := s.Start(cfg); err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestStartNoCategories(t *testing.T) {
	s := NewTracingSession()
	cfg := DefaultTracingConfig
	cfg.Categories = nil
	if err := s.Start(cfg); err == nil {
		t.Fatal("expected error with no categories")
	}
}

func TestAddEvent(t *testing.T) {
	s := NewTracingSession()
	if err := s.Start(DefaultTracingConfig); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if !s.AddEvent(NewTraceEvent("devtools.timeline", "X", "b", 1, 1, nil)) {
		t.Fatal("event should be accepted")
	}
	if s.GetEventCount() != 1 {
		t.Fatalf("count=%d", s.GetEventCount())
	}
}

func TestAddEventWhenStopped(t *testing.T) {
	s := NewTracingSession()
	if s.AddEvent(NewTraceEvent("cat", "name", "b", 1, 1, nil)) {
		t.Fatal("event should be rejected when not running")
	}
}

func TestMaxEventsCap(t *testing.T) {
	s := NewTracingSession()
	cfg := DefaultTracingConfig
	cfg.MaxEvents = 3
	if err := s.Start(cfg); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		s.AddEvent(NewTraceEvent("cat", "n", "b", 1, 1, nil))
	}
	if s.GetEventCount() != 3 {
		t.Fatalf("count=%d want 3", s.GetEventCount())
	}
	if s.GetDroppedCount() != 2 {
		t.Fatalf("dropped=%d want 2", s.GetDroppedCount())
	}
}

func TestStopReturnsEvents(t *testing.T) {
	s := NewTracingSession()
	if err := s.Start(DefaultTracingConfig); err != nil {
		t.Fatal(err)
	}
	s.AddEvent(NewTraceEvent("cat", "a", "b", 1, 1, nil))
	s.AddEvent(NewTraceEvent("cat", "b", "b", 1, 1, nil))
	events, err := s.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events=%d", len(events))
	}
}

func TestExportChromeTrace(t *testing.T) {
	events := []TraceEvent{
		{Cat: "devtools.timeline", Name: "X", Phase: "b", Timestamp: 1000, PID: 1, TID: 1},
		{Cat: "v8", Name: "Y", Phase: "e", Timestamp: 2000, PID: 1, TID: 1, Args: map[string]any{"k": "v"}},
	}
	raw, err := ExportChromeTrace(events)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "traceEvents") {
		t.Fatalf("missing traceEvents: %s", raw)
	}
	var file struct {
		TraceEvents []TraceEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.TraceEvents) != 2 {
		t.Fatalf("events=%d", len(file.TraceEvents))
	}
}

func TestImportChromeTrace(t *testing.T) {
	raw := []byte(`{"traceEvents":[{"cat":"c","name":"n","ph":"b","ts":1,"pid":2,"tid":3}]}`)
	events, err := ImportChromeTrace(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Name != "n" {
		t.Fatalf("events=%+v", events)
	}
}

func TestImportExportRoundTrip(t *testing.T) {
	events := []TraceEvent{
		{Cat: "a", Name: "x", Phase: "B", Timestamp: 10, PID: 1, TID: 2, Args: map[string]any{"n": 1}},
	}
	raw, err := ExportChromeTrace(events)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ImportChromeTrace(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Cat != "a" || back[0].Name != "x" {
		t.Fatalf("back=%+v", back)
	}
}

func TestCategories(t *testing.T) {
	s := NewTracingSession()
	if err := s.Start(DefaultTracingConfig); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	cats := s.Categories()
	if len(cats) != 15 {
		t.Fatalf("cats=%d", len(cats))
	}
	// Mutating returned slice must not affect session.
	cats[0] = "mutated"
	if s.Categories()[0] == "mutated" {
		t.Fatal("returned slice should be a copy")
	}
}

func TestDuration(t *testing.T) {
	s := NewTracingSession()
	if s.Duration() != 0 {
		t.Fatalf("idle duration=%v", s.Duration())
	}
	if err := s.Start(DefaultTracingConfig); err != nil {
		t.Fatal(err)
	}
	if s.Duration() <= 0 {
		t.Fatal("running duration should be positive")
	}
	s.Stop()
	if s.Duration() <= 0 {
		t.Fatal("stopped duration should be positive")
	}
}

func TestNewTraceEventTimestamp(t *testing.T) {
	e := NewTraceEvent("cat", "name", "b", 1, 1, map[string]any{"k": "v"})
	if e.Timestamp == 0 {
		t.Fatal("timestamp should be set")
	}
	if e.Args["k"] != "v" {
		t.Fatalf("args=%+v", e.Args)
	}
}

func TestStartZeroMaxEventsUsesDefault(t *testing.T) {
	s := NewTracingSession()
	cfg := DefaultTracingConfig
	cfg.MaxEvents = 0
	if err := s.Start(cfg); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	// Should not panic and should accept events.
	if !s.AddEvent(NewTraceEvent("c", "n", "b", 1, 1, nil)) {
		t.Fatal("event rejected")
	}
}

func TestExportEmpty(t *testing.T) {
	raw, err := ExportChromeTrace(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "traceEvents") {
		t.Fatalf("raw=%s", raw)
	}
}
