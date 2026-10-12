package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/profile"
)

type contractRuntime struct {
	closed    atomic.Bool
	closeErr  error
	fetchErr  error
	returnNil bool
	fetchSeen atomic.Int64
}

func (r *contractRuntime) Fetch(ctx context.Context, _ string, _ engine.FetchOpts) (*engine.Page, error) {
	r.fetchSeen.Add(1)
	if r.fetchErr != nil {
		return nil, r.fetchErr
	}
	if r.returnNil {
		return nil, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *contractRuntime) Close() error {
	r.closed.Store(true)
	return r.closeErr
}

func (r *contractRuntime) Healthy() bool { return !r.closed.Load() }

type contractFactory struct {
	runtime RenderlessRuntime
	err     error
}

func (f contractFactory) Start(context.Context, AgentConfig) (RenderlessRuntime, error) {
	return f.runtime, f.err
}

// newTestEngineRuntime creates a real renderless engine that can fetch the
// provided httptest fixture server. It is used by tests that need a real
// runtime against loopback fixtures.
func newTestEngineRuntime(t *testing.T, srv *httptest.Server) RenderlessRuntime {
	t.Helper()
	cfg := engine.Config{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true}}
	if srv != nil {
		u, err := url.Parse(srv.URL)
		if err == nil {
			p := u.Port()
			if p != "" {
				if port, err := strconv.Atoi(p); err == nil {
					ports := []int{80, 443, port}
					sort.Ints(ports)
					cfg.PolicyConfig.AllowedPorts = ports
				}
			}
		}
	}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	return &ownedEngineRuntime{Engine: eng}
}

type retryFactory struct {
	mu      sync.Mutex
	partial *contractRuntime
	healthy *contractRuntime
	calls   int
}

func (f *retryFactory) Start(context.Context, AgentConfig) (RenderlessRuntime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		return f.partial, errors.New("first launch failed")
	}
	return f.healthy, nil
}

type contractTelemetry struct {
	mu     sync.Mutex
	events []AgentEvent
}

func (t *contractTelemetry) Record(event AgentEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *contractTelemetry) count(eventType AgentEventType) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := 0
	for _, event := range t.events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

type blockingDispatcher struct {
	started chan struct{}
}

func (d blockingDispatcher) Execute(ctx context.Context, _ RenderlessRuntime, _ Task) (*PageResult, error) {
	close(d.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

type errorDispatcher struct{ err error }

func (d errorDispatcher) Execute(context.Context, RenderlessRuntime, Task) (*PageResult, error) {
	return nil, d.err
}

type resultDispatcher struct{ result *PageResult }

func (d resultDispatcher) Execute(context.Context, RenderlessRuntime, Task) (*PageResult, error) {
	return d.result, nil
}

type unknownAction struct{}

func (unknownAction) Kind() ActionKind { return "unknown" }
func (unknownAction) artemisAction()   {}

func newContractAgent(t *testing.T, config AgentConfig, runtime RenderlessRuntime, dispatcher Dispatcher) (*Agent, *contractTelemetry) {
	t.Helper()
	telemetry := &contractTelemetry{}
	agent, err := NewAgentWithDependencies(config, Dependencies{
		RuntimeFactory: contractFactory{runtime: runtime}, Dispatcher: dispatcher,
		SessionStore: newMemorySessionStore(), Telemetry: telemetry,
	})
	if err != nil {
		t.Fatal(err)
	}
	return agent, telemetry
}

func startSession(t *testing.T, agent *Agent) *Session {
	t.Helper()
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	session, err := agent.CreateSession("owner")
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func stopContractAgent(t *testing.T, agent *Agent) {
	t.Helper()
	if err := agent.Stop(); err != nil {
		t.Errorf("stop agent: %v", err)
	}
}

func TestAgentExecutesFetchWithObservableEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>before</title></head><body><h1>before</h1><a href="/next">next</a><script>document.querySelector("h1").textContent="contract-body";</script></body></html>`))
	}))
	defer server.Close()

	agent, _ := newContractAgent(t, AgentConfig{}, newTestEngineRuntime(t, server), renderlessDispatcher{})
	session := startSession(t, agent)
	result := agent.ExecuteTask(context.Background(), Task{
		ID: "observable", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, RunScripts: true},
	})
	if !result.Success || result.Data == nil {
		t.Fatalf("success=%v code=%s error=%q data=%+v", result.Success, result.ErrorCode, result.Error, result.Data)
	}
	if result.Data.StatusCode != http.StatusOK || result.Data.Title != "before" {
		t.Fatalf("data = %+v", result.Data)
	}
	if result.Data.Markdown == "" || result.Data.Text == "" || result.Data.HTML == "" || len(result.Data.Links) != 1 {
		t.Fatalf("observable evidence missing: %+v", result.Data)
	}
	if !strings.Contains(result.Data.Markdown, "contract-body") {
		t.Fatalf("script mutation missing from markdown: %q", result.Data.Markdown)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentCapabilitySnapshotCombinesClaimsAndLiveHealth(t *testing.T) {
	agent := mustAgent(t, AgentConfig{})
	before := agent.CapabilitySnapshot()
	if before.Version != Version || before.Health.Renderless.Healthy || len(before.Capabilities) == 0 {
		t.Fatalf("before = %+v", before)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := agent.CapabilitySnapshot()
	if !after.Health.Renderless.Healthy || after.Health.Chromium.State != SupportUnavailable {
		t.Fatalf("after = %+v", after)
	}
	capability, ok := CapabilityByID("agent.high_level")
	if !ok || capability.State != SupportSupported || capability.Mode != ModeRenderless {
		t.Fatalf("agent capability = %+v, %v", capability, ok)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRenderlessDispatcherRejectsInvalidVariantsAndRuntimeFailures(t *testing.T) {
	runtime := &contractRuntime{}
	dispatcher := renderlessDispatcher{}
	tests := []struct {
		name   string
		action Action
		code   TaskErrorCode
	}{
		{name: "nil fetch pointer", action: (*FetchAction)(nil), code: TaskErrorInvalidInput},
		{name: "unknown action", action: unknownAction{}, code: TaskErrorCapabilityUnavailable},
		{name: "invalid URL", action: &FetchAction{URL: "file:///private/data"}, code: TaskErrorInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := dispatcher.Execute(context.Background(), runtime, Task{Action: test.action})
			if taskErrorCode(err) != test.code {
				t.Fatalf("err = %v", err)
			}
		})
	}
	runtime.fetchErr = errors.New("network failed")
	_, err := dispatcher.Execute(context.Background(), runtime, Task{Action: FetchAction{URL: "https://example.com"}})
	if taskErrorCode(err) != TaskErrorExecutionFailed {
		t.Fatalf("fetch error = %v", err)
	}
	runtime.fetchErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = dispatcher.Execute(ctx, runtime, Task{Action: FetchAction{URL: "https://example.com"}})
	if taskErrorCode(err) != TaskErrorCancelled {
		t.Fatalf("cancel error = %v", err)
	}
	runtime.returnNil = true
	_, err = dispatcher.Execute(context.Background(), runtime, Task{Action: FetchAction{URL: "https://example.com"}})
	if taskErrorCode(err) != TaskErrorExecutionFailed {
		t.Fatalf("nil page = %v", err)
	}
}

func TestTaskJSONRoundTripPreservesTypedAction(t *testing.T) {
	want := Task{
		ID: "wire", SessionID: "session", Timeout: 3 * time.Second,
		Action: FetchAction{URL: "https://example.com", RunScripts: true},
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Task
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	action, ok := got.Action.(FetchAction)
	if !ok || got.ID != want.ID || got.SessionID != want.SessionID || got.Timeout != want.Timeout || action.URL != "https://example.com" || !action.RunScripts {
		t.Fatalf("round trip = %+v action=%+v", got, action)
	}
	if err := json.Unmarshal([]byte(`{"id":"wire","action":{"type":"chromium.click"}}`), &got); taskErrorCode(err) != TaskErrorCapabilityUnavailable {
		t.Fatalf("unknown action = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"id":"wire","action":{}}`), &got); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("missing action type = %v", err)
	}
	if _, err := json.Marshal(Task{}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("missing action = %v", err)
	}
	if _, err := json.Marshal(Task{Action: (*FetchAction)(nil)}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("nil action = %v", err)
	}
	if _, err := json.Marshal(Task{Action: unknownAction{}}); taskErrorCode(err) != TaskErrorCapabilityUnavailable {
		t.Fatalf("unknown action = %v", err)
	}
	if _, err := json.Marshal(Task{Action: &FetchAction{URL: "https://example.com"}}); err != nil {
		t.Fatalf("pointer action = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"action":`), &got); err == nil {
		t.Fatalf("malformed task = %v", err)
	}
}

func TestTaskErrorSupportsErrorsIsAndCauseFreeFormatting(t *testing.T) {
	cause := errors.New("cause")
	err := newTaskError(TaskErrorExecutionFailed, "probe", cause)
	if !errors.Is(err, cause) || err.Error() == "" {
		t.Fatalf("err = %v", err)
	}
	if got := newTaskError(TaskErrorInvalidInput, "probe", nil).Error(); got != "artemis: probe: invalid_input" {
		t.Fatalf("cause-free error = %q", got)
	}
}

func TestAgentStartRollsBackPartialRuntime(t *testing.T) {
	runtime := &contractRuntime{closeErr: errors.New("rollback close failed")}
	agent, err := NewAgentWithDependencies(AgentConfig{}, Dependencies{
		RuntimeFactory: contractFactory{runtime: runtime, err: errors.New("launch failed")},
		Dispatcher:     renderlessDispatcher{}, SessionStore: newMemorySessionStore(), Telemetry: &contractTelemetry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = agent.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "launch failed") || !strings.Contains(err.Error(), "rollback close failed") || !runtime.closed.Load() || agent.State() != AgentStateError || agent.Health().Renderless.Healthy {
		t.Fatalf("err=%v closed=%v state=%s health=%+v", err, runtime.closed.Load(), agent.State(), agent.Health())
	}
}

func TestAgentStartRejectsNilRuntime(t *testing.T) {
	agent, _ := newContractAgent(t, AgentConfig{}, nil, errorDispatcher{err: errors.New("unused")})
	err := agent.Start(context.Background())
	if taskErrorCode(err) != TaskErrorExecutionFailed || agent.State() != AgentStateError {
		t.Fatalf("err=%v state=%s", err, agent.State())
	}
}

func TestAgentStartRollsBackWhenFactoryIgnoresCancellation(t *testing.T) {
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: errors.New("unused")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := agent.Start(ctx)
	if taskErrorCode(err) != TaskErrorCancelled || !runtime.closed.Load() || agent.State() != AgentStateError {
		t.Fatalf("err=%v closed=%v state=%s", err, runtime.closed.Load(), agent.State())
	}
}

func TestAgentCanRecoverFromFailedStart(t *testing.T) {
	factory := &retryFactory{partial: &contractRuntime{}, healthy: &contractRuntime{}}
	agent, err := NewAgentWithDependencies(AgentConfig{}, Dependencies{
		RuntimeFactory: factory, Dispatcher: errorDispatcher{err: errors.New("unused")},
		SessionStore: newMemorySessionStore(), Telemetry: &contractTelemetry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err == nil || !factory.partial.closed.Load() {
		t.Fatalf("first start=%v partialClosed=%v", err, factory.partial.closed.Load())
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("recovery start = %v", err)
	}
	if !agent.Health().Renderless.Healthy {
		t.Fatalf("health = %+v", agent.Health())
	}
	if err := agent.Stop(); err != nil || !factory.healthy.closed.Load() {
		t.Fatalf("stop=%v healthyClosed=%v", err, factory.healthy.closed.Load())
	}
}

func TestAgentLifecycleTransitionsAndIdempotentStop(t *testing.T) {
	runtime := &contractRuntime{}
	agent, telemetry := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: errors.New("unused")})
	var invalidContext context.Context
	if err := agent.Start(invalidContext); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("nil start context = %v", err)
	}
	if err := agent.Stop(); taskErrorCode(err) != TaskErrorInvalidTransition {
		t.Fatalf("stop before start = %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); taskErrorCode(err) != TaskErrorInvalidTransition {
		t.Fatalf("duplicate start = %v", err)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := agent.Stop(); err != nil {
		t.Fatalf("repeated stop = %v", err)
	}
	if err := agent.Start(context.Background()); taskErrorCode(err) != TaskErrorInvalidTransition {
		t.Fatalf("restart after stop = %v", err)
	}
	if _, err := agent.CreateSession("after-stop"); taskErrorCode(err) != TaskErrorInvalidTransition {
		t.Fatalf("session after stop = %v", err)
	}
	result := agent.ExecuteTask(context.Background(), Task{ID: "after-stop", SessionID: "closed", Action: FetchAction{URL: "https://example.com"}})
	if result.ErrorCode != TaskErrorInvalidTransition {
		t.Fatalf("execution after stop = %+v", result)
	}
	if !runtime.closed.Load() || telemetry.count(AgentEventStarted) != 1 || telemetry.count(AgentEventStopped) != 1 {
		t.Fatalf("closed=%v events=%+v", runtime.closed.Load(), telemetry.events)
	}
}

func TestAgentRejectsNilExecutionContext(t *testing.T) {
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: errors.New("must not execute")})
	session := startSession(t, agent)
	var invalidContext context.Context
	result := agent.ExecuteTask(invalidContext, Task{
		ID: "nil-context", SessionID: session.SessionID(), Action: FetchAction{URL: "https://example.com"},
	})
	if result.ErrorCode != TaskErrorInvalidInput {
		t.Fatalf("result = %+v", result)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentStopReportsCloseFailureAfterReleasingOwnership(t *testing.T) {
	runtime := &contractRuntime{closeErr: errors.New("close failed")}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: errors.New("unused")})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := agent.Stop()
	if taskErrorCode(err) != TaskErrorExecutionFailed || agent.State() != AgentStateStopped || agent.Health().Renderless.Healthy {
		t.Fatalf("err=%v state=%s health=%+v", err, agent.State(), agent.Health())
	}
}

func TestAgentStopCancelsAndWaitsForExecution(t *testing.T) {
	started := make(chan struct{})
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, blockingDispatcher{started: started})
	session := startSession(t, agent)
	resultCh := make(chan TaskResult, 1)
	go func() {
		resultCh <- agent.ExecuteTask(context.Background(), Task{
			ID: "blocked", SessionID: session.SessionID(), Action: FetchAction{URL: "https://example.com"},
		})
	}()
	<-started
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
	result := <-resultCh
	if result.ErrorCode != TaskErrorCancelled || !runtime.closed.Load() {
		t.Fatalf("result=%+v closed=%v", result, runtime.closed.Load())
	}
}

func TestSessionCloseCancelsAndWaitsForOwnedExecution(t *testing.T) {
	started := make(chan struct{})
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, blockingDispatcher{started: started})
	session := startSession(t, agent)
	resultCh := make(chan TaskResult, 1)
	go func() {
		resultCh <- agent.ExecuteTask(context.Background(), Task{
			ID: "session-blocked", SessionID: session.SessionID(), Action: FetchAction{URL: "https://example.com"},
		})
	}()
	<-started
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	result := <-resultCh
	if result.ErrorCode != TaskErrorCancelled || runtime.closed.Load() || !agent.IsStarted() {
		t.Fatalf("result=%+v runtimeClosed=%v state=%s", result, runtime.closed.Load(), agent.State())
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentTimeoutAndStableInjectedErrorClasses(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		runtime := &contractRuntime{}
		agent, _ := newContractAgent(t, AgentConfig{}, runtime, blockingDispatcher{started: make(chan struct{})})
		session := startSession(t, agent)
		result := agent.ExecuteTask(context.Background(), Task{
			ID: "timeout", SessionID: session.SessionID(), Timeout: time.Millisecond,
			Action: FetchAction{URL: "https://example.com"},
		})
		if result.ErrorCode != TaskErrorTimeout {
			t.Fatalf("result = %+v", result)
		}
		if err := agent.Stop(); err != nil {
			t.Fatal(err)
		}
	})

	for _, code := range []TaskErrorCode{TaskErrorPolicyDenied, TaskErrorBrowserCrash, TaskErrorStaleReference, TaskErrorExecutionFailed} {
		t.Run(string(code), func(t *testing.T) {
			runtime := &contractRuntime{}
			agent, _ := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: newTaskError(code, "probe", errors.New("probe"))})
			session := startSession(t, agent)
			result := agent.ExecuteTask(context.Background(), Task{
				ID: "typed", SessionID: session.SessionID(), Action: FetchAction{URL: "https://example.com"},
			})
			if result.ErrorCode != code {
				t.Fatalf("result = %+v", result)
			}
			if err := agent.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentRejectsNegativeTaskTimeout(t *testing.T) {
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: errors.New("must not execute")})
	session := startSession(t, agent)
	result := agent.ExecuteTask(context.Background(), Task{
		ID: "negative-timeout", SessionID: session.SessionID(), Timeout: -time.Second,
		Action: FetchAction{URL: "https://example.com"},
	})
	if result.ErrorCode != TaskErrorInvalidInput {
		t.Fatalf("result = %+v", result)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentAppliesConfiguredOperationTimeouts(t *testing.T) {
	tests := []struct {
		name   string
		config AgentConfig
		action FetchAction
	}{
		{name: "fetch", config: AgentConfig{FetchTimeout: time.Millisecond}, action: FetchAction{URL: "https://example.com"}},
		{name: "scripts", config: AgentConfig{ScriptTimeout: time.Millisecond}, action: FetchAction{URL: "https://example.com", RunScripts: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := &contractRuntime{}
			agent, _ := newContractAgent(t, test.config, runtime, blockingDispatcher{started: make(chan struct{})})
			session := startSession(t, agent)
			result := agent.ExecuteTask(context.Background(), Task{
				ID: test.name, SessionID: session.SessionID(), Action: test.action,
			})
			if result.ErrorCode != TaskErrorTimeout {
				t.Fatalf("result = %+v", result)
			}
			if err := agent.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentRejectsUnhealthyOwnedRuntime(t *testing.T) {
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{}, runtime, errorDispatcher{err: errors.New("must not execute")})
	session := startSession(t, agent)
	runtime.closed.Store(true)
	result := agent.ExecuteTask(context.Background(), Task{
		ID: "crashed", SessionID: session.SessionID(), Action: FetchAction{URL: "https://example.com"},
	})
	if result.ErrorCode != TaskErrorBrowserCrash || agent.Health().Renderless.Healthy {
		t.Fatalf("result=%+v health=%+v", result, agent.Health())
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRejectsDispatcherSuccessWithoutEvidence(t *testing.T) {
	for _, result := range []*PageResult{nil, {}} {
		runtime := &contractRuntime{}
		agent, _ := newContractAgent(t, AgentConfig{}, runtime, resultDispatcher{result: result})
		session := startSession(t, agent)
		got := agent.ExecuteTask(context.Background(), Task{
			ID: "no-evidence", SessionID: session.SessionID(), Action: FetchAction{URL: "https://example.com"},
		})
		if got.Success || got.ErrorCode != TaskErrorExecutionFailed {
			t.Fatalf("result = %+v", got)
		}
		if err := agent.Stop(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentSessionOwnershipLimitLookupAndClose(t *testing.T) {
	runtime := &contractRuntime{}
	agent, telemetry := newContractAgent(t, AgentConfig{MaxSessions: 2}, runtime, errorDispatcher{err: errors.New("unused")})
	if _, err := agent.CreateSession("owner"); taskErrorCode(err) != TaskErrorInvalidTransition {
		t.Fatalf("session before start = %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.CreateSession(""); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("empty owner = %v", err)
	}
	first, err := agent.CreateSession("first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := agent.CreateSession("second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.CreateSession("third"); taskErrorCode(err) != TaskErrorSessionLimit {
		t.Fatalf("limit error = %v", err)
	}
	if got, ok := agent.Session(first.SessionID()); !ok || got != first {
		t.Fatalf("lookup = %v, %v", got, ok)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if telemetry.count(AgentEventSessionClosed) != 1 {
		t.Fatalf("session close events = %d", telemetry.count(AgentEventSessionClosed))
	}
	if first.IsActive() {
		t.Fatal("closed session remained active")
	}
	if _, ok := agent.Session(first.SessionID()); ok {
		t.Fatal("closed session remained registered")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
	if telemetry.count(AgentEventSessionClosed) != 2 {
		t.Fatalf("session close events after stop = %d", telemetry.count(AgentEventSessionClosed))
	}
	if second.IsActive() || agent.Health().Sessions != 0 {
		t.Fatalf("second active=%v health=%+v", second.IsActive(), agent.Health())
	}
}

func TestSessionTabLimitAndClosedReference(t *testing.T) {
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{MaxTabs: 1}, runtime, errorDispatcher{err: errors.New("unused")})
	session := startSession(t, agent)
	if err := session.AddTab(); err != nil {
		t.Fatal(err)
	}
	if err := session.AddTab(); taskErrorCode(err) != TaskErrorSessionLimit {
		t.Fatalf("tab limit = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.AddTab(); taskErrorCode(err) != TaskErrorStaleReference {
		t.Fatalf("closed add = %v", err)
	}
	if err := session.RemoveTab(); taskErrorCode(err) != TaskErrorStaleReference {
		t.Fatalf("closed remove = %v", err)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentConcurrentSessionCreationIsBoundedAndUnique(t *testing.T) {
	const limit = 16
	runtime := &contractRuntime{}
	agent, _ := newContractAgent(t, AgentConfig{MaxSessions: limit}, runtime, errorDispatcher{err: errors.New("unused")})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, limit*2)
	for i := 0; i < limit*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if session, err := agent.CreateSession("concurrent"); err == nil {
				ids <- session.SessionID()
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]struct{})
	for id := range ids {
		seen[id] = struct{}{}
	}
	if len(seen) != limit || agent.Health().Sessions != limit {
		t.Fatalf("unique=%d sessions=%d", len(seen), agent.Health().Sessions)
	}
	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRejectsInvalidConstructionAndPublicMapParameters(t *testing.T) {
	if _, err := NewAgent(AgentConfig{MaxSessions: -1}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("invalid production config = %v", err)
	}
	if _, err := NewAgent(AgentConfig{UserAgent: "Artemis\r\nInjected: true"}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("invalid user agent = %v", err)
	}
	if _, err := NewAgentWithDependencies(AgentConfig{MaxTabs: -1}, Dependencies{}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("invalid config = %v", err)
	}
	if _, err := NewAgentWithDependencies(AgentConfig{}, Dependencies{}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("missing dependencies = %v", err)
	}
	var nilFactory *contractFactory
	if _, err := NewAgentWithDependencies(AgentConfig{}, Dependencies{
		RuntimeFactory: nilFactory, Dispatcher: renderlessDispatcher{},
		SessionStore: newMemorySessionStore(), Telemetry: discardTelemetry{},
	}); taskErrorCode(err) != TaskErrorInvalidInput {
		t.Fatalf("typed nil dependency = %v", err)
	}
	taskType := reflect.TypeOf(Task{})
	for i := 0; i < taskType.NumField(); i++ {
		if taskType.Field(i).Type.Kind() == reflect.Map {
			t.Fatalf("public Task field %s accepts a map", taskType.Field(i).Name)
		}
	}
}

func taskErrorCode(err error) TaskErrorCode {
	var taskErr *TaskError
	if errors.As(err, &taskErr) {
		return taskErr.Code
	}
	return ""
}

type fakeProfileRuntime struct {
	openID    profile.SessionID
	openOwner string
	closeErr  error
	closed    atomic.Int64
}

func (f *fakeProfileRuntime) Open(_ context.Context, req profile.OpenSessionRequest) (*profile.RuntimeSession, error) {
	return &profile.RuntimeSession{ID: f.openID, OwnerUserRef: req.OwnerUserRef, ProfileID: req.ProfileID, Class: req.Class}, nil
}

func (f *fakeProfileRuntime) Close(ctx context.Context, id profile.SessionID, owner string) error {
	f.closed.Add(1)
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("profile close received unbounded context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.closeErr != nil {
		return f.closeErr
	}
	if string(id) != string(f.openID) || owner != f.openOwner {
		return errors.New("profile close received wrong id or owner")
	}
	return nil
}

type admissionProfileRuntime struct {
	openID       profile.SessionID
	openStarted  chan struct{}
	openRelease  chan struct{}
	openOnce     sync.Once
	openCalls    atomic.Int64
	closeCalls   atomic.Int64
	closeErr     error
	mu           sync.Mutex
	closedIDs    []profile.SessionID
	closedOwners []string
}

func (r *admissionProfileRuntime) Open(_ context.Context, req profile.OpenSessionRequest) (*profile.RuntimeSession, error) {
	r.openCalls.Add(1)
	if r.openStarted != nil {
		r.openOnce.Do(func() { close(r.openStarted) })
	}
	if r.openRelease != nil {
		<-r.openRelease
	}
	return &profile.RuntimeSession{ID: r.openID, ProfileID: req.ProfileID, OwnerUserRef: req.OwnerUserRef, Class: req.Class}, nil
}

func (r *admissionProfileRuntime) Close(ctx context.Context, id profile.SessionID, owner string) error {
	r.closeCalls.Add(1)
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("profile close received unbounded context")
	}
	r.mu.Lock()
	r.closedIDs = append(r.closedIDs, id)
	r.closedOwners = append(r.closedOwners, owner)
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.closeErr
}

func (r *admissionProfileRuntime) closeArguments() ([]profile.SessionID, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]profile.SessionID(nil), r.closedIDs...), append([]string(nil), r.closedOwners...)
}

func startAdmissionAgent(t *testing.T, config AgentConfig, runtime profileRuntime) (*Agent, *contractTelemetry) {
	t.Helper()
	agent, telemetry := newContractAgent(t, config, &contractRuntime{}, errorDispatcher{err: errors.New("unused")})
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	agent.profileRuntime = runtime
	return agent, telemetry
}

func TestCreateSessionForProfileRejectsOpenAfterStop(t *testing.T) {
	runtime := &admissionProfileRuntime{
		openID: "late-session", openStarted: make(chan struct{}), openRelease: make(chan struct{}),
	}
	agent, telemetry := startAdmissionAgent(t, AgentConfig{MaxSessions: 1}, runtime)
	createResult := make(chan struct {
		session *Session
		err     error
	}, 1)
	go func() {
		session, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
			ProfileID: "profile-late", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral,
		})
		createResult <- struct {
			session *Session
			err     error
		}{session: session, err: err}
	}()
	<-runtime.openStarted

	stopResult := make(chan error, 1)
	go func() { stopResult <- agent.Stop() }()
	select {
	case err := <-stopResult:
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(runtime.openRelease)
		t.Fatal("stop waited for blocked profile open")
	}
	close(runtime.openRelease)

	result := <-createResult
	if result.session != nil || taskErrorCode(result.err) != TaskErrorInvalidTransition {
		t.Fatalf("late create = session=%v err=%v", result.session, result.err)
	}
	if agent.State() != AgentStateStopped || len(agent.ListSessions()) != 0 {
		t.Fatalf("agent state=%s sessions=%d", agent.State(), len(agent.ListSessions()))
	}
	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("late profile close calls = %d", runtime.closeCalls.Load())
	}
	closedIDs, closedOwners := runtime.closeArguments()
	if len(closedIDs) != 1 || closedIDs[0] != runtime.openID || len(closedOwners) != 1 || closedOwners[0] != "owner-1" {
		t.Fatalf("close arguments = ids=%v owners=%v", closedIDs, closedOwners)
	}
	if telemetry.count(AgentEventSessionCreated) != 0 {
		t.Fatalf("false session.created events = %d", telemetry.count(AgentEventSessionCreated))
	}
}

func TestCreateSessionForProfileReservesCapacityDuringOpen(t *testing.T) {
	runtime := &admissionProfileRuntime{
		openID: "reserved-session", openStarted: make(chan struct{}), openRelease: make(chan struct{}),
	}
	agent, _ := startAdmissionAgent(t, AgentConfig{MaxSessions: 1}, runtime)
	defer stopContractAgent(t, agent)
	firstResult := make(chan error, 1)
	go func() {
		_, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
			ProfileID: "profile-one", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral,
		})
		firstResult <- err
	}()
	<-runtime.openStarted

	_, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
		ProfileID: "profile-two", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral,
	})
	if taskErrorCode(err) != TaskErrorSessionLimit {
		t.Fatalf("reserved capacity error = %v", err)
	}
	if runtime.openCalls.Load() != 1 {
		t.Fatalf("open calls while capacity reserved = %d", runtime.openCalls.Load())
	}
	close(runtime.openRelease)
	if err := <-firstResult; err != nil {
		t.Fatalf("first create: %v", err)
	}
	if len(agent.ListSessions()) != 1 {
		t.Fatalf("sessions after reserved open = %d", len(agent.ListSessions()))
	}
}

func TestCreateSessionForProfileDuplicateRollsBackExactlyOnce(t *testing.T) {
	runtime := &admissionProfileRuntime{openID: "duplicate-session"}
	agent, telemetry := startAdmissionAgent(t, AgentConfig{MaxSessions: 2}, runtime)
	defer stopContractAgent(t, agent)
	request := profile.OpenSessionRequest{ProfileID: "profile-one", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral}
	if _, err := agent.CreateSessionForProfile(context.Background(), request); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
		ProfileID: "profile-two", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral,
	})
	if taskErrorCode(err) != TaskErrorExecutionFailed || !strings.Contains(err.Error(), `duplicate session "duplicate-session"`) {
		t.Fatalf("duplicate create = %v", err)
	}
	if len(agent.ListSessions()) != 1 || telemetry.count(AgentEventSessionCreated) != 1 {
		t.Fatalf("duplicate publication state: sessions=%d created=%d", len(agent.ListSessions()), telemetry.count(AgentEventSessionCreated))
	}
	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("duplicate rollback close calls = %d", runtime.closeCalls.Load())
	}
}

func TestCreateSessionForProfilePreservesLifecycleErrorWhenRollbackFails(t *testing.T) {
	runtime := &admissionProfileRuntime{
		openID: "failed-cleanup", openStarted: make(chan struct{}), openRelease: make(chan struct{}),
		closeErr: errors.New("profile cleanup failed"),
	}
	agent, _ := startAdmissionAgent(t, AgentConfig{MaxSessions: 1}, runtime)
	createResult := make(chan error, 1)
	go func() {
		_, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
			ProfileID: "profile-failed-cleanup", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral,
		})
		createResult <- err
	}()
	<-runtime.openStarted
	if err := agent.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	close(runtime.openRelease)
	err := <-createResult
	if taskErrorCode(err) != TaskErrorInvalidTransition || !strings.Contains(err.Error(), "profile cleanup failed") {
		t.Fatalf("lifecycle cleanup error = %v", err)
	}
	if runtime.closeCalls.Load() != 1 || len(agent.ListSessions()) != 0 {
		t.Fatalf("cleanup state: closes=%d sessions=%d", runtime.closeCalls.Load(), len(agent.ListSessions()))
	}
}

func TestCreateSessionForProfileCallerCancellationReleasesAdmission(t *testing.T) {
	runtime := &admissionProfileRuntime{
		openID: "cancelled-session", openStarted: make(chan struct{}), openRelease: make(chan struct{}),
	}
	agent, telemetry := startAdmissionAgent(t, AgentConfig{MaxSessions: 1}, runtime)
	defer stopContractAgent(t, agent)
	ctx, cancel := context.WithCancel(context.Background())
	createResult := make(chan error, 1)
	go func() {
		_, err := agent.CreateSessionForProfile(ctx, profile.OpenSessionRequest{
			ProfileID: "profile-cancelled", OwnerUserRef: "owner-1", Class: profile.ProfileEphemeral,
		})
		createResult <- err
	}()
	<-runtime.openStarted
	cancel()
	close(runtime.openRelease)
	if err := <-createResult; taskErrorCode(err) != TaskErrorCancelled {
		t.Fatalf("cancelled create = %v", err)
	}
	checkProfileSessionOpenings(t, agent, 0)
	if runtime.closeCalls.Load() != 1 || telemetry.count(AgentEventSessionCreated) != 0 || len(agent.ListSessions()) != 0 {
		t.Fatalf("cancelled publication state: closes=%d created=%d sessions=%d", runtime.closeCalls.Load(), telemetry.count(AgentEventSessionCreated), len(agent.ListSessions()))
	}
}

func checkProfileSessionOpenings(t *testing.T, agent *Agent, want int) {
	t.Helper()
	agent.mu.RLock()
	defer agent.mu.RUnlock()
	if agent.profileSessionOpenings != want {
		t.Fatalf("profile openings = %d, want %d", agent.profileSessionOpenings, want)
	}
}

func startProfileAgent(t *testing.T, cfg AgentConfig) (*Agent, *fakeProfileRuntime) {
	t.Helper()
	agent, err := NewAgentWithDependencies(cfg, Dependencies{
		RuntimeFactory: contractFactory{runtime: &contractRuntime{}},
		Dispatcher:     renderlessDispatcher{},
		SessionStore:   newMemorySessionStore(),
		Telemetry:      &contractTelemetry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProfileRuntime{openID: "profile-session-1", openOwner: "owner-1"}
	agent.profileRuntime = fake
	return agent, fake
}

func TestProfileCloseUsesBoundedContext(t *testing.T) {
	agent, fake := startProfileAgent(t, AgentConfig{})
	defer stopContractAgent(t, agent)

	session, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
		ProfileID:    "profile-1",
		OwnerUserRef: fake.openOwner,
		Class:        profile.ProfileEphemeral,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := agent.CloseSession(session.id); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if fake.closed.Load() != 1 {
		t.Fatalf("profile close not called, got %d calls", fake.closed.Load())
	}
}

func TestCreateSessionRollbackReportsCloseError(t *testing.T) {
	agent, fake := startProfileAgent(t, AgentConfig{MaxSessions: 1})

	if _, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
		ProfileID:    "profile-1",
		OwnerUserRef: fake.openOwner,
		Class:        profile.ProfileEphemeral,
	}); err != nil {
		t.Fatalf("first profile session: %v", err)
	}
	fake.closeErr = errors.New("profile close failed")
	_, err := agent.CreateSessionForProfile(context.Background(), profile.OpenSessionRequest{
		ProfileID:    "profile-2",
		OwnerUserRef: fake.openOwner,
		Class:        profile.ProfileEphemeral,
	})
	if err == nil {
		t.Fatal("expected error for second session over limit")
	}
	if !strings.Contains(err.Error(), "profile close failed") {
		t.Fatalf("rollback close error not surfaced: %v", err)
	}
	if !strings.Contains(err.Error(), "maximum 1 active sessions") {
		t.Fatalf("original store error not surfaced: %v", err)
	}
	if fake.closed.Load() != 1 {
		t.Fatalf("rollback close not called, got %d calls", fake.closed.Load())
	}
	if stopErr := agent.Stop(); stopErr == nil || !strings.Contains(stopErr.Error(), "profile close failed") {
		t.Fatalf("stop error = %v, want profile close failed", stopErr)
	}
}
