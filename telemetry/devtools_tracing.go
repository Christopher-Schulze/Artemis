package telemetry

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// TracingTransferMode controls how trace events are delivered (spec L4268).
type TracingTransferMode string

const (
	TransferModeReportEvents   TracingTransferMode = "ReportEvents"
	TransferModeReturnAsStream TracingTransferMode = "ReturnAsStream"
)

// TracingConfig configures a DevTools Tracing.start session.
type TracingConfig struct {
	Categories   []string
	TransferMode TracingTransferMode
	MaxEvents    int
	Enabled      bool
}

// DefaultTracingConfig is the canonical tracing configuration: 15 categories,
// stream transfer, and a 5M event cap.
var DefaultTracingConfig = TracingConfig{
	Categories: []string{
		"disabled-by-default-devtools.timeline",
		"disabled-by-default-devtools.timeline.frame",
		"disabled-by-default-devtools.timeline.stack",
		"disabled-by-default-devtools.screenshot",
		"blink.console",
		"blink.user_timing",
		"devtools.timeline",
		"toplevel",
		"v8.execute",
		"v8.compile",
		"netlog",
		"gpu",
		"compositor",
		"benchmark",
		"disabled-by-default-cpu_profiler",
	},
	TransferMode: TransferModeReturnAsStream,
	MaxEvents:    5000000,
	Enabled:      true,
}

// TraceEvent is a single Chrome trace event in the DevTools trace format.
type TraceEvent struct {
	Cat       string         `json:"cat"`
	Name      string         `json:"name"`
	Phase     string         `json:"ph"`
	Timestamp int64          `json:"ts"`
	PID       int64          `json:"pid"`
	TID       int64          `json:"tid"`
	Args      map[string]any `json:"args,omitempty"`
}

// TracingSession represents an active or completed DevTools tracing session.
type TracingSession struct {
	mu        sync.RWMutex
	config    TracingConfig
	events    []TraceEvent
	started   bool
	startedAt time.Time
	stoppedAt time.Time
	dropped   int
}

// NewTracingSession builds an idle TracingSession.
func NewTracingSession() *TracingSession {
	return &TracingSession{}
}

// Start begins a tracing session with the supplied config. Returns an error
// if a session is already active or the config is disabled.
func (s *TracingSession) Start(config TracingConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("tracing session already started")
	}
	if !config.Enabled {
		return fmt.Errorf("tracing config is disabled")
	}
	if len(config.Categories) == 0 {
		return fmt.Errorf("tracing config requires at least one category")
	}
	max := config.MaxEvents
	if max <= 0 {
		max = DefaultTracingConfig.MaxEvents
	}
	s.config = config
	s.config.MaxEvents = max
	s.events = make([]TraceEvent, 0, 1024)
	s.started = true
	s.startedAt = time.Now()
	s.stoppedAt = time.Time{}
	s.dropped = 0
	return nil
}

// Stop ends the active tracing session and returns the collected events.
// Returns an error if no session is active.
func (s *TracingSession) Stop() ([]TraceEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, fmt.Errorf("no active tracing session")
	}
	s.started = false
	s.stoppedAt = time.Now()
	out := s.events
	s.events = nil
	return out, nil
}

// AddEvent appends a trace event to the active session. If the session's
// MaxEvents cap is reached, the event is dropped and the dropped counter is
// incremented. Returns true if accepted.
func (s *TracingSession) AddEvent(event TraceEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return false
	}
	if len(s.events) >= s.config.MaxEvents {
		s.dropped++
		return false
	}
	s.events = append(s.events, event)
	return true
}

// GetEventCount returns the number of events collected so far.
func (s *TracingSession) GetEventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// GetDroppedCount returns the number of events dropped due to the cap.
func (s *TracingSession) GetDroppedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dropped
}

// IsRunning reports whether a tracing session is currently active.
func (s *TracingSession) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// Categories returns the categories of the active or last config.
func (s *TracingSession) Categories() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.config.Categories))
	copy(out, s.config.Categories)
	return out
}

// Duration returns the elapsed time of the active or completed session.
func (s *TracingSession) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.started && s.stoppedAt.IsZero() {
		return 0
	}
	if s.started {
		return time.Since(s.startedAt)
	}
	return s.stoppedAt.Sub(s.startedAt)
}

// chromeTraceFile is the top-level Chrome trace JSON envelope.
type chromeTraceFile struct {
	TraceEvents []TraceEvent `json:"traceEvents"`
}

// ExportChromeTrace serializes events into the Chrome trace JSON format
// (an object with a "traceEvents" array).
func ExportChromeTrace(events []TraceEvent) ([]byte, error) {
	file := chromeTraceFile{TraceEvents: events}
	raw, err := json.Marshal(file)
	if err != nil {
		return nil, fmt.Errorf("marshal chrome trace: %w", err)
	}
	return raw, nil
}

// ImportChromeTrace parses a Chrome trace JSON file back into events.
func ImportChromeTrace(raw []byte) ([]TraceEvent, error) {
	var file chromeTraceFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("unmarshal chrome trace: %w", err)
	}
	return file.TraceEvents, nil
}

// NewTraceEvent builds a TraceEvent with a microsecond timestamp.
func NewTraceEvent(cat, name, phase string, pid, tid int64, args map[string]any) TraceEvent {
	return TraceEvent{
		Cat:       cat,
		Name:      name,
		Phase:     phase,
		Timestamp: time.Now().UnixMicro(),
		PID:       pid,
		TID:       tid,
		Args:      args,
	}
}
