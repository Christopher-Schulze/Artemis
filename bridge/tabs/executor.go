package tabs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// executor.go (spec L4021: bridge/tabs/executor.go - concurrent tab
// execution).
//
// Multi-tab management: concurrent tab execution. Runs actions across
// multiple tabs in parallel with controlled concurrency.

// TabExecutor manages concurrent execution across multiple tabs
// (spec L4021: concurrent tab execution).
type TabExecutor struct {
	registry      *TabRegistry
	maxConcurrent int
	sem           chan struct{}
	wg            sync.WaitGroup
}

// TabTask is a task to execute on a specific tab
// (spec L4021: concurrent tab execution).
type TabTask struct {
	TabID   string
	Action  func(ctx context.Context, tab *Tab) error
	Timeout time.Duration
}

// TabTaskResult is the result of a tab task execution
// (spec L4021: concurrent tab execution).
type TabTaskResult struct {
	TabID    string        `json:"tabId"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// NewTabExecutor creates a new TabExecutor
// (spec L4021: concurrent tab execution).
func NewTabExecutor(registry *TabRegistry, maxConcurrent int) *TabExecutor {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &TabExecutor{
		registry:      registry,
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// ExecuteTask executes a single task on a tab
// (spec L4021: concurrent tab execution).
func (e *TabExecutor) ExecuteTask(ctx context.Context, task TabTask) TabTaskResult {
	start := time.Now()
	tab, ok := e.registry.GetTab(task.TabID)
	if !ok {
		return TabTaskResult{
			TabID:   task.TabID,
			Success: false,
			Error:   fmt.Sprintf("executor: tab %s not found", task.TabID),
		}
	}
	timeout := task.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := task.Action(taskCtx, tab)
	result := TabTaskResult{
		TabID:    task.TabID,
		Duration: time.Since(start),
	}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}
	return result
}

// ExecuteConcurrent executes multiple tasks concurrently
// (spec L4021: concurrent tab execution).
func (e *TabExecutor) ExecuteConcurrent(ctx context.Context, tasks []TabTask) []TabTaskResult {
	results := make([]TabTaskResult, len(tasks))
	for i, task := range tasks {
		e.wg.Add(1)
		e.sem <- struct{}{}
		go func(idx int, t TabTask) {
			defer e.wg.Done()
			defer func() { <-e.sem }()
			results[idx] = e.ExecuteTask(ctx, t)
		}(i, task)
	}
	e.wg.Wait()
	return results
}

// ExecuteSequential executes tasks one at a time
// (spec L4021: concurrent tab execution).
func (e *TabExecutor) ExecuteSequential(ctx context.Context, tasks []TabTask) []TabTaskResult {
	results := make([]TabTaskResult, len(tasks))
	for i, task := range tasks {
		results[i] = e.ExecuteTask(ctx, task)
	}
	return results
}

// MaxConcurrent returns the max concurrent limit
// (spec L4021: concurrent tab execution).
func (e *TabExecutor) MaxConcurrent() int {
	return e.maxConcurrent
}

// String returns a diagnostic summary.
func (r TabTaskResult) String() string {
	return fmt.Sprintf("TabTaskResult{tab:%s success:%v duration:%v}", r.TabID, r.Success, r.Duration)
}
