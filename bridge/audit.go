package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// audit.go (spec L4249: OCSF audit ss6.1 browser action logging).
//
// Browser actions are logged as OCSF (Open Cybersecurity Schema
// Framework) events (ss6.1). This hook captures browser navigation,
// click, input, and other actions and emits OCSF-formatted audit
// events.

// OCSFEvent represents an OCSF-formatted audit event for a browser
// action (spec L4249, ss6.1).
type OCSFEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	Action    string            `json:"action"`
	TargetURL string            `json:"target_url,omitempty"`
	Actor     string            `json:"actor"`
	Result    string            `json:"result"`
	Duration  int64             `json:"duration_ms"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// OCSFAuditLogger is the interface for the host OCSF audit system
// The real implementation is provided by the embedding host.
type OCSFAuditLogger interface {
	LogOCSFEvent(ctx context.Context, event OCSFEvent) error
}

// AuditHook logs browser actions as OCSF events (ss6.1) (spec L4249).
type AuditHook struct {
	mu      sync.RWMutex
	logger  OCSFAuditLogger
	enabled bool
	stats   AuditHookStats
}

// AuditHookStats tracks audit hook activity.
type AuditHookStats struct {
	Total  int `json:"total"`
	Logged int `json:"logged"`
	Failed int `json:"failed"`
}

// NewAuditHook creates a new audit hook with the given logger.
func NewAuditHook(logger OCSFAuditLogger) *AuditHook {
	return &AuditHook{
		logger:  logger,
		enabled: true,
	}
}

// Enabled returns whether the hook is active.
func (h *AuditHook) Enabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

// SetEnabled enables or disables the hook.
func (h *AuditHook) SetEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
}

// Stats returns the current statistics.
func (h *AuditHook) Stats() AuditHookStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// ResetStats resets statistics (for testing).
func (h *AuditHook) ResetStats() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stats = AuditHookStats{}
}

// LogAction logs a browser action as an OCSF event (spec L4249).
func (h *AuditHook) LogAction(ctx context.Context, action, targetURL, actor, result string, durationMs int64, metadata map[string]string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stats.Total++

	if !h.enabled {
		return nil
	}

	if h.logger == nil {
		h.stats.Failed++
		return fmt.Errorf("no OCSF audit logger configured")
	}

	event := OCSFEvent{
		Timestamp: time.Now(),
		Action:    action,
		TargetURL: targetURL,
		Actor:     actor,
		Result:    result,
		Duration:  durationMs,
		Metadata:  metadata,
	}

	if err := h.logger.LogOCSFEvent(ctx, event); err != nil {
		h.stats.Failed++
		return err
	}

	h.stats.Logged++
	return nil
}

// LogNavigation is a convenience method for logging navigation events.
func (h *AuditHook) LogNavigation(ctx context.Context, url, actor string, durationMs int64, err error) error {
	result := "success"
	if err != nil {
		result = fmt.Sprintf("error: %v", err)
	}
	return h.LogAction(ctx, "navigate", url, actor, result, durationMs, nil)
}

// LogClick is a convenience method for logging click events.
func (h *AuditHook) LogClick(ctx context.Context, url, actor, selector string, durationMs int64, err error) error {
	result := "success"
	if err != nil {
		result = fmt.Sprintf("error: %v", err)
	}
	metadata := map[string]string{"selector": selector}
	return h.LogAction(ctx, "click", url, actor, result, durationMs, metadata)
}

// LogInput is a convenience method for logging input events.
func (h *AuditHook) LogInput(ctx context.Context, url, actor, selector string, durationMs int64, err error) error {
	result := "success"
	if err != nil {
		result = fmt.Sprintf("error: %v", err)
	}
	metadata := map[string]string{"selector": selector}
	return h.LogAction(ctx, "input", url, actor, result, durationMs, metadata)
}
