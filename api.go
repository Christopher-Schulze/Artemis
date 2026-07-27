package artemis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

// api.go is the public API for the artemis package.
//
// It provides the top-level Agent, Session, and Task types. The Agent routes
// typed fetches through the canonical hybrid contract; standalone Chromium
// operations use router.ChromiumExecutor with an owned CDP page.

// Agent is the top-level artemis browser automation agent
type Agent struct {
	mu              sync.RWMutex
	config          AgentConfig
	state           AgentState
	runtime         RenderlessRuntime
	factory         RuntimeFactory
	dispatcher      Dispatcher
	hybrid          *artemisrouter.HybridRouter
	sessions        SessionStore
	telemetry       Telemetry
	runCtx          context.Context
	cancel          context.CancelFunc
	operations      sync.WaitGroup
	sessionSeq      uint64
	chromiumActions ChromiumActions
	profileRuntime  profileRuntime
}

// AgentConfig configures the artemis agent
type AgentConfig struct {
	MaxSessions   int                  `json:"maxSessions"`
	MaxTabs       int                  `json:"maxTabs"`
	ScriptTimeout time.Duration        `json:"scriptTimeout"`
	FetchTimeout  time.Duration        `json:"fetchTimeout"`
	UserAgent     string               `json:"userAgent"`
	ObeyRobots    bool                 `json:"obeyRobots"`
	PolicyConfig  network.PolicyConfig `json:"policyConfig"`
	Diagnostics   diagnostics.Config   `json:"diagnostics"`
}

// AgentState enumerates agent lifecycle states
type AgentState string

const (
	AgentStateCreated  AgentState = "created"
	AgentStateStarting AgentState = "starting"
	AgentStateRunning  AgentState = "running"
	AgentStateStopping AgentState = "stopping"
	AgentStateStopped  AgentState = "stopped"
	AgentStateError    AgentState = "error"
)

// Session represents an active browser automation session
type Session struct {
	mu         sync.RWMutex
	owner      *Agent
	ctx        context.Context
	cancel     context.CancelFunc
	operations sync.WaitGroup
	id         string
	userID     string
	createdAt  time.Time
	tabs       int
	active     bool
	pages      map[string]*engine.Page
	pageSeq    uint64
	managed    bool
	profileID  string
}

// Task represents a browser automation task
type Task struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionId"`
	Action    Action        `json:"-"`
	Timeout   time.Duration `json:"timeout"`
}

// TaskResult is the result of a task execution
type TaskResult struct {
	TaskID    string        `json:"taskId"`
	Success   bool          `json:"success"`
	Data      *PageResult   `json:"data,omitempty"`
	ErrorCode TaskErrorCode `json:"errorCode,omitempty"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// TaskErrorCode is a stable machine-readable task failure category.
type TaskErrorCode string

const (
	TaskErrorInvalidInput          TaskErrorCode = "invalid_input"
	TaskErrorCapabilityUnavailable TaskErrorCode = "capability_unavailable"
	TaskErrorPolicyDenied          TaskErrorCode = "policy_denied"
	TaskErrorTimeout               TaskErrorCode = "timeout"
	TaskErrorCancelled             TaskErrorCode = "cancelled"
	TaskErrorBrowserCrash          TaskErrorCode = "browser_crash"
	TaskErrorStaleReference        TaskErrorCode = "stale_reference"
	TaskErrorExecutionFailed       TaskErrorCode = "execution_failed"
	TaskErrorInvalidTransition     TaskErrorCode = "invalid_transition"
	TaskErrorSessionNotFound       TaskErrorCode = "session_not_found"
	TaskErrorSessionLimit          TaskErrorCode = "session_limit"
	TaskErrorResourceLimit         TaskErrorCode = "resource_limit"
	TaskErrorPageNotFound          TaskErrorCode = "page_not_found"
)

// NewAgent validates config and creates an Agent with production dependencies.
func NewAgent(config AgentConfig) (*Agent, error) {
	return NewAgentWithDependencies(config, Dependencies{
		RuntimeFactory: engineRuntimeFactory{},
		Dispatcher:     renderlessDispatcher{},
		SessionStore:   newMemorySessionStore(),
		Telemetry:      discardTelemetry{},
	})
}

// Start starts the agent
func (a *Agent) Start(ctx context.Context) error {
	if ctx == nil {
		return newTaskError(TaskErrorInvalidInput, "start", fmt.Errorf("nil context"))
	}
	factory, config, err := a.beginStart()
	if err != nil {
		return err
	}
	runtime, err := factory.Start(ctx, config)
	if err != nil {
		return a.failStart(runtime, err)
	}
	if runtime == nil {
		return a.failStart(nil, fmt.Errorf("runtime factory returned nil"))
	}
	if err := ctx.Err(); err != nil {
		return a.failStart(runtime, err)
	}
	return a.finishStart(runtime)
}

func (a *Agent) beginStart() (RuntimeFactory, AgentConfig, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != AgentStateCreated && a.state != AgentStateError {
		return nil, AgentConfig{}, newTaskError(TaskErrorInvalidTransition, "start", fmt.Errorf("state %s", a.state))
	}
	a.state = AgentStateStarting
	return a.factory, a.config, nil
}

func (a *Agent) failStart(runtime RenderlessRuntime, err error) error {
	if runtime != nil {
		if closeErr := runtime.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback close: %w", closeErr))
		}
	}
	a.mu.Lock()
	a.state = AgentStateError
	a.mu.Unlock()
	return classifyTaskError("start", err)
}

func (a *Agent) finishStart(runtime RenderlessRuntime) error {
	runCtx, cancel := context.WithCancel(context.Background())
	hybrid, hybridErr := a.buildHybridRouter(runtime)
	if hybridErr != nil {
		cancel()
		_ = runtime.Close()
		a.mu.Lock()
		a.state = AgentStateError
		a.mu.Unlock()
		return newTaskError(TaskErrorExecutionFailed, "start", hybridErr)
	}
	a.mu.Lock()
	if a.state != AgentStateStarting {
		a.mu.Unlock()
		cancel()
		_ = runtime.Close()
		return newTaskError(TaskErrorInvalidTransition, "start", fmt.Errorf("state changed during startup"))
	}
	a.runtime = runtime
	a.hybrid = hybrid
	a.runCtx = runCtx
	a.cancel = cancel
	a.state = AgentStateRunning
	a.mu.Unlock()
	a.telemetry.Record(AgentEvent{Type: AgentEventStarted, At: time.Now()})
	return nil
}

// Stop stops the agent
func (a *Agent) Stop() error {
	runtime, cancel, stopped, err := a.beginStop()
	if err != nil || stopped {
		return err
	}
	if cancel != nil {
		cancel()
	}
	a.operations.Wait()
	sessionErr := a.closeSessions()
	var closeErr error
	if runtime != nil {
		closeErr = runtime.Close()
	}
	closeErr = errors.Join(sessionErr, closeErr)
	a.finishStop()
	if closeErr != nil {
		return newTaskError(TaskErrorExecutionFailed, "stop", closeErr)
	}
	return nil
}

func (a *Agent) beginStop() (RenderlessRuntime, context.CancelFunc, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == AgentStateStopped {
		return nil, nil, true, nil
	}
	if a.state != AgentStateRunning {
		return nil, nil, false, newTaskError(TaskErrorInvalidTransition, "stop", fmt.Errorf("state %s", a.state))
	}
	a.state = AgentStateStopping
	return a.runtime, a.cancel, false, nil
}

func (a *Agent) closeSessions() error {
	var closeErr error
	for _, session := range a.sessions.List() {
		if session == nil {
			continue
		}
		closeErr = errors.Join(closeErr, a.CloseSession(session.SessionID()))
	}
	return closeErr
}

func (a *Agent) finishStop() {
	a.mu.Lock()
	a.runtime = nil
	a.hybrid = nil
	a.runCtx = nil
	a.cancel = nil
	a.state = AgentStateStopped
	a.mu.Unlock()
	a.telemetry.Record(AgentEvent{Type: AgentEventStopped, At: time.Now()})
}

// IsStarted reports whether the agent is started
func (a *Agent) IsStarted() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state == AgentStateRunning
}

// State returns the agent state
func (a *Agent) State() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// Config returns the agent config
func (a *Agent) Config() AgentConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// Diagnostics returns the runtime's redacted retention-bounded audit records.
func (a *Agent) Diagnostics() ([]diagnostics.Record, error) {
	a.mu.RLock()
	runtime := a.runtime
	state := a.state
	a.mu.RUnlock()
	provider, ok := runtime.(interface {
		Diagnostics() ([]diagnostics.Record, error)
	})
	if state != AgentStateRunning || !ok {
		return nil, newTaskError(TaskErrorInvalidTransition, "diagnostics", fmt.Errorf("state %s", state))
	}
	return provider.Diagnostics()
}

// CreateSession creates a new browser session
func (a *Agent) CreateSession(userID string) (*Session, error) {
	a.mu.Lock()
	state := a.state
	maxSessions := a.config.MaxSessions
	if state != AgentStateRunning {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorInvalidTransition, "create_session", fmt.Errorf("state %s", state))
	}
	if userID == "" {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorInvalidInput, "create_session", fmt.Errorf("empty user ID"))
	}
	now := time.Now()
	a.sessionSeq++
	sessionCtx, sessionCancel := context.WithCancel(a.runCtx)
	session := &Session{
		owner:     a,
		ctx:       sessionCtx,
		cancel:    sessionCancel,
		id:        fmt.Sprintf("session-%d-%d", now.UnixNano(), a.sessionSeq),
		userID:    userID,
		createdAt: now,
		active:    true,
		pages:     make(map[string]*engine.Page),
	}
	if err := a.sessions.PutIfBelow(session, maxSessions); err != nil {
		a.mu.Unlock()
		return nil, classifyTaskError("create_session", err)
	}
	a.mu.Unlock()
	a.telemetry.Record(AgentEvent{Type: AgentEventSessionCreated, SessionID: session.id, At: now})
	return session, nil
}

// ExecuteTask executes a browser automation task
func (a *Agent) ExecuteTask(ctx context.Context, task Task) TaskResult {
	start := time.Now()
	if ctx == nil {
		return failedTaskResult(task.ID, start, newTaskError(TaskErrorInvalidInput, "execute", fmt.Errorf("nil context")))
	}
	lease, err := a.beginExecution()
	if err != nil {
		return failedTaskResult(task.ID, start, err)
	}
	defer a.operations.Done()
	if err := validateTask(task); err != nil {
		return failedTaskResult(task.ID, start, err)
	}
	sessionCtx, done, err := a.beginSessionExecution(task.SessionID)
	if err != nil {
		return failedTaskResult(task.ID, start, err)
	}
	defer done()
	execCtx, cancel := executionContext(ctx, lease.runCtx, sessionCtx, task, lease.config)
	defer cancel()
	data, dispatchErr := executeTaskThroughRouter(execCtx, lease, task)
	if dispatchErr != nil {
		return failedTaskResult(task.ID, start, classifyTaskError("execute", dispatchErr))
	}
	if err := validatePageResult(data); err != nil {
		return failedTaskResult(task.ID, start, err)
	}
	a.telemetry.Record(AgentEvent{Type: AgentEventTaskCompleted, SessionID: task.SessionID, TaskID: task.ID, At: time.Now()})
	return TaskResult{TaskID: task.ID, Success: true, Data: data, Duration: time.Since(start)}
}

func validatePageResult(result *PageResult) *TaskError {
	if result == nil {
		return newTaskError(TaskErrorExecutionFailed, "execute", fmt.Errorf("dispatcher returned no result"))
	}
	if result.URL == "" || result.StatusCode < 100 || result.StatusCode > 599 || result.HTML == "" {
		return newTaskError(TaskErrorExecutionFailed, "execute", fmt.Errorf("dispatcher returned incomplete page evidence"))
	}
	return nil
}

type executionLease struct {
	runtime    RenderlessRuntime
	dispatcher Dispatcher
	hybrid     *artemisrouter.HybridRouter
	runCtx     context.Context
	config     AgentConfig
}

func executeTaskThroughRouter(ctx context.Context, lease executionLease, task Task) (*PageResult, error) {
	if lease.hybrid == nil {
		return lease.dispatcher.Execute(ctx, lease.runtime, task)
	}
	var action FetchAction
	switch value := task.Action.(type) {
	case FetchAction:
		action = value
	case *FetchAction:
		if value == nil {
			return nil, newTaskError(TaskErrorInvalidInput, "dispatch", fmt.Errorf("nil fetch action"))
		}
		action = *value
	default:
		return lease.dispatcher.Execute(ctx, lease.runtime, task)
	}
	signals := artemisrouter.Signals{IsHTML: true}
	if action.RunScripts {
		signals.ScriptCount = 1
	}
	result, err := lease.hybrid.Execute(ctx, artemisrouter.RouteRequest{
		URL: action.URL, Action: artemisrouter.ActionFetch, Signals: signals,
		State:   artemisrouter.BrowserState{SessionID: task.SessionID},
		TraceID: task.ID, EvidenceID: task.ID,
	})
	if err != nil {
		return nil, routeErrorToTaskError(err)
	}
	defer result.Close()
	return &PageResult{
		URL: result.Output.URL, StatusCode: result.Output.StatusCode, Title: result.Output.Title,
		HTML: result.Output.HTML, Text: result.Output.Text, Markdown: result.Output.Markdown,
		Links: result.Output.Links,
	}, nil
}

func routeErrorToTaskError(err error) *TaskError {
	var routeErr *artemisrouter.RouteError
	if !errors.As(err, &routeErr) || routeErr == nil {
		return classifyTaskError("route", err)
	}
	code := TaskErrorExecutionFailed
	switch routeErr.Code {
	case artemisrouter.ErrorInvalidInput:
		code = TaskErrorInvalidInput
	case artemisrouter.ErrorPolicyDenied:
		code = TaskErrorPolicyDenied
	case artemisrouter.ErrorUnavailable:
		code = TaskErrorCapabilityUnavailable
	case artemisrouter.ErrorCancelled:
		code = TaskErrorCancelled
	case artemisrouter.ErrorResourceBudget:
		code = TaskErrorResourceLimit
	}
	return newTaskError(code, "route", err)
}

func (a *Agent) beginExecution() (executionLease, *TaskError) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != AgentStateRunning || a.runtime == nil {
		return executionLease{}, newTaskError(TaskErrorInvalidTransition, "execute", fmt.Errorf("state %s", a.state))
	}
	if !a.runtime.Healthy() {
		return executionLease{}, newTaskError(TaskErrorBrowserCrash, "execute", fmt.Errorf("renderless runtime is unhealthy"))
	}
	a.operations.Add(1)
	return executionLease{runtime: a.runtime, dispatcher: a.dispatcher, hybrid: a.hybrid, runCtx: a.runCtx, config: a.config}, nil
}

func (a *Agent) buildHybridRouter(runtime RenderlessRuntime) (*artemisrouter.HybridRouter, error) {
	if _, ok := a.dispatcher.(renderlessDispatcher); !ok {
		return nil, nil
	}
	executor := artemisrouter.RuntimeExecutor{Runtime: runtime}
	return artemisrouter.New(artemisrouter.Config{Executors: map[artemisrouter.Mode]artemisrouter.Executor{
		artemisrouter.ModeStaticFetch:  executor,
		artemisrouter.ModeRenderlessJS: executor,
	}})
}

func validateTask(task Task) *TaskError {
	if task.ID == "" || task.SessionID == "" || task.Action == nil {
		return newTaskError(TaskErrorInvalidInput, "execute", fmt.Errorf("task ID, session ID, and action are required"))
	}
	if task.Timeout < 0 {
		return newTaskError(TaskErrorInvalidInput, "execute", fmt.Errorf("task timeout cannot be negative"))
	}
	return nil
}

func (a *Agent) beginSessionExecution(sessionID string) (context.Context, func(), *TaskError) {
	session, ok := a.sessions.Get(sessionID)
	if !ok || session == nil {
		return nil, nil, newTaskError(TaskErrorSessionNotFound, "execute", fmt.Errorf("session %q", sessionID))
	}
	sessionCtx, active := session.beginOperation()
	if !active {
		return nil, nil, newTaskError(TaskErrorSessionNotFound, "execute", fmt.Errorf("session %q", sessionID))
	}
	return sessionCtx, session.operations.Done, nil
}

func executionContext(parent, runCtx, sessionCtx context.Context, task Task, config AgentConfig) (context.Context, context.CancelFunc) {
	execCtx, cancel := context.WithCancel(parent)
	stopRun := context.AfterFunc(runCtx, cancel)
	stopSession := context.AfterFunc(sessionCtx, cancel)
	timeoutCtx, timeoutCancel := context.WithTimeout(execCtx, taskTimeout(task, config))
	return network.WithSessionID(timeoutCtx, task.SessionID), func() {
		timeoutCancel()
		stopSession()
		stopRun()
		cancel()
	}
}

func taskTimeout(task Task, config AgentConfig) time.Duration {
	if task.Timeout > 0 {
		return task.Timeout
	}
	if fetch, ok := task.Action.(FetchAction); ok && fetch.RunScripts {
		return config.ScriptTimeout
	}
	if fetch, ok := task.Action.(*FetchAction); ok && fetch != nil && fetch.RunScripts {
		return config.ScriptTimeout
	}
	return config.FetchTimeout
}

// ApplyDefaults applies default values to the agent config
func (c *AgentConfig) ApplyDefaults() {
	if c.MaxSessions <= 0 {
		c.MaxSessions = 64
	}
	if c.MaxTabs <= 0 {
		c.MaxTabs = 10
	}
	if c.ScriptTimeout <= 0 {
		c.ScriptTimeout = 30 * time.Second
	}
	if c.FetchTimeout <= 0 {
		c.FetchTimeout = 30 * time.Second
	}
	if c.UserAgent == "" {
		c.UserAgent = engine.DefaultUserAgent
	}
}

// IsValidAgentState reports whether an agent state is valid
func IsValidAgentState(s AgentState) bool {
	switch s {
	case AgentStateCreated, AgentStateStarting, AgentStateRunning, AgentStateStopping, AgentStateStopped, AgentStateError:
		return true
	}
	return false
}

// SessionID returns the session ID
func (s *Session) SessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// UserID returns the session user ID
func (s *Session) UserID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userID
}

// IsActive reports whether the session is active
func (s *Session) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// Close closes the session
func (s *Session) Close() error {
	if s.owner == nil {
		_, err := s.deactivate()
		return err
	}
	return s.owner.CloseSession(s.id)
}

// TabCount returns the number of tabs in the session
func (s *Session) TabCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tabs
}

// AddTab increments the tab count
func (s *Session) AddTab() *TaskError {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return newTaskError(TaskErrorStaleReference, "add_tab", fmt.Errorf("session %q is closed", s.id))
	}
	if s.owner != nil && s.tabs >= s.owner.Config().MaxTabs {
		return newTaskError(TaskErrorSessionLimit, "add_tab", fmt.Errorf("maximum %d tabs", s.owner.Config().MaxTabs))
	}
	s.tabs++
	return nil
}

// RemoveTab decrements the tab count
func (s *Session) RemoveTab() *TaskError {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return newTaskError(TaskErrorStaleReference, "remove_tab", fmt.Errorf("session %q is closed", s.id))
	}
	if s.tabs > 0 {
		s.tabs--
	}
	return nil
}

// String returns a diagnostic summary.
func (a *Agent) String() string {
	a.mu.RLock()
	state := a.state
	maxTabs := a.config.MaxTabs
	a.mu.RUnlock()
	return fmt.Sprintf("Agent{state:%s sessions:%d maxTabs:%d}", state, a.sessions.Len(), maxTabs)
}

// String returns a diagnostic summary.
func (s *Session) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("Session{id:%s user:%s tabs:%d active:%v}", s.id, s.userID, s.tabs, s.active)
}

// String returns a diagnostic summary.
func (t Task) String() string {
	kind := ActionKind("")
	if t.Action != nil {
		kind = t.Action.Kind()
	}
	return fmt.Sprintf("Task{id:%s session:%s action:%s}", t.ID, t.SessionID, kind)
}

// String returns a diagnostic summary.
func (r TaskResult) String() string {
	return fmt.Sprintf("TaskResult{task:%s success:%v duration:%v}", r.TaskID, r.Success, r.Duration)
}
