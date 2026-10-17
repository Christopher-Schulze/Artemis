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
	locks         *TabLock
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
		locks:         NewTabLock(),
	}
}

// ExecuteTask executes a single task on a tab
// (spec L4021: concurrent tab execution).
func (e *TabExecutor) ExecuteTask(ctx context.Context, task TabTask) TabTaskResult {
	start := time.Now()
	if ctx == nil {
		return TabTaskResult{TabID: task.TabID, Success: false, Error: "executor: context required"}
	}
	if e.registry == nil {
		return TabTaskResult{TabID: task.TabID, Success: false, Error: "executor: tab registry required"}
	}
	if task.Action == nil {
		return TabTaskResult{TabID: task.TabID, Error: "executor: action required"}
	}
	timeout := task.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := e.Acquire(taskCtx, task.TabID)
	if err != nil {
		return TabTaskResult{TabID: task.TabID, Error: err.Error(), Duration: time.Since(start)}
	}
	defer release()
	if err := e.registry.Sync(taskCtx); err != nil {
		return TabTaskResult{TabID: task.TabID, Success: false, Error: err.Error()}
	}
	tab, ok := e.registry.GetTab(task.TabID)
	if !ok {
		return TabTaskResult{
			TabID:   task.TabID,
			Success: false,
			Error:   fmt.Sprintf("executor: tab %s not found", task.TabID),
		}
	}
	err = task.Action(taskCtx, tab)
	if err == nil {
		err = taskCtx.Err()
	}
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
	var workers sync.WaitGroup
	jobs := make(chan int)
	for range min(len(tasks), e.maxConcurrent) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for idx := range jobs {
				results[idx] = e.ExecuteTask(ctx, tasks[idx])
			}
		}()
	}
	for idx := range tasks {
		jobs <- idx
	}
	close(jobs)
	workers.Wait()
	return results
}

// Acquire shares per-tab serialization and bounded cross-tab admission with
// production page owners. Waiting on a busy tab never consumes global capacity.
func (e *TabExecutor) Acquire(ctx context.Context, tabID string) (func(), error) {
	if ctx == nil || tabID == "" {
		return nil, fmt.Errorf("executor: context and tab ID required")
	}
	if err := e.locks.LockContext(ctx, tabID, "executor"); err != nil {
		return nil, err
	}
	select {
	case e.sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-e.sem
			e.locks.Unlock(tabID)
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { <-e.sem; e.locks.Unlock(tabID) }) }, nil
	case <-ctx.Done():
		e.locks.Unlock(tabID)
		return nil, ctx.Err()
	}
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

// WaitTabIdle joins this tab's execution without reserving cross-tab capacity.
func (e *TabExecutor) WaitTabIdle(ctx context.Context, tabID string) error {
	if ctx == nil || tabID == "" {
		return fmt.Errorf("executor: context and tab ID required")
	}
	if err := e.locks.LockContext(ctx, tabID, "cleanup"); err != nil {
		return err
	}
	e.locks.Unlock(tabID)
	return nil
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
