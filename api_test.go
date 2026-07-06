package artemis

import (
	"context"
	"testing"
	"time"
)

// ==================== api.go tests ====================

// TestTASK2258_NewAgent verifies agent creation
func TestTASK2258_NewAgent(t *testing.T) {
	a := NewAgent(AgentConfig{MaxTabs: 5})
	if a == nil {
		t.Fatal("agent should not be nil")
	}
	if a.State() != AgentStateCreated {
		t.Error("initial state should be created")
	}
	if a.IsStarted() {
		t.Error("should not be started initially")
	}
}

// TestTASK2258_AgentConfigDefaults verifies defaults
func TestTASK2258_AgentConfigDefaults(t *testing.T) {
	cfg := AgentConfig{}
	cfg.ApplyDefaults()
	if cfg.MaxTabs != 10 {
		t.Error("default max tabs should be 10")
	}
	if cfg.ScriptTimeout != 30*time.Second {
		t.Error("default script timeout should be 30s")
	}
	if cfg.UserAgent == "" {
		t.Error("default user agent should not be empty")
	}
}

// TestTASK2258_AgentStart verifies start
func TestTASK2258_AgentStart(t *testing.T) {
	a := NewAgent(AgentConfig{})
	err := a.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !a.IsStarted() {
		t.Error("should be started")
	}
	if a.State() != AgentStateRunning {
		t.Error("state should be running")
	}
}

// TestTASK2258_AgentStartTwice verifies double start fails.
func TestTASK2258_AgentStartTwice(t *testing.T) {
	a := NewAgent(AgentConfig{})
	a.Start(context.Background())
	err := a.Start(context.Background())
	if err == nil {
		t.Error("double start should fail")
	}
}

// TestTASK2258_AgentStop verifies stop
func TestTASK2258_AgentStop(t *testing.T) {
	a := NewAgent(AgentConfig{})
	a.Start(context.Background())
	err := a.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if a.IsStarted() {
		t.Error("should not be started after stop")
	}
	if a.State() != AgentStateStopped {
		t.Error("state should be stopped")
	}
}

// TestTASK2258_AgentStopNotStarted verifies stop without start.
func TestTASK2258_AgentStopNotStarted(t *testing.T) {
	a := NewAgent(AgentConfig{})
	err := a.Stop()
	if err == nil {
		t.Error("stop without start should fail")
	}
}

// TestTASK2258_AgentConfig verifies config retrieval
func TestTASK2258_AgentConfig(t *testing.T) {
	a := NewAgent(AgentConfig{MaxTabs: 7, UserAgent: "test"})
	cfg := a.Config()
	if cfg.MaxTabs != 7 {
		t.Error("max tabs mismatch")
	}
	if cfg.UserAgent != "test" {
		t.Error("user agent mismatch")
	}
}

// TestTASK2258_AgentCreateSession verifies session creation
func TestTASK2258_AgentCreateSession(t *testing.T) {
	a := NewAgent(AgentConfig{})
	s := a.CreateSession("user1")
	if s == nil {
		t.Fatal("session should not be nil")
	}
	if s.UserID() != "user1" {
		t.Error("user ID mismatch")
	}
	if !s.IsActive() {
		t.Error("session should be active")
	}
}

// TestTASK2258_AgentExecuteTask verifies task execution
func TestTASK2258_AgentExecuteTask(t *testing.T) {
	a := NewAgent(AgentConfig{})
	a.Start(context.Background())
	result := a.ExecuteTask(context.Background(), Task{
		ID:  "task-1",
		URL: "https://example.com",
	})
	if !result.Success {
		t.Error("task should succeed")
	}
}

// TestTASK2258_AgentExecuteTaskNotStarted verifies task fails when not started.
func TestTASK2258_AgentExecuteTaskNotStarted(t *testing.T) {
	a := NewAgent(AgentConfig{})
	result := a.ExecuteTask(context.Background(), Task{
		ID:  "task-1",
		URL: "https://example.com",
	})
	if result.Success {
		t.Error("task should fail when not started")
	}
}

// TestTASK2258_AgentExecuteTaskEmptyURL verifies empty URL fails.
func TestTASK2258_AgentExecuteTaskEmptyURL(t *testing.T) {
	a := NewAgent(AgentConfig{})
	a.Start(context.Background())
	result := a.ExecuteTask(context.Background(), Task{ID: "task-1"})
	if result.Success {
		t.Error("empty URL should fail")
	}
}

// TestTASK2258_IsValidAgentState verifies validation.
func TestTASK2258_IsValidAgentState(t *testing.T) {
	if !IsValidAgentState(AgentStateRunning) {
		t.Error("running should be valid")
	}
	if IsValidAgentState(AgentState("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2258_SessionClose verifies session close
func TestTASK2258_SessionClose(t *testing.T) {
	a := NewAgent(AgentConfig{})
	s := a.CreateSession("user1")
	s.Close()
	if s.IsActive() {
		t.Error("session should not be active after close")
	}
}

// TestTASK2258_SessionTabs verifies tab management
func TestTASK2258_SessionTabs(t *testing.T) {
	a := NewAgent(AgentConfig{})
	s := a.CreateSession("user1")
	if s.TabCount() != 0 {
		t.Error("initial tab count should be 0")
	}
	s.AddTab()
	s.AddTab()
	if s.TabCount() != 2 {
		t.Errorf("tabs: got %d, want 2", s.TabCount())
	}
	s.RemoveTab()
	if s.TabCount() != 1 {
		t.Errorf("tabs: got %d, want 1", s.TabCount())
	}
}

// TestTASK2258_SessionRemoveTabZero verifies tab count doesn't go negative.
func TestTASK2258_SessionRemoveTabZero(t *testing.T) {
	a := NewAgent(AgentConfig{})
	s := a.CreateSession("user1")
	s.RemoveTab()
	if s.TabCount() != 0 {
		t.Error("tab count should not go below 0")
	}
}

// TestTASK2258_AgentString verifies String.
func TestTASK2258_AgentString(t *testing.T) {
	a := NewAgent(AgentConfig{})
	s := a.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2258_SessionString verifies String.
func TestTASK2258_SessionString(t *testing.T) {
	a := NewAgent(AgentConfig{})
	s := a.CreateSession("user1")
	str := s.String()
	if str == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2258_TaskString verifies String.
func TestTASK2258_TaskString(t *testing.T) {
	task := Task{ID: "t1", URL: "https://example.com", Action: "click"}
	s := task.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2258_TaskResultString verifies String.
func TestTASK2258_TaskResultString(t *testing.T) {
	r := TaskResult{TaskID: "t1", Success: true, Duration: 10 * time.Millisecond}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2258_FullSpecParity verifies the api.go public API
func TestTASK2258_FullSpecParity(t *testing.T) {
	// Create agent
	a := NewAgent(AgentConfig{})
	if a.State() != AgentStateCreated {
		t.Error("should start in created state")
	}

	// Start agent
	a.Start(context.Background())
	if !a.IsStarted() {
		t.Error("should be started")
	}

	// Create session
	s := a.CreateSession("user1")
	if !s.IsActive() {
		t.Error("session should be active")
	}

	// Execute task
	result := a.ExecuteTask(context.Background(), Task{
		ID:  "task-1",
		URL: "https://example.com",
	})
	if !result.Success {
		t.Error("task should succeed")
	}

	// Stop agent
	a.Stop()
	if a.State() != AgentStateStopped {
		t.Error("should be stopped")
	}
}
