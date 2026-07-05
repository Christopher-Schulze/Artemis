package bridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// HealthState is the bridge subsystem health surface (spec ss28.16 / bridge health.go).
type HealthState string

const (
	HealthHealthy    HealthState = "healthy"
	HealthDegraded   HealthState = "degraded"
	HealthRecovering HealthState = "recovering"
	HealthUnhealthy  HealthState = "unhealthy"
)

// HealthChecker tracks consecutive probe failures and recovery transitions.
type HealthChecker struct {
	mu        sync.Mutex
	threshold int
	failures  int
	state     HealthState
	lastProbe time.Time
}

func NewHealthChecker(failureThreshold int) *HealthChecker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	return &HealthChecker{threshold: failureThreshold, state: HealthHealthy}
}

func (h *HealthChecker) RecordProbe(ok bool) HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastProbe = time.Now()
	if ok {
		if h.state == HealthDegraded || h.state == HealthRecovering {
			h.state = HealthRecovering
			h.failures = 0
			return HealthRecovering
		}
		h.state = HealthHealthy
		h.failures = 0
		return HealthHealthy
	}
	h.failures++
	if h.failures >= h.threshold {
		h.state = HealthDegraded
		return HealthDegraded
	}
	return h.state
}

func (h *HealthChecker) Check() HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

func (h *HealthChecker) Recover() HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures = 0
	h.state = HealthHealthy
	return h.state
}

func (h *HealthChecker) FailureCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures
}

// CheckHealthRecovery runs probeFn until healthy or recovery budget elapses.
func CheckHealthRecovery(probeFn func() bool, checker *HealthChecker, attempts int) (HealthState, error) {
	if checker == nil {
		return "", fmt.Errorf("bridge health: nil checker")
	}
	if attempts <= 0 {
		attempts = 3
	}
	var last HealthState
	for i := 0; i < attempts; i++ {
		last = checker.RecordProbe(probeFn())
		if last == HealthHealthy || last == HealthRecovering {
			return checker.Recover(), nil
		}
	}
	return last, fmt.Errorf("bridge health: recovery failed after %d probes", attempts)
}

// --- 4-step CheckHealth flow (spec L4599) ---

// HealthStep identifies one of the 4 probe steps.
type HealthStep string

const (
	StepProcess HealthStep = "process"
	StepCDP     HealthStep = "cdp_targets"
	StepRender  HealthStep = "render"
	StepDOM     HealthStep = "dom"
)

// StepResult is the outcome of one probe step.
type StepResult struct {
	Step   HealthStep
	OK     bool
	Reason string
	TookMs int64
}

// CheckHealthResult is the aggregate outcome of the 4-step flow.
type CheckHealthResult struct {
	Healthy bool
	State   HealthState
	Steps   []StepResult
	TotalMs int64
}

// FailedStep returns the first failed step, or nil if all passed.
func (r *CheckHealthResult) FailedStep() *StepResult {
	for i := range r.Steps {
		if !r.Steps[i].OK {
			return &r.Steps[i]
		}
	}
	return nil
}

// HealthProbes abstracts the 4 probe steps for testability (spec L4599).
// Each probe returns (ok, reason). Production wires these to real
// process.Signal / CDP / screenshot / Runtime.evaluate calls.
type HealthProbes interface {
	// ProcessProbe: process.Signal(0) PID alive + non-zombie (reap with Wait() if defunct).
	ProcessProbe(ctx context.Context) (bool, string)
	// CDPProbe: CDP Target.getTargets within 500ms.
	CDPProbe(ctx context.Context) (bool, string)
	// RenderProbe: throwaway tab, about:blank + Page.captureScreenshot within 1s, PNG magic 0x89504E47.
	RenderProbe(ctx context.Context) (bool, string)
	// DOMProbe: Runtime.evaluate document.querySelectorAll('*').length > 0.
	DOMProbe(ctx context.Context) (bool, string)
}

// CheckHealthBudget is the total time budget for the 4-step flow (spec: 3s).
const CheckHealthBudget = 3 * time.Second

// CheckHealth runs the 4-step probe flow in sequence within a 3s budget
// (spec L4599). Any failure -> UNHEALTHY. Steps run in order so that the
// cheapest checks (process, CDP) short-circuit before expensive ones
// (render, DOM).
func CheckHealth(ctx context.Context, probes HealthProbes) CheckHealthResult {
	budgetCtx, cancel := context.WithTimeout(ctx, CheckHealthBudget)
	defer cancel()

	result := CheckHealthResult{State: HealthUnhealthy}
	start := time.Now()

	steps := []struct {
		step  HealthStep
		probe func(context.Context) (bool, string)
	}{
		{StepProcess, probes.ProcessProbe},
		{StepCDP, probes.CDPProbe},
		{StepRender, probes.RenderProbe},
		{StepDOM, probes.DOMProbe},
	}

	for _, s := range steps {
		if budgetCtx.Err() != nil {
			result.Steps = append(result.Steps, StepResult{
				Step:   s.step,
				OK:     false,
				Reason: "budget_exceeded",
			})
			result.TotalMs = time.Since(start).Milliseconds()
			return result
		}
		stepStart := time.Now()
		ok, reason := s.probe(budgetCtx)
		sr := StepResult{
			Step:   s.step,
			OK:     ok,
			Reason: reason,
			TookMs: time.Since(stepStart).Milliseconds(),
		}
		result.Steps = append(result.Steps, sr)
		if !ok {
			result.TotalMs = time.Since(start).Milliseconds()
			return result
		}
	}

	result.Healthy = true
	result.State = HealthHealthy
	result.TotalMs = time.Since(start).Milliseconds()
	return result
}

// --- Recovery path (spec L4599) ---

// RecoveryAction abstracts one recovery step. Production wires these to
// real Browser.close / SIGTERM / SIGKILL / lockfile deletion / relaunch.
type RecoveryAction interface {
	// CloseBrowser sends Browser.close RPC.
	CloseBrowser(ctx context.Context) error
	// TerminateProcess sends SIGTERM, waits 2s, then SIGKILL, reaps.
	TerminateProcess(ctx context.Context) error
	// DeleteLockfiles removes SingletonLock/.parent/.cookie.
	DeleteLockfiles(ctx context.Context) error
	// Relaunch starts bridge.Launch on a new port.
	Relaunch(ctx context.Context) error
	// RestoreProfile restores profile/storage state.
	RestoreProfile(ctx context.Context) error
}

// RecoveryResult is the outcome of the recovery path.
type RecoveryResult struct {
	Success    bool
	Step       string // step where recovery succeeded or failed
	DurationMs int64
	ReProbedOK bool
	Error      string
}

// RecoveryBudget is the total time budget for recovery (spec: Browser.close 2s + SIGTERM 2s + SIGKILL + reap + relaunch + re-probe).
const RecoveryBudget = 15 * time.Second

// Recover executes the bounded recovery sequence (spec L4599):
// Browser.close -> 2s -> SIGTERM -> 2s -> SIGKILL + reap -> delete lockfiles
// -> relaunch -> profile/storage restore -> single re-probe.
func Recover(ctx context.Context, action RecoveryAction, probes HealthProbes) RecoveryResult {
	budgetCtx, cancel := context.WithTimeout(ctx, RecoveryBudget)
	defer cancel()

	start := time.Now()
	result := RecoveryResult{Step: "start"}

	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"browser_close", action.CloseBrowser},
		{"terminate_process", action.TerminateProcess},
		{"delete_lockfiles", action.DeleteLockfiles},
		{"relaunch", action.Relaunch},
		{"restore_profile", action.RestoreProfile},
	}

	for _, s := range steps {
		if err := s.fn(budgetCtx); err != nil {
			if budgetCtx.Err() != nil {
				result.Step = s.name
				result.Error = fmt.Sprintf("budget_exceeded at %s: %v", s.name, err)
				result.DurationMs = time.Since(start).Milliseconds()
				return result
			}
			result.Step = s.name
			result.Error = err.Error()
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		}
	}

	// Single re-probe after recovery.
	reProbe := CheckHealth(budgetCtx, probes)
	result.Step = "reprobe"
	result.ReProbedOK = reProbe.Healthy
	result.Success = reProbe.Healthy
	if !reProbe.Healthy {
		if failed := reProbe.FailedStep(); failed != nil {
			result.Error = fmt.Sprintf("reprobe failed at %s: %s", failed.Step, failed.Reason)
		}
	}
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

// --- Piggyback probe (spec L4599) ---

// PiggybackBudget is the time budget for the piggyback micro-probe (spec: 500ms).
const PiggybackBudget = 500 * time.Millisecond

// PiggybackResult is the outcome of a piggyback micro-probe.
type PiggybackResult struct {
	OK     bool
	Reason string
	TookMs int64
	Tool   string // which browser tool triggered the piggyback
}

// PiggybackProbe runs a Target.getTargets micro-check with a 500ms budget,
// batched into the CDP stream (spec L4599). On micro-fail, the caller must
// run full CheckHealth + recovery.
func PiggybackProbe(ctx context.Context, probes HealthProbes, tool string) PiggybackResult {
	budgetCtx, cancel := context.WithTimeout(ctx, PiggybackBudget)
	defer cancel()
	start := time.Now()
	ok, reason := probes.CDPProbe(budgetCtx)
	return PiggybackResult{
		OK:     ok,
		Reason: reason,
		TookMs: time.Since(start).Milliseconds(),
		Tool:   tool,
	}
}

// --- Metrics (spec L4599) ---

// HealthMetrics tracks the spec-required counters/histograms.
type HealthMetrics struct {
	ChecksTotal            atomic.Int64
	FailuresTotal          atomic.Int64
	RecoveryDurationMs     atomic.Int64
	PiggybackChecksTotal   atomic.Int64
	PiggybackFailuresTotal atomic.Int64
	DetectionDelayMs       atomic.Int64
	TabLostTotal           atomic.Int64
	CleanupFailedTotal     atomic.Int64
	RecoveredSessionTotal  atomic.Int64
	StepFailures           sync.Map // HealthStep -> *atomic.Int64
}

// NewHealthMetrics creates a zeroed metrics struct.
func NewHealthMetrics() *HealthMetrics {
	return &HealthMetrics{}
}

// RecordStepFailure increments the per-step failure counter.
func (m *HealthMetrics) RecordStepFailure(step HealthStep) {
	if m == nil {
		return
	}
	v, _ := m.StepFailures.LoadOrStore(step, &atomic.Int64{})
	v.(*atomic.Int64).Add(1)
}

// StepFailureCount returns the per-step failure count.
func (m *HealthMetrics) StepFailureCount(step HealthStep) int64 {
	if m == nil {
		return 0
	}
	v, ok := m.StepFailures.Load(step)
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
}

// RecordCheck records a full CheckHealth result.
func (m *HealthMetrics) RecordCheck(result CheckHealthResult) {
	if m == nil {
		return
	}
	m.ChecksTotal.Add(1)
	if !result.Healthy {
		m.FailuresTotal.Add(1)
		if failed := result.FailedStep(); failed != nil {
			m.RecordStepFailure(failed.Step)
		}
	}
	m.DetectionDelayMs.Store(result.TotalMs)
}

// RecordPiggyback records a piggyback probe result.
func (m *HealthMetrics) RecordPiggyback(result PiggybackResult) {
	if m == nil {
		return
	}
	m.PiggybackChecksTotal.Add(1)
	if !result.OK {
		m.PiggybackFailuresTotal.Add(1)
	}
}

// RecordRecovery records a recovery result.
func (m *HealthMetrics) RecordRecovery(result RecoveryResult) {
	if m == nil {
		return
	}
	m.RecoveryDurationMs.Store(result.DurationMs)
	if result.Success {
		m.RecoveredSessionTotal.Add(1)
	}
}

// --- Circuit breaker (spec L4599: breaker open 60s) ---

// CircuitBreaker implements the 60s open window after recovery failure.
type CircuitBreaker struct {
	mu           sync.Mutex
	open         bool
	openedAt     time.Time
	openWindow   time.Duration
	failureCount int
	threshold    int
}

// NewCircuitBreaker creates a breaker that opens for openWindow after
// threshold consecutive recovery failures.
func NewCircuitBreaker(threshold int, openWindow time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 1
	}
	if openWindow <= 0 {
		openWindow = 60 * time.Second
	}
	return &CircuitBreaker{threshold: threshold, openWindow: openWindow}
}

// IsOpen reports whether the breaker is currently open (blocking calls).
func (b *CircuitBreaker) IsOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return false
	}
	if time.Since(b.openedAt) >= b.openWindow {
		b.open = false
		b.failureCount = 0
		return false
	}
	return true
}

// RecordSuccess resets the failure count.
func (b *CircuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount = 0
	b.open = false
}

// RecordFailure increments the failure count and opens the breaker if
// threshold is reached.
func (b *CircuitBreaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount++
	if b.failureCount >= b.threshold {
		b.open = true
		b.openedAt = time.Now()
	}
}

// --- Lockfile cleanup helper (spec L4599) ---

// ChromiumLockfiles are the lockfiles to delete during recovery.
var ChromiumLockfiles = []string{"SingletonLock", "SingletonSocket", "SingletonCookie"}

// DeleteChromiumLockfiles removes SingletonLock/SingletonSocket/SingletonCookie
// from the given user-data-dir (spec L4599).
func DeleteChromiumLockfiles(dataDir string) error {
	if dataDir == "" {
		return fmt.Errorf("bridge health: empty data dir")
	}
	for _, name := range ChromiumLockfiles {
		p := filepath.Join(dataDir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("bridge health: delete %s: %w", name, err)
		}
	}
	return nil
}
