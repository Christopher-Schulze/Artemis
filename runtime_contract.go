package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
)

// ActionKind identifies a closed high-level Agent operation.
type ActionKind string

const ActionFetch ActionKind = "fetch"

// Action is implemented only by Artemis action values.
type Action interface {
	Kind() ActionKind
	artemisAction()
}

// FetchAction fetches one page through the owned renderless runtime.
type FetchAction struct {
	URL        string `json:"url"`
	RunScripts bool   `json:"runScripts"`
}

func (FetchAction) Kind() ActionKind { return ActionFetch }
func (FetchAction) artemisAction()   {}

type taskWire struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Timeout   time.Duration   `json:"timeout"`
	Action    json.RawMessage `json:"action"`
}

type actionWire struct {
	Type       ActionKind `json:"type"`
	URL        string     `json:"url,omitempty"`
	RunScripts bool       `json:"runScripts,omitempty"`
}

// MarshalJSON preserves the closed action discriminator on the wire.
func (t Task) MarshalJSON() ([]byte, error) {
	if t.Action == nil {
		return nil, newTaskError(TaskErrorInvalidInput, "marshal_task", fmt.Errorf("action is required"))
	}
	var action actionWire
	switch value := t.Action.(type) {
	case FetchAction:
		action = actionWire{Type: ActionFetch, URL: value.URL, RunScripts: value.RunScripts}
	case *FetchAction:
		if value == nil {
			return nil, newTaskError(TaskErrorInvalidInput, "marshal_task", fmt.Errorf("nil fetch action"))
		}
		action = actionWire{Type: ActionFetch, URL: value.URL, RunScripts: value.RunScripts}
	default:
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "marshal_task", fmt.Errorf("action %q", t.Action.Kind()))
	}
	actionJSON, err := json.Marshal(action)
	if err != nil {
		return nil, newTaskError(TaskErrorExecutionFailed, "marshal_task", err)
	}
	return json.Marshal(taskWire{ID: t.ID, SessionID: t.SessionID, Timeout: t.Timeout, Action: actionJSON})
}

// UnmarshalJSON validates the action discriminator before constructing Task.
func (t *Task) UnmarshalJSON(data []byte) error {
	var wire taskWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return newTaskError(TaskErrorInvalidInput, "unmarshal_task", err)
	}
	var action actionWire
	if err := json.Unmarshal(wire.Action, &action); err != nil {
		return newTaskError(TaskErrorInvalidInput, "unmarshal_task", err)
	}
	switch action.Type {
	case ActionFetch:
		t.Action = FetchAction{URL: action.URL, RunScripts: action.RunScripts}
	case "":
		return newTaskError(TaskErrorInvalidInput, "unmarshal_task", fmt.Errorf("action type is required"))
	default:
		return newTaskError(TaskErrorCapabilityUnavailable, "unmarshal_task", fmt.Errorf("action %q", action.Type))
	}
	t.ID = wire.ID
	t.SessionID = wire.SessionID
	t.Timeout = wire.Timeout
	return nil
}

// PageResult is observable evidence produced by a completed fetch action.
type PageResult struct {
	URL        string       `json:"url"`
	StatusCode int          `json:"statusCode"`
	Title      string       `json:"title"`
	HTML       string       `json:"html"`
	Text       string       `json:"text"`
	Markdown   string       `json:"markdown"`
	Links      []agent.Link `json:"links"`
}

// TaskError is a stable typed failure returned by the Agent lifecycle.
type TaskError struct {
	Code  TaskErrorCode
	Op    string
	Cause error
}

func (e *TaskError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("artemis: %s: %s", e.Op, e.Code)
	}
	return fmt.Sprintf("artemis: %s: %s: %v", e.Op, e.Code, e.Cause)
}

func (e *TaskError) Unwrap() error { return e.Cause }

func newTaskError(code TaskErrorCode, op string, cause error) *TaskError {
	return &TaskError{Code: code, Op: op, Cause: cause}
}

func classifyTaskError(op string, err error) *TaskError {
	var taskErr *TaskError
	if errors.As(err, &taskErr) {
		return taskErr
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return newTaskError(TaskErrorTimeout, op, err)
	case errors.Is(err, context.Canceled):
		return newTaskError(TaskErrorCancelled, op, err)
	default:
		return newTaskError(TaskErrorExecutionFailed, op, err)
	}
}

func failedTaskResult(taskID string, start time.Time, err *TaskError) TaskResult {
	return TaskResult{
		TaskID: taskID, Success: false, ErrorCode: err.Code,
		Error: err.Error(), Duration: time.Since(start),
	}
}

// RenderlessRuntime is the resource owned by a started Agent.
type RenderlessRuntime interface {
	Fetch(context.Context, string, engine.FetchOpts) (*engine.Page, error)
	Healthy() bool
	Close() error
}

// RuntimeFactory creates the owned runtime and may return a partial runtime
// together with an error so Start can prove rollback.
type RuntimeFactory interface {
	Start(context.Context, AgentConfig) (RenderlessRuntime, error)
}

type engineRuntimeFactory struct{}

func (engineRuntimeFactory) Start(ctx context.Context, config AgentConfig) (RenderlessRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime, err := engine.New(engine.Config{UserAgent: config.UserAgent, Timeout: config.FetchTimeout})
	if err != nil {
		return nil, err
	}
	return &ownedEngineRuntime{Engine: runtime}, nil
}

type ownedEngineRuntime struct {
	*engine.Engine
	closed atomic.Bool
}

func (r *ownedEngineRuntime) Healthy() bool { return r != nil && r.Engine != nil && !r.closed.Load() }

func (r *ownedEngineRuntime) Close() error {
	if r == nil || r.Engine == nil || r.closed.Swap(true) {
		return nil
	}
	return r.Engine.Close()
}

// Dispatcher is the single execution seam used by Agent.ExecuteTask.
type Dispatcher interface {
	Execute(context.Context, RenderlessRuntime, Task) (*PageResult, error)
}

type renderlessDispatcher struct{}

func (renderlessDispatcher) Execute(ctx context.Context, runtime RenderlessRuntime, task Task) (*PageResult, error) {
	switch action := task.Action.(type) {
	case FetchAction:
		return executeFetch(ctx, runtime, action)
	case *FetchAction:
		if action == nil {
			return nil, newTaskError(TaskErrorInvalidInput, "dispatch", fmt.Errorf("nil fetch action"))
		}
		return executeFetch(ctx, runtime, *action)
	default:
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "dispatch", fmt.Errorf("action %q", task.Action.Kind()))
	}
}

func executeFetch(ctx context.Context, runtime RenderlessRuntime, action FetchAction) (*PageResult, error) {
	parsed, err := url.ParseRequestURI(action.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, newTaskError(TaskErrorInvalidInput, "fetch", fmt.Errorf("invalid HTTP URL %q", action.URL))
	}
	page, err := runtime.Fetch(ctx, action.URL, engine.FetchOpts{RunScripts: action.RunScripts})
	if err != nil {
		return nil, classifyTaskError("fetch", err)
	}
	if page == nil {
		return nil, newTaskError(TaskErrorExecutionFailed, "fetch", fmt.Errorf("runtime returned no page"))
	}
	defer page.Close()
	return &PageResult{
		URL: page.URL(), StatusCode: page.StatusCode(), Title: page.Title(), HTML: page.HTML(),
		Text: page.Text(), Markdown: page.Markdown(), Links: page.Links(),
	}, nil
}

// SessionStore owns the active session registry.
type SessionStore interface {
	PutIfBelow(*Session, int) error
	Get(string) (*Session, bool)
	Delete(string)
	List() []*Session
	Len() int
}

type memorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: make(map[string]*Session)}
}

func (s *memorySessionStore) PutIfBelow(session *Session, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= limit {
		return newTaskError(TaskErrorSessionLimit, "store_session", fmt.Errorf("maximum %d active sessions", limit))
	}
	if _, exists := s.sessions[session.id]; exists {
		return newTaskError(TaskErrorExecutionFailed, "store_session", fmt.Errorf("duplicate session %q", session.id))
	}
	s.sessions[session.id] = session
	return nil
}

func (s *memorySessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *memorySessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *memorySessionStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	return result
}

func (s *memorySessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// AgentEventType identifies lifecycle evidence emitted through Telemetry.
type AgentEventType string

const (
	AgentEventStarted        AgentEventType = "agent.started"
	AgentEventStopped        AgentEventType = "agent.stopped"
	AgentEventSessionCreated AgentEventType = "session.created"
	AgentEventSessionClosed  AgentEventType = "session.closed"
	AgentEventTaskCompleted  AgentEventType = "task.completed"
)

// AgentEvent is a typed lifecycle or execution observation.
type AgentEvent struct {
	Type      AgentEventType
	SessionID string
	TaskID    string
	At        time.Time
}

// Telemetry receives typed Agent observations.
type Telemetry interface{ Record(AgentEvent) }

type discardTelemetry struct{}

func (discardTelemetry) Record(AgentEvent) {}

// Dependencies are injectable lifecycle boundaries used for tests and embedding.
type Dependencies struct {
	RuntimeFactory RuntimeFactory
	Dispatcher     Dispatcher
	SessionStore   SessionStore
	Telemetry      Telemetry
}

// NewAgentWithDependencies validates and constructs an Agent with explicit ports.
func NewAgentWithDependencies(config AgentConfig, dependencies Dependencies) (*Agent, error) {
	if config.MaxSessions < 0 || config.MaxTabs < 0 || config.ScriptTimeout < 0 || config.FetchTimeout < 0 {
		return nil, newTaskError(TaskErrorInvalidInput, "new_agent", fmt.Errorf("limits and timeouts cannot be negative"))
	}
	if strings.ContainsAny(config.UserAgent, "\r\n") {
		return nil, newTaskError(TaskErrorInvalidInput, "new_agent", fmt.Errorf("user agent contains a line break"))
	}
	config.ApplyDefaults()
	if dependencyMissing(dependencies.RuntimeFactory) || dependencyMissing(dependencies.Dispatcher) || dependencyMissing(dependencies.SessionStore) || dependencyMissing(dependencies.Telemetry) {
		return nil, newTaskError(TaskErrorInvalidInput, "new_agent", fmt.Errorf("all dependencies are required"))
	}
	return &Agent{
		config: config, state: AgentStateCreated, factory: dependencies.RuntimeFactory,
		dispatcher: dependencies.Dispatcher, sessions: dependencies.SessionStore, telemetry: dependencies.Telemetry,
	}, nil
}

func dependencyMissing(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Session returns an active owned session.
func (a *Agent) Session(id string) (*Session, bool) {
	session, ok := a.sessions.Get(id)
	return session, ok && session != nil && session.IsActive()
}

// CloseSession closes and unregisters an owned session idempotently.
func (a *Agent) CloseSession(id string) error {
	session, ok := a.sessions.Get(id)
	if !ok || session == nil {
		return nil
	}
	closed := session.deactivate()
	a.sessions.Delete(id)
	if closed {
		a.telemetry.Record(AgentEvent{Type: AgentEventSessionClosed, SessionID: id, At: time.Now()})
	}
	return nil
}

func (s *Session) beginOperation() (context.Context, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return nil, false
	}
	s.operations.Add(1)
	return s.ctx, true
}

func (s *Session) deactivate() bool {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return false
	}
	s.active = false
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.operations.Wait()
	s.mu.Lock()
	s.tabs = 0
	s.mu.Unlock()
	return true
}

// DependencyHealth reports observed runtime state.
type DependencyHealth struct {
	State   SupportState `json:"state"`
	Healthy bool         `json:"healthy"`
	Detail  string       `json:"detail,omitempty"`
}

// Health is a real-time Agent lifecycle snapshot.
type Health struct {
	State      AgentState       `json:"state"`
	Sessions   int              `json:"sessions"`
	Renderless DependencyHealth `json:"renderless"`
	Chromium   DependencyHealth `json:"chromium"`
}

// AgentCapabilitySnapshot joins static release claims with observed health.
type AgentCapabilitySnapshot struct {
	Version      string       `json:"version"`
	Capabilities []Capability `json:"capabilities"`
	Health       Health       `json:"health"`
}

// Health returns observed dependency and ownership state.
func (a *Agent) Health() Health {
	a.mu.RLock()
	state := a.state
	runtimeReady := a.runtime != nil && a.runtime.Healthy() && state == AgentStateRunning
	a.mu.RUnlock()
	return Health{
		State: state, Sessions: a.sessions.Len(),
		Renderless: DependencyHealth{State: SupportSupported, Healthy: runtimeReady},
		Chromium:   DependencyHealth{State: SupportUnavailable, Healthy: false, Detail: "Chromium/CDP is not release-supported"},
	}
}

// CapabilitySnapshot returns the release registry beside live dependency state.
func (a *Agent) CapabilitySnapshot() AgentCapabilitySnapshot {
	return AgentCapabilitySnapshot{Version: Version, Capabilities: Capabilities(), Health: a.Health()}
}
