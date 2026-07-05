package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProbes implements HealthProbes with configurable per-step results.
type fakeProbes struct {
	processOK, processFail bool
	cdpOK, cdpFail         bool
	renderOK, renderFail   bool
	domOK, domFail         bool
	processDelay           time.Duration
	processCalls, cdpCalls atomic.Int64
	renderCalls, domCalls  atomic.Int64
	processReason          string
}

func (f *fakeProbes) ProcessProbe(ctx context.Context) (bool, string) {
	f.processCalls.Add(1)
	if f.processDelay > 0 {
		select {
		case <-time.After(f.processDelay):
		case <-ctx.Done():
			return false, "budget_exceeded"
		}
	}
	if f.processFail {
		return false, f.processReason
	}
	return f.processOK, "ok"
}

func (f *fakeProbes) CDPProbe(ctx context.Context) (bool, string) {
	f.cdpCalls.Add(1)
	if f.cdpFail {
		return false, "cdp_timeout"
	}
	return f.cdpOK, "ok"
}

func (f *fakeProbes) RenderProbe(ctx context.Context) (bool, string) {
	f.renderCalls.Add(1)
	if f.renderFail {
		return false, "screenshot_failed"
	}
	return f.renderOK, "ok"
}

func (f *fakeProbes) DOMProbe(ctx context.Context) (bool, string) {
	f.domCalls.Add(1)
	if f.domFail {
		return false, "dom_empty"
	}
	return f.domOK, "ok"
}

func allOKProbes() *fakeProbes {
	return &fakeProbes{
		processOK: true,
		cdpOK:     true,
		renderOK:  true,
		domOK:     true,
	}
}

func TestCheckHealth_AllStepsPass(t *testing.T) {
	p := allOKProbes()
	res := CheckHealth(context.Background(), p)
	if !res.Healthy {
		t.Fatalf("expected healthy, state=%s", res.State)
	}
	if len(res.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(res.Steps))
	}
	for _, s := range res.Steps {
		if !s.OK {
			t.Fatalf("step %s not OK: %s", s.Step, s.Reason)
		}
	}
	if p.processCalls.Load() != 1 || p.cdpCalls.Load() != 1 || p.renderCalls.Load() != 1 || p.domCalls.Load() != 1 {
		t.Fatal("not all probes called exactly once")
	}
}

func TestCheckHealth_ProcessFail_ShortCircuits(t *testing.T) {
	p := &fakeProbes{
		processOK: false, processFail: true, processReason: "pid_dead",
		cdpOK: true, renderOK: true, domOK: true,
	}
	res := CheckHealth(context.Background(), p)
	if res.Healthy {
		t.Fatal("should not be healthy when process fails")
	}
	if res.State != HealthUnhealthy {
		t.Fatalf("state=%s want unhealthy", res.State)
	}
	if p.cdpCalls.Load() != 0 || p.renderCalls.Load() != 0 || p.domCalls.Load() != 0 {
		t.Fatal("should short-circuit after process failure")
	}
	failed := res.FailedStep()
	if failed == nil || failed.Step != StepProcess {
		t.Fatalf("expected process failure, got %+v", failed)
	}
	if failed.Reason != "pid_dead" {
		t.Fatalf("reason=%s", failed.Reason)
	}
}

func TestCheckHealth_CDPFail_ShortCircuits(t *testing.T) {
	p := &fakeProbes{
		processOK: true,
		cdpOK:     false, cdpFail: true,
		renderOK: true, domOK: true,
	}
	res := CheckHealth(context.Background(), p)
	if res.Healthy {
		t.Fatal("should not be healthy when CDP fails")
	}
	if p.renderCalls.Load() != 0 || p.domCalls.Load() != 0 {
		t.Fatal("should short-circuit after CDP failure")
	}
	failed := res.FailedStep()
	if failed == nil || failed.Step != StepCDP {
		t.Fatalf("expected CDP failure, got %+v", failed)
	}
}

func TestCheckHealth_RenderFail_ShortCircuits(t *testing.T) {
	p := &fakeProbes{
		processOK: true, cdpOK: true,
		renderOK: false, renderFail: true,
		domOK: true,
	}
	res := CheckHealth(context.Background(), p)
	if res.Healthy {
		t.Fatal("should not be healthy when render fails")
	}
	if p.domCalls.Load() != 0 {
		t.Fatal("should short-circuit after render failure")
	}
	failed := res.FailedStep()
	if failed == nil || failed.Step != StepRender {
		t.Fatalf("expected render failure, got %+v", failed)
	}
}

func TestCheckHealth_DOMFail(t *testing.T) {
	p := &fakeProbes{
		processOK: true, cdpOK: true, renderOK: true,
		domOK: false, domFail: true,
	}
	res := CheckHealth(context.Background(), p)
	if res.Healthy {
		t.Fatal("should not be healthy when DOM fails")
	}
	failed := res.FailedStep()
	if failed == nil || failed.Step != StepDOM {
		t.Fatalf("expected DOM failure, got %+v", failed)
	}
}

func TestCheckHealth_BudgetExceeded(t *testing.T) {
	p := &fakeProbes{
		processOK: true, processDelay: 4 * time.Second,
		cdpOK: true, renderOK: true, domOK: true,
	}
	res := CheckHealth(context.Background(), p)
	if res.Healthy {
		t.Fatal("should not be healthy when budget exceeded")
	}
}

func TestCheckHealth_FailedStep_NilWhenAllPass(t *testing.T) {
	p := allOKProbes()
	res := CheckHealth(context.Background(), p)
	if res.FailedStep() != nil {
		t.Fatal("FailedStep should be nil when all pass")
	}
}

// --- Recovery tests ---

type fakeRecoveryAction struct {
	closeOK, terminateOK, deleteOK, relaunchOK, restoreOK                bool
	closeErr, terminateErr, deleteErr, relaunchErr, restoreErr           error
	closeCalls, terminateCalls, deleteCalls, relaunchCalls, restoreCalls atomic.Int64
}

func (a *fakeRecoveryAction) CloseBrowser(ctx context.Context) error {
	a.closeCalls.Add(1)
	return a.closeErr
}
func (a *fakeRecoveryAction) TerminateProcess(ctx context.Context) error {
	a.terminateCalls.Add(1)
	return a.terminateErr
}
func (a *fakeRecoveryAction) DeleteLockfiles(ctx context.Context) error {
	a.deleteCalls.Add(1)
	return a.deleteErr
}
func (a *fakeRecoveryAction) Relaunch(ctx context.Context) error {
	a.relaunchCalls.Add(1)
	return a.relaunchErr
}
func (a *fakeRecoveryAction) RestoreProfile(ctx context.Context) error {
	a.restoreCalls.Add(1)
	return a.restoreErr
}

func TestRecover_Success(t *testing.T) {
	action := &fakeRecoveryAction{
		closeOK: true, terminateOK: true, deleteOK: true, relaunchOK: true, restoreOK: true,
	}
	probes := allOKProbes()
	res := Recover(context.Background(), action, probes)
	if !res.Success {
		t.Fatalf("expected success, step=%s err=%s", res.Step, res.Error)
	}
	if !res.ReProbedOK {
		t.Fatal("reprobe should pass")
	}
	if action.closeCalls.Load() != 1 || action.terminateCalls.Load() != 1 || action.deleteCalls.Load() != 1 || action.relaunchCalls.Load() != 1 || action.restoreCalls.Load() != 1 {
		t.Fatal("not all recovery steps called")
	}
}

func TestRecover_CloseBrowserFails(t *testing.T) {
	action := &fakeRecoveryAction{
		closeErr: errors.New("rpc_timeout"),
	}
	probes := allOKProbes()
	res := Recover(context.Background(), action, probes)
	if res.Success {
		t.Fatal("should not succeed when CloseBrowser fails")
	}
	if res.Step != "browser_close" {
		t.Fatalf("step=%s want browser_close", res.Step)
	}
	if action.terminateCalls.Load() != 0 {
		t.Fatal("should not continue after close failure")
	}
}

func TestRecover_TerminateFails(t *testing.T) {
	action := &fakeRecoveryAction{
		terminateErr: errors.New("sigkill_failed"),
	}
	probes := allOKProbes()
	res := Recover(context.Background(), action, probes)
	if res.Success {
		t.Fatal("should not succeed when terminate fails")
	}
	if res.Step != "terminate_process" {
		t.Fatalf("step=%s want terminate_process", res.Step)
	}
}

func TestRecover_RelaunchFails(t *testing.T) {
	action := &fakeRecoveryAction{
		relaunchErr: errors.New("port_in_use"),
	}
	probes := allOKProbes()
	res := Recover(context.Background(), action, probes)
	if res.Success {
		t.Fatal("should not succeed when relaunch fails")
	}
	if res.Step != "relaunch" {
		t.Fatalf("step=%s want relaunch", res.Step)
	}
}

func TestRecover_ReprobeFails(t *testing.T) {
	action := &fakeRecoveryAction{
		closeOK: true, terminateOK: true, deleteOK: true, relaunchOK: true, restoreOK: true,
	}
	probes := &fakeProbes{
		processOK: true, cdpOK: false, cdpFail: true, renderOK: true, domOK: true,
	}
	res := Recover(context.Background(), action, probes)
	if res.Success {
		t.Fatal("should not succeed when reprobe fails")
	}
	if res.Step != "reprobe" {
		t.Fatalf("step=%s want reprobe", res.Step)
	}
	if res.ReProbedOK {
		t.Fatal("reprobe should fail")
	}
}

// --- Piggyback tests ---

func TestPiggybackProbe_Success(t *testing.T) {
	p := allOKProbes()
	res := PiggybackProbe(context.Background(), p, "browse")
	if !res.OK {
		t.Fatalf("expected OK, reason=%s", res.Reason)
	}
	if res.Tool != "browse" {
		t.Fatalf("tool=%s want browse", res.Tool)
	}
}

func TestPiggybackProbe_Fail(t *testing.T) {
	p := &fakeProbes{
		processOK: true, cdpOK: false, cdpFail: true, renderOK: true, domOK: true,
	}
	res := PiggybackProbe(context.Background(), p, "click")
	if res.OK {
		t.Fatal("should fail when CDP fails")
	}
	if res.Tool != "click" {
		t.Fatalf("tool=%s want click", res.Tool)
	}
}

func TestPiggybackProbe_BudgetExceeded(t *testing.T) {
	// Test CDP budget via a fake with CDP delay exceeding 500ms budget.
	p2 := &cdpDelayProbes{delay: 600 * time.Millisecond}
	res := PiggybackProbe(context.Background(), p2, "scrape")
	if res.OK {
		t.Fatal("should fail when CDP budget exceeded")
	}
}

type cdpDelayProbes struct {
	delay time.Duration
}

func (p *cdpDelayProbes) ProcessProbe(ctx context.Context) (bool, string) { return true, "ok" }
func (p *cdpDelayProbes) CDPProbe(ctx context.Context) (bool, string) {
	select {
	case <-time.After(p.delay):
		return true, "ok"
	case <-ctx.Done():
		return false, "budget_exceeded"
	}
}
func (p *cdpDelayProbes) RenderProbe(ctx context.Context) (bool, string) { return true, "ok" }
func (p *cdpDelayProbes) DOMProbe(ctx context.Context) (bool, string)    { return true, "ok" }

// --- Metrics tests ---

func TestHealthMetrics_RecordCheck(t *testing.T) {
	m := NewHealthMetrics()
	p := allOKProbes()
	res := CheckHealth(context.Background(), p)
	m.RecordCheck(res)
	if m.ChecksTotal.Load() != 1 {
		t.Fatalf("checks=%d", m.ChecksTotal.Load())
	}
	if m.FailuresTotal.Load() != 0 {
		t.Fatalf("failures=%d", m.FailuresTotal.Load())
	}
}

func TestHealthMetrics_RecordCheck_Failure(t *testing.T) {
	m := NewHealthMetrics()
	p := &fakeProbes{processOK: false, processFail: true, processReason: "dead", cdpOK: true, renderOK: true, domOK: true}
	res := CheckHealth(context.Background(), p)
	m.RecordCheck(res)
	if m.FailuresTotal.Load() != 1 {
		t.Fatalf("failures=%d", m.FailuresTotal.Load())
	}
	if m.StepFailureCount(StepProcess) != 1 {
		t.Fatalf("step process failures=%d", m.StepFailureCount(StepProcess))
	}
}

func TestHealthMetrics_RecordPiggyback(t *testing.T) {
	m := NewHealthMetrics()
	m.RecordPiggyback(PiggybackResult{OK: true, Tool: "browse"})
	m.RecordPiggyback(PiggybackResult{OK: false, Tool: "click"})
	if m.PiggybackChecksTotal.Load() != 2 {
		t.Fatalf("piggyback checks=%d", m.PiggybackChecksTotal.Load())
	}
	if m.PiggybackFailuresTotal.Load() != 1 {
		t.Fatalf("piggyback failures=%d", m.PiggybackFailuresTotal.Load())
	}
}

func TestHealthMetrics_RecordRecovery(t *testing.T) {
	m := NewHealthMetrics()
	m.RecordRecovery(RecoveryResult{Success: true, DurationMs: 500})
	if m.RecoveredSessionTotal.Load() != 1 {
		t.Fatal("recovered session not counted")
	}
	if m.RecoveryDurationMs.Load() != 500 {
		t.Fatal("recovery duration not recorded")
	}
}

func TestHealthMetrics_NilSafe(t *testing.T) {
	var m *HealthMetrics
	m.RecordCheck(CheckHealthResult{})
	m.RecordPiggyback(PiggybackResult{})
	m.RecordRecovery(RecoveryResult{})
	m.RecordStepFailure(StepProcess)
	if m.StepFailureCount(StepProcess) != 0 {
		t.Fatal("nil metrics should return 0")
	}
}

// --- Circuit breaker tests ---

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	b := NewCircuitBreaker(2, 60*time.Second)
	if b.IsOpen() {
		t.Fatal("breaker should start closed")
	}
	b.RecordFailure()
	if b.IsOpen() {
		t.Fatal("breaker should not open after 1 failure")
	}
	b.RecordFailure()
	if !b.IsOpen() {
		t.Fatal("breaker should open after 2 failures")
	}
}

func TestCircuitBreaker_ClosesAfterWindow(t *testing.T) {
	b := NewCircuitBreaker(1, 50*time.Millisecond)
	b.RecordFailure()
	if !b.IsOpen() {
		t.Fatal("breaker should be open")
	}
	time.Sleep(60 * time.Millisecond)
	if b.IsOpen() {
		t.Fatal("breaker should close after window")
	}
}

func TestCircuitBreaker_RecordSuccessResets(t *testing.T) {
	b := NewCircuitBreaker(2, 60*time.Second)
	b.RecordFailure()
	b.RecordSuccess()
	b.RecordFailure()
	if b.IsOpen() {
		t.Fatal("breaker should not open after success reset")
	}
}

func TestCircuitBreaker_Defaults(t *testing.T) {
	b := NewCircuitBreaker(0, 0)
	if b.threshold != 1 {
		t.Fatalf("default threshold=%d want 1", b.threshold)
	}
	if b.openWindow != 60*time.Second {
		t.Fatalf("default window=%v want 60s", b.openWindow)
	}
}

// --- Lockfile cleanup tests ---

func TestDeleteChromiumLockfiles_RemovesExisting(t *testing.T) {
	dir := t.TempDir()
	for _, name := range ChromiumLockfiles {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("lock"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteChromiumLockfiles(dir); err != nil {
		t.Fatalf("DeleteChromiumLockfiles: %v", err)
	}
	for _, name := range ChromiumLockfiles {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("lockfile %s still exists", name)
		}
	}
}

func TestDeleteChromiumLockfiles_NonExistentOK(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteChromiumLockfiles(dir); err != nil {
		t.Fatalf("should not fail when lockfiles don't exist: %v", err)
	}
}

func TestDeleteChromiumLockfiles_EmptyDir(t *testing.T) {
	if err := DeleteChromiumLockfiles(""); err == nil {
		t.Fatal("should fail on empty dir")
	}
}

// --- Existing HealthChecker still works ---

func TestHealthChecker_StillWorks(t *testing.T) {
	h := NewHealthChecker(2)
	if h.RecordProbe(false) != HealthHealthy {
		t.Fatal("1 failure should not degrade")
	}
	if h.RecordProbe(false) != HealthDegraded {
		t.Fatal("2 failures should degrade")
	}
	if h.RecordProbe(true) != HealthRecovering {
		t.Fatal("recovery should be recovering state")
	}
	// Recover() explicitly resets to healthy.
	if h.Recover() != HealthHealthy {
		t.Fatal("Recover should reset to healthy")
	}
}

func TestCheckHealthResult_FailedStep(t *testing.T) {
	r := CheckHealthResult{
		Steps: []StepResult{
			{Step: StepProcess, OK: true},
			{Step: StepCDP, OK: false, Reason: "timeout"},
			{Step: StepRender, OK: false, Reason: "no_screenshot"},
		},
	}
	f := r.FailedStep()
	if f == nil || f.Step != StepCDP {
		t.Fatalf("expected CDP as first failure, got %+v", f)
	}
}
