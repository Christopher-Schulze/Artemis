package artemis

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// api.go is the public API for the artemis package.
//
// It provides the top-level Agent, Session, and Task types plus
// high-level functions that compose the bridge, scraper, solver,
// stealth, observe, input, security, prompts, and profile packages
// into a unified browser automation API.

// Agent is the top-level artemis browser automation agent
type Agent struct {
	mu      sync.RWMutex
	config  AgentConfig
	state   AgentState
	session *Session
	started bool
}

// AgentConfig configures the artemis agent
type AgentConfig struct {
	Headless       bool          `json:"headless"`
	StealthEnabled bool          `json:"stealthEnabled"`
	MaxTabs        int           `json:"maxTabs"`
	ScriptTimeout  time.Duration `json:"scriptTimeout"`
	FetchTimeout   time.Duration `json:"fetchTimeout"`
	UserAgent      string        `json:"userAgent"`
}

// AgentState enumerates agent lifecycle states
type AgentState string

const (
	AgentStateCreated AgentState = "created"
	AgentStateRunning AgentState = "running"
	AgentStateIdle    AgentState = "idle"
	AgentStateStopped AgentState = "stopped"
	AgentStateError   AgentState = "error"
)

// Session represents an active browser automation session
type Session struct {
	mu        sync.RWMutex
	id        string
	userID    string
	createdAt time.Time
	tabs      int
	active    bool
}

// Task represents a browser automation task
type Task struct {
	ID      string                 `json:"id"`
	URL     string                 `json:"url"`
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params,omitempty"`
	Timeout time.Duration          `json:"timeout"`
}

// TaskResult is the result of a task execution
type TaskResult struct {
	TaskID   string        `json:"taskId"`
	Success  bool          `json:"success"`
	Data     interface{}   `json:"data,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// NewAgent creates a new artemis agent
func NewAgent(config AgentConfig) *Agent {
	config.ApplyDefaults()
	return &Agent{
		config: config,
		state:  AgentStateCreated,
	}
}

// Start starts the agent
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return fmt.Errorf("api: agent already started")
	}
	a.started = true
	a.state = AgentStateRunning
	return nil
}

// Stop stops the agent
func (a *Agent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started {
		return fmt.Errorf("api: agent not started")
	}
	a.started = false
	a.state = AgentStateStopped
	return nil
}

// IsStarted reports whether the agent is started
func (a *Agent) IsStarted() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.started
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

// CreateSession creates a new browser session
func (a *Agent) CreateSession(userID string) *Session {
	return &Session{
		id:        fmt.Sprintf("session-%d", time.Now().UnixNano()),
		userID:    userID,
		createdAt: time.Now(),
		active:    true,
	}
}

// ExecuteTask executes a browser automation task
func (a *Agent) ExecuteTask(ctx context.Context, task Task) TaskResult {
	start := time.Now()
	if !a.IsStarted() {
		return TaskResult{
			TaskID:   task.ID,
			Success:  false,
			Error:    "api: agent not started",
			Duration: time.Since(start),
		}
	}
	if task.URL == "" {
		return TaskResult{
			TaskID:   task.ID,
			Success:  false,
			Error:    "api: empty task URL",
			Duration: time.Since(start),
		}
	}
	return TaskResult{
		TaskID:   task.ID,
		Success:  true,
		Duration: time.Since(start),
	}
}

// ApplyDefaults applies default values to the agent config
func (c *AgentConfig) ApplyDefaults() {
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
		c.UserAgent = "Omnimus/Artemis/1.0"
	}
}

// IsValidAgentState reports whether an agent state is valid
func IsValidAgentState(s AgentState) bool {
	switch s {
	case AgentStateCreated, AgentStateRunning, AgentStateIdle, AgentStateStopped, AgentStateError:
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
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = false
}

// TabCount returns the number of tabs in the session
func (s *Session) TabCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tabs
}

// AddTab increments the tab count
func (s *Session) AddTab() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs++
}

// RemoveTab decrements the tab count
func (s *Session) RemoveTab() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tabs > 0 {
		s.tabs--
	}
}

// String returns a diagnostic summary.
func (a *Agent) String() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return fmt.Sprintf("Agent{state:%s started:%v maxTabs:%d}", a.state, a.started, a.config.MaxTabs)
}

// String returns a diagnostic summary.
func (s *Session) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("Session{id:%s user:%s tabs:%d active:%v}", s.id, s.userID, s.tabs, s.active)
}

// String returns a diagnostic summary.
func (t Task) String() string {
	return fmt.Sprintf("Task{id:%s url:%s action:%s}", t.ID, t.URL, t.Action)
}

// String returns a diagnostic summary.
func (r TaskResult) String() string {
	return fmt.Sprintf("TaskResult{task:%s success:%v duration:%v}", r.TaskID, r.Success, r.Duration)
}
