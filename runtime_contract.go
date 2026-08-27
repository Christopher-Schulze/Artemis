package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/profile"
)

// ActionKind identifies a closed high-level Agent operation.
type ActionKind string

const ActionFetch ActionKind = "fetch"

// defaultProfileCloseTimeout bounds how long the profile runtime is given to close
// a managed session during teardown or rollback before we report a timeout.
const defaultProfileCloseTimeout = 10 * time.Second

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

// AssertResult is the structured outcome of a page assertion.
type AssertResult struct {
	Pass bool   `json:"pass"`
	Got  string `json:"got"`
}

// ChromiumActions executes Chromium/CDP browser actions with the same
// request/outcome contract used by bridge/actions.Runtime.
type ChromiumActions interface {
	Execute(context.Context, actions.Request) actions.Outcome
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

// profileRuntime abstracts the optional profile manager so tests can inject
// fakes without spinning up an on-disk profile store.
type profileRuntime interface {
	Open(context.Context, profile.OpenSessionRequest) (*profile.RuntimeSession, error)
	Close(context.Context, profile.SessionID, string) error
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
	runtime, err := engine.New(engine.Config{
		UserAgent:    config.UserAgent,
		Timeout:      config.FetchTimeout,
		ObeyRobots:   config.ObeyRobots,
		PolicyConfig: config.PolicyConfig,
		Diagnostics:  config.Diagnostics,
	})
	if err != nil {
		return nil, err
	}
	return &ownedEngineRuntime{Engine: runtime}, nil
}

type ownedEngineRuntime struct {
	*engine.Engine
	closed atomic.Bool
}

func (r *ownedEngineRuntime) ReleaseSession(sessionID string) {
	if r != nil && r.Engine != nil {
		r.Engine.ReleaseSession(sessionID)
	}
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

// ListSessions returns a snapshot of all owned sessions.
func (a *Agent) ListSessions() []*Session {
	return a.sessions.List()
}

// CloseSession closes and unregisters an owned session idempotently.
func (a *Agent) CloseSession(id string) error {
	session, ok := a.sessions.Get(id)
	if !ok || session == nil {
		return nil
	}
	closed, closeErr := session.deactivate()
	a.sessions.Delete(id)
	if closed {
		a.mu.RLock()
		runtime := a.runtime
		profileRuntime := a.profileRuntime
		a.mu.RUnlock()
		if releaser, ok := runtime.(interface{ ReleaseSession(string) }); ok {
			releaser.ReleaseSession(id)
		}
		a.telemetry.Record(AgentEvent{Type: AgentEventSessionClosed, SessionID: id, At: time.Now()})
		if session.managed && profileRuntime != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), defaultProfileCloseTimeout)
			defer cancel()
			if err := profileRuntime.Close(closeCtx, profile.SessionID(session.id), session.userID); err != nil {
				closeErr = errors.Join(closeErr, err)
			}
		}
	}
	if closeErr != nil {
		return classifyTaskError("close_session", closeErr)
	}
	return nil
}

// CloseSessionForOwner closes a session only if the caller owns it.
func (a *Agent) CloseSessionForOwner(id, owner string) error {
	session, ok := a.sessions.Get(id)
	if !ok || session == nil {
		return nil
	}
	if session.userID != owner {
		return newTaskError(TaskErrorPolicyDenied, "close_session", fmt.Errorf("ownership denied"))
	}
	return a.CloseSession(id)
}

// SetChromiumActions injects the optional Chromium action executor.
func (a *Agent) SetChromiumActions(ca ChromiumActions) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chromiumActions = ca
}

// ChromiumActions returns the configured Chromium action executor, or nil.
func (a *Agent) ChromiumActions() ChromiumActions {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.chromiumActions
}

// SetProfileRuntime injects the optional profile runtime.
func (a *Agent) SetProfileRuntime(rm *profile.RuntimeManager) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.profileRuntime = rm
}

// CreateSessionForProfile opens a managed browser profile session.
func (a *Agent) CreateSessionForProfile(ctx context.Context, req profile.OpenSessionRequest) (*Session, error) {
	if ctx == nil {
		return nil, newTaskError(TaskErrorInvalidInput, "create_session", fmt.Errorf("nil context"))
	}
	a.mu.Lock()
	state := a.state
	maxSessions := a.config.MaxSessions
	profileRuntime := a.profileRuntime
	runCtx := a.runCtx
	generation := a.lifecycleGeneration
	if state != AgentStateRunning {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorInvalidTransition, "create_session", fmt.Errorf("state %s", state))
	}
	if profileRuntime == nil {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "create_session", fmt.Errorf("profile runtime is not configured"))
	}
	if req.OwnerUserRef == "" {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorInvalidInput, "create_session", fmt.Errorf("empty owner user ref"))
	}
	if req.ProfileID == "" {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorInvalidInput, "create_session", fmt.Errorf("empty profile id"))
	}
	if runCtx == nil {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorInvalidTransition, "create_session", fmt.Errorf("agent run context is unavailable"))
	}
	if a.profileSessionOpenings > 0 && a.sessions.Len()+a.profileSessionOpenings >= maxSessions {
		a.mu.Unlock()
		return nil, newTaskError(TaskErrorSessionLimit, "create_session", fmt.Errorf("maximum %d active sessions", maxSessions))
	}
	a.profileSessionOpenings++
	a.mu.Unlock()

	openCtx, cancelOpen := context.WithCancel(ctx)
	stopRun := context.AfterFunc(runCtx, cancelOpen)
	defer func() {
		stopRun()
		cancelOpen()
	}()
	rs, openErr := profileRuntime.Open(openCtx, req)
	if openErr != nil {
		lifecycleErr := a.finishProfileSessionAdmission(generation)
		closeErr := closeOpenedProfileSession(profileRuntime, rs, req.OwnerUserRef)
		if lifecycleErr != nil {
			return nil, profileSessionError(lifecycleErr, openErr, closeErr)
		}
		if ctxErr := openCtx.Err(); ctxErr != nil {
			return nil, profileSessionError(classifyTaskError("create_session", ctxErr), openErr, closeErr)
		}
		return nil, profileSessionError(openErr, closeErr)
	}
	if rs == nil {
		lifecycleErr := a.finishProfileSessionAdmission(generation)
		nilSessionErr := newTaskError(TaskErrorExecutionFailed, "create_session", fmt.Errorf("profile runtime returned no session"))
		if lifecycleErr != nil {
			return nil, profileSessionError(lifecycleErr, nilSessionErr)
		}
		if ctxErr := openCtx.Err(); ctxErr != nil {
			return nil, profileSessionError(classifyTaskError("create_session", ctxErr), nilSessionErr)
		}
		return nil, nilSessionErr
	}

	now := time.Now()
	a.profileSessionPublicationMu.Lock()
	a.mu.Lock()
	a.profileSessionOpenings--
	lifecycleErr := a.profileSessionLifecycleErrorLocked(generation)
	if lifecycleErr != nil {
		a.mu.Unlock()
		a.profileSessionPublicationMu.Unlock()
		closeErr := closeOpenedProfileSession(profileRuntime, rs, req.OwnerUserRef)
		return nil, profileSessionError(lifecycleErr, closeErr)
	}
	if ctxErr := openCtx.Err(); ctxErr != nil {
		a.mu.Unlock()
		a.profileSessionPublicationMu.Unlock()
		closeErr := closeOpenedProfileSession(profileRuntime, rs, req.OwnerUserRef)
		return nil, profileSessionError(classifyTaskError("create_session", ctxErr), closeErr)
	}
	sessionCtx, sessionCancel := context.WithCancel(runCtx)
	session := &Session{
		owner: a, ctx: sessionCtx, cancel: sessionCancel,
		id: string(rs.ID), userID: req.OwnerUserRef, createdAt: now,
		active: true, pages: make(map[string]*engine.Page),
		managed: true, profileID: string(req.ProfileID),
	}
	storeErr := a.sessions.PutIfBelow(session, maxSessions)
	if storeErr != nil {
		a.mu.Unlock()
		a.profileSessionPublicationMu.Unlock()
		sessionCancel()
		closeErr := closeOpenedProfileSession(profileRuntime, rs, req.OwnerUserRef)
		return nil, profileSessionError(storeErr, closeErr)
	}
	a.mu.Unlock()
	a.telemetry.Record(AgentEvent{Type: AgentEventSessionCreated, SessionID: session.id, At: now})
	a.profileSessionPublicationMu.Unlock()
	return session, nil
}

func (a *Agent) finishProfileSessionAdmission(generation uint64) *TaskError {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.profileSessionOpenings--
	return a.profileSessionLifecycleErrorLocked(generation)
}

func (a *Agent) profileSessionLifecycleErrorLocked(generation uint64) *TaskError {
	if a.state == AgentStateRunning && a.lifecycleGeneration == generation {
		return nil
	}
	if a.state != AgentStateRunning {
		return newTaskError(TaskErrorInvalidTransition, "create_session", fmt.Errorf("state %s", a.state))
	}
	return newTaskError(TaskErrorInvalidTransition, "create_session", fmt.Errorf("agent lifecycle changed during profile open"))
}

func closeOpenedProfileSession(runtime profileRuntime, session *profile.RuntimeSession, owner string) error {
	if runtime == nil || session == nil {
		return nil
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), defaultProfileCloseTimeout)
	defer cancel()
	closeOwner := session.OwnerUserRef
	if closeOwner == "" {
		closeOwner = owner
	}
	return runtime.Close(closeCtx, session.ID, closeOwner)
}

func profileSessionError(primary error, details ...error) *TaskError {
	var taskErr *TaskError
	if errors.As(primary, &taskErr) {
		causes := make([]error, 0, len(details)+1)
		if taskErr.Cause != nil {
			causes = append(causes, taskErr.Cause)
		}
		for _, detail := range details {
			if detail != nil {
				causes = append(causes, detail)
			}
		}
		if len(causes) == 0 {
			return taskErr
		}
		return newTaskError(taskErr.Code, taskErr.Op, errors.Join(causes...))
	}
	causes := []error{primary}
	for _, detail := range details {
		if detail != nil {
			causes = append(causes, detail)
		}
	}
	return classifyTaskError("create_session", errors.Join(causes...))
}

func (a *Agent) runtimeForSession() (RenderlessRuntime, *TaskError) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.state != AgentStateRunning || a.runtime == nil {
		return nil, newTaskError(TaskErrorInvalidTransition, "session", fmt.Errorf("agent state %s", a.state))
	}
	return a.runtime, nil
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

func (s *Session) deactivate() (bool, error) {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return false, nil
	}
	s.active = false
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.operations.Wait()
	s.mu.Lock()
	closeErr := s.closeAllPagesLocked()
	s.mu.Unlock()
	return true, closeErr
}

func (s *Session) closeAllPagesLocked() error {
	var closeErr error
	for id, page := range s.pages {
		delete(s.pages, id)
		if page != nil {
			closeErr = errors.Join(closeErr, page.Close())
		}
	}
	s.tabs = 0
	return closeErr
}

// OpenPage fetches a URL into a new page within the session.
func (s *Session) OpenPage(ctx context.Context, url string, runScripts bool) (string, *engine.Page, *TaskError) {
	if s.owner == nil {
		return "", nil, newTaskError(TaskErrorExecutionFailed, "open_page", fmt.Errorf("session has no owner"))
	}
	if err := s.AddTab(); err != nil {
		return "", nil, err
	}
	sessionCtx, ok := s.beginOperation()
	if !ok {
		return "", nil, newTaskError(TaskErrorStaleReference, "open_page", fmt.Errorf("session %q is closed", s.id))
	}
	defer s.operations.Done()
	runtime, err := s.owner.runtimeForSession()
	if err != nil {
		if rmErr := s.RemoveTab(); rmErr != nil {
			return "", nil, rmErr
		}
		return "", nil, err
	}
	execCtx, cancel := context.WithCancel(ctx)
	execCtx = network.WithSessionID(execCtx, s.id)
	stopSession := context.AfterFunc(sessionCtx, cancel)
	defer func() {
		cancel()
		stopSession()
	}()
	page, fetchErr := runtime.Fetch(execCtx, url, engine.FetchOpts{RunScripts: runScripts})
	if fetchErr != nil {
		if page != nil {
			if closeErr := page.Close(); closeErr != nil {
				fetchErr = errors.Join(fetchErr, closeErr)
			}
		}
		if rmErr := s.RemoveTab(); rmErr != nil {
			return "", nil, rmErr
		}
		return "", nil, classifyTaskError("open_page", fetchErr)
	}
	if page == nil {
		if rmErr := s.RemoveTab(); rmErr != nil {
			return "", nil, rmErr
		}
		return "", nil, newTaskError(TaskErrorExecutionFailed, "open_page", fmt.Errorf("runtime returned no page"))
	}
	now := time.Now()
	s.mu.Lock()
	s.pageSeq++
	pageID := fmt.Sprintf("p%d-%d", now.UnixNano(), s.pageSeq)
	s.pages[pageID] = page
	s.mu.Unlock()
	return pageID, page, nil
}

// Page returns an open page by id.
func (s *Session) Page(id string) *engine.Page {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pages[id]
}

// ClosePage closes a single page and removes it from the session.
func (s *Session) ClosePage(id string) *TaskError {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return newTaskError(TaskErrorStaleReference, "close_page", fmt.Errorf("session %q is closed", s.id))
	}
	page := s.pages[id]
	delete(s.pages, id)
	if page != nil {
		s.tabs--
	}
	s.mu.Unlock()
	if page != nil {
		if err := page.Close(); err != nil {
			return classifyTaskError("close_page", err)
		}
		return nil
	}
	return newTaskError(TaskErrorPageNotFound, "close_page", fmt.Errorf("page %q not found", id))
}

// Eval evaluates a JavaScript expression in the page's JS context.
func (s *Session) Eval(ctx context.Context, pageID, expr string) (*js.Value, *TaskError) {
	page := s.Page(pageID)
	if page == nil {
		return nil, newTaskError(TaskErrorPageNotFound, "eval", fmt.Errorf("page %q not found", pageID))
	}
	execCtx, cancel := context.WithTimeout(ctx, s.owner.Config().ScriptTimeout)
	defer cancel()
	v, err := page.Eval(execCtx, expr)
	if err != nil {
		return nil, classifyTaskError("eval", err)
	}
	return v, nil
}

// Dump returns the page content in the requested format.
func (s *Session) Dump(pageID, format string) (any, *TaskError) {
	page := s.Page(pageID)
	if page == nil {
		return nil, newTaskError(TaskErrorPageNotFound, "dump", fmt.Errorf("page %q not found", pageID))
	}
	switch format {
	case "html":
		return page.HTML(), nil
	case "markdown", "md":
		return page.Markdown(), nil
	case "text":
		return page.Text(), nil
	case "title":
		return page.Title(), nil
	case "links":
		return page.Links(), nil
	case "structured":
		return page.StructuredData(), nil
	case "semantic":
		return agent.SemanticString(page.SemanticTree()), nil
	default:
		return nil, newTaskError(TaskErrorInvalidInput, "dump", fmt.Errorf("unknown format %q", format))
	}
}

// ClickByText clicks the first button/anchor/input matching the text.
func (s *Session) ClickByText(ctx context.Context, pageID, text string) *TaskError {
	page := s.Page(pageID)
	if page == nil {
		return newTaskError(TaskErrorPageNotFound, "click_by_text", fmt.Errorf("page %q not found", pageID))
	}
	n, ok := agent.ClickByText(page.Document(), text)
	if !ok {
		return newTaskError(TaskErrorExecutionFailed, "click_by_text", fmt.Errorf("no clickable element with text %q", text))
	}
	execCtx, cancel := context.WithTimeout(ctx, s.owner.Config().ScriptTimeout)
	defer cancel()
	if err := page.Click(execCtx, n); err != nil {
		return classifyTaskError("click_by_text", err)
	}
	return nil
}

// Type types text into the input/textarea matching the selector.
func (s *Session) Type(pageID, selector, text string) *TaskError {
	page := s.Page(pageID)
	if page == nil {
		return newTaskError(TaskErrorPageNotFound, "type", fmt.Errorf("page %q not found", pageID))
	}
	if err := agent.Type(page.Document(), selector, text); err != nil {
		return newTaskError(TaskErrorExecutionFailed, "type", err)
	}
	return nil
}

// WaitIdle blocks until in-flight async fetches have settled.
func (s *Session) WaitIdle(ctx context.Context, pageID string) *TaskError {
	page := s.Page(pageID)
	if page == nil {
		return newTaskError(TaskErrorPageNotFound, "wait_idle", fmt.Errorf("page %q not found", pageID))
	}
	execCtx, cancel := context.WithTimeout(ctx, s.owner.Config().ScriptTimeout)
	defer cancel()
	if err := page.WaitIdle(execCtx); err != nil {
		return classifyTaskError("wait_idle", err)
	}
	return nil
}

// Assert evaluates an assertion against the current page state.
func (s *Session) Assert(ctx context.Context, pageID, mode, selector, substring, expr string, status int, want *bool) (AssertResult, *TaskError) {
	page := s.Page(pageID)
	if page == nil {
		return AssertResult{}, newTaskError(TaskErrorPageNotFound, "assert", fmt.Errorf("page %q not found", pageID))
	}
	target := true
	if want != nil {
		target = *want
	}
	switch mode {
	case "selector_exists":
		n, err := page.Document().QuerySelector(selector)
		if err != nil {
			return AssertResult{}, newTaskError(TaskErrorExecutionFailed, "assert", fmt.Errorf("query: %w", err))
		}
		got := n != nil
		return AssertResult{Pass: got == target, Got: fmt.Sprintf("exists=%v", got)}, nil
	case "title_contains":
		t := page.Title()
		got := strings.Contains(t, substring)
		return AssertResult{Pass: got == target, Got: t}, nil
	case "text_contains":
		t := page.Text()
		got := strings.Contains(t, substring)
		return AssertResult{Pass: got == target, Got: truncate(t, 200)}, nil
	case "url_contains":
		u := page.URL()
		got := strings.Contains(u, substring)
		return AssertResult{Pass: got == target, Got: u}, nil
	case "status_eq":
		got := page.StatusCode()
		return AssertResult{Pass: got == status, Got: strconv.Itoa(got)}, nil
	case "eval_truthy":
		if expr == "" {
			return AssertResult{}, newTaskError(TaskErrorInvalidInput, "assert", fmt.Errorf("eval_truthy requires expr"))
		}
		execCtx, cancel := context.WithTimeout(ctx, s.owner.Config().ScriptTimeout)
		defer cancel()
		v, err := page.Eval(execCtx, expr)
		if err != nil {
			return AssertResult{}, newTaskError(TaskErrorExecutionFailed, "assert", fmt.Errorf("eval: %w", err))
		}
		got := v != nil && v.Bool()
		return AssertResult{Pass: got == target, Got: v.String()}, nil
	default:
		return AssertResult{}, newTaskError(TaskErrorInvalidInput, "assert", fmt.Errorf("unknown assert mode %q", mode))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ChromiumAct executes a Chromium/CDP action through the configured executor.
func (s *Session) ChromiumAct(ctx context.Context, request actions.Request) (actions.Outcome, *TaskError) {
	sessionCtx, ok := s.beginOperation()
	if !ok {
		return actions.Outcome{}, newTaskError(TaskErrorStaleReference, "chromium_act", fmt.Errorf("session %q is closed", s.id))
	}
	defer s.operations.Done()
	execCtx, cancel := context.WithCancel(ctx)
	stopSession := context.AfterFunc(sessionCtx, cancel)
	defer func() {
		cancel()
		stopSession()
	}()
	ca := s.owner.ChromiumActions()
	if ca == nil {
		return actions.Outcome{}, newTaskError(TaskErrorCapabilityUnavailable, "chromium_act", fmt.Errorf("chromium action runtime is not configured"))
	}
	return ca.Execute(execCtx, request), nil
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
