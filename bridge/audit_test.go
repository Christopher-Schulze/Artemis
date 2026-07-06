package bridge

import (
	"context"
	"errors"
	"testing"
)

// mockOCSFLogger for testing.
type mockOCSFLogger struct {
	err    error
	events []OCSFEvent
}

func (m *mockOCSFLogger) LogOCSFEvent(ctx context.Context, event OCSFEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

func TestAuditHook_LogAction_Success(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	err := hook.LogAction(context.Background(), "navigate", "https://example.com", "agent", "success", 100, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logger.events) != 1 {
		t.Errorf("expected 1 event logged, got %d", len(logger.events))
	}
	if logger.events[0].Action != "navigate" {
		t.Errorf("expected action=navigate, got %s", logger.events[0].Action)
	}
	if hook.Stats().Logged != 1 {
		t.Errorf("expected logged=1, got %d", hook.Stats().Logged)
	}
}

func TestAuditHook_LogAction_Disabled(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.SetEnabled(false)
	err := hook.LogAction(context.Background(), "navigate", "https://example.com", "agent", "success", 100, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logger.events) != 0 {
		t.Errorf("expected 0 events when disabled, got %d", len(logger.events))
	}
}

func TestAuditHook_LogAction_NoLogger(t *testing.T) {
	hook := NewAuditHook(nil)
	err := hook.LogAction(context.Background(), "navigate", "https://example.com", "agent", "success", 100, nil)
	if err == nil {
		t.Error("expected error with no logger")
	}
	if hook.Stats().Failed != 1 {
		t.Errorf("expected failed=1, got %d", hook.Stats().Failed)
	}
}

func TestAuditHook_LogAction_LoggerError(t *testing.T) {
	logger := &mockOCSFLogger{err: errors.New("log failure")}
	hook := NewAuditHook(logger)
	err := hook.LogAction(context.Background(), "navigate", "https://example.com", "agent", "success", 100, nil)
	if err == nil {
		t.Error("expected error from logger")
	}
	if hook.Stats().Failed != 1 {
		t.Errorf("expected failed=1, got %d", hook.Stats().Failed)
	}
}

func TestAuditHook_LogNavigation_Success(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogNavigation(context.Background(), "https://example.com", "agent", 50, nil)
	if logger.events[0].Action != "navigate" {
		t.Errorf("expected action=navigate, got %s", logger.events[0].Action)
	}
	if logger.events[0].Result != "success" {
		t.Errorf("expected result=success, got %s", logger.events[0].Result)
	}
}

func TestAuditHook_LogNavigation_Error(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogNavigation(context.Background(), "https://example.com", "agent", 50, errors.New("timeout"))
	if logger.events[0].Result != "error: timeout" {
		t.Errorf("expected result=error: timeout, got %s", logger.events[0].Result)
	}
}

func TestAuditHook_LogClick(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogClick(context.Background(), "https://example.com", "agent", "#button", 30, nil)
	if logger.events[0].Action != "click" {
		t.Errorf("expected action=click, got %s", logger.events[0].Action)
	}
	if logger.events[0].Metadata["selector"] != "#button" {
		t.Errorf("expected selector metadata, got %v", logger.events[0].Metadata)
	}
}

func TestAuditHook_LogInput(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogInput(context.Background(), "https://example.com", "agent", "#field", 20, nil)
	if logger.events[0].Action != "input" {
		t.Errorf("expected action=input, got %s", logger.events[0].Action)
	}
}

func TestAuditHook_Stats(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogAction(context.Background(), "navigate", "url1", "agent", "success", 10, nil)
	hook.LogAction(context.Background(), "click", "url2", "agent", "success", 20, nil)
	if hook.Stats().Total != 2 {
		t.Errorf("expected total=2, got %d", hook.Stats().Total)
	}
	if hook.Stats().Logged != 2 {
		t.Errorf("expected logged=2, got %d", hook.Stats().Logged)
	}
}

func TestAuditHook_ResetStats(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogAction(context.Background(), "navigate", "url", "agent", "success", 10, nil)
	hook.ResetStats()
	if hook.Stats().Total != 0 {
		t.Error("expected total=0 after reset")
	}
}

func TestAuditHook_Enabled(t *testing.T) {
	hook := NewAuditHook(&mockOCSFLogger{})
	if !hook.Enabled() {
		t.Error("expected enabled by default")
	}
	hook.SetEnabled(false)
	if hook.Enabled() {
		t.Error("expected disabled after SetEnabled(false)")
	}
}

func TestAuditHook_TimestampSet(t *testing.T) {
	logger := &mockOCSFLogger{}
	hook := NewAuditHook(logger)
	hook.LogAction(context.Background(), "navigate", "url", "agent", "success", 10, nil)
	if logger.events[0].Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}
