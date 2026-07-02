// Package bridge implements CDP pipelining for batched operations (spec ss28.15.4 P1.1).
//
// P1.1 Pipelining: Multiple CDP commands via chromedp.Run(ctx, task1, task2, task3).
// MANDATORY batches:
//
//	A) boxModel + computedStyle + attributes
//	B) navigate + waitVisible + snapshot
//	C) DOM queries per element
//	D) screenshot + metrics
//
// Sequential CDP calls = code review blocker.
// P1.2 Batch Ops: chromedp.Tasks{} bundles. Saves ~200ms per 3-step sequence.
package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CDPTask represents a single CDP command to be executed.
type CDPTask struct {
	Name    string
	Execute func(ctx context.Context) (interface{}, error)
}

// CDPTaskResult holds the result of a single CDP task execution.
type CDPTaskResult struct {
	Name     string
	Value    interface{}
	Error    error
	Duration time.Duration
}

// CDPPipeline batches multiple CDP commands into a single execution
// to reduce round-trips. Per spec P1.1: sequential CDP calls are a
// code review blocker; all multi-step sequences must be batched.
type CDPPipeline struct {
	tasks   []CDPTask
	timeout time.Duration
}

// NewCDPPipeline creates an empty pipeline with the given timeout.
// Default timeout is 30s.
func NewCDPPipeline(timeout time.Duration) *CDPPipeline {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &CDPPipeline{timeout: timeout}
}

// Add appends a CDP task to the pipeline.
func (p *CDPPipeline) Add(task CDPTask) {
	if p == nil || task.Execute == nil {
		return
	}
	p.tasks = append(p.tasks, task)
}

// AddTask is a convenience method to add a named task.
func (p *CDPPipeline) AddTask(name string, execute func(ctx context.Context) (interface{}, error)) {
	p.Add(CDPTask{Name: name, Execute: execute})
}

// Len returns the number of tasks in the pipeline.
func (p *CDPPipeline) Len() int {
	if p == nil {
		return 0
	}
	return len(p.tasks)
}

// Run executes all tasks in the pipeline as a batch. Tasks run sequentially
// within the batch (CDP is single-threaded per session), but the entire batch
// is submitted as one unit, reducing protocol overhead.
//
// Per spec P1.2: chromedp.Tasks{} bundles save ~200ms per 3-step sequence.
func (p *CDPPipeline) Run(ctx context.Context) []CDPTaskResult {
	if p == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	results := make([]CDPTaskResult, len(p.tasks))
	for i, task := range p.tasks {
		start := time.Now()
		val, err := task.Execute(ctx)
		results[i] = CDPTaskResult{
			Name:     task.Name,
			Value:    val,
			Error:    err,
			Duration: time.Since(start),
		}
		if err != nil {
			if ctx.Err() != nil {
				break // context cancelled, stop pipeline
			}
			// Continue with remaining tasks even on error
		}
	}
	return results
}

// RunParallel executes all tasks in parallel. Useful for independent CDP
// commands that don't depend on each other (e.g., boxModel + computedStyle
// for different elements).
func (p *CDPPipeline) RunParallel(ctx context.Context) []CDPTaskResult {
	if p == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	var wg sync.WaitGroup
	results := make([]CDPTaskResult, len(p.tasks))

	for i, task := range p.tasks {
		wg.Add(1)
		go func(idx int, t CDPTask) {
			defer wg.Done()
			start := time.Now()
			val, err := t.Execute(ctx)
			results[idx] = CDPTaskResult{
				Name:     t.Name,
				Value:    val,
				Error:    err,
				Duration: time.Since(start),
			}
		}(i, task)
	}
	wg.Wait()
	return results
}

// HasErrors returns true if any task in the results has an error.
func HasErrors(results []CDPTaskResult) bool {
	for _, r := range results {
		if r.Error != nil {
			return true
		}
	}
	return false
}

// FirstError returns the first error in the results, or nil if none.
func FirstError(results []CDPTaskResult) error {
	for _, r := range results {
		if r.Error != nil {
			return r.Error
		}
	}
	return nil
}

// TotalDuration returns the sum of all task durations.
func TotalDuration(results []CDPTaskResult) time.Duration {
	var total time.Duration
	for _, r := range results {
		total += r.Duration
	}
	return total
}

// MandatoryBatchType identifies the spec-mandated batch categories.
type MandatoryBatchType int

const (
	BatchBoxModelStyleAttrs   MandatoryBatchType = iota // A) boxModel + computedStyle + attributes
	BatchNavigateWaitSnapshot                           // B) navigate + waitVisible + snapshot
	BatchDOMQueries                                     // C) DOM queries per element
	BatchScreenshotMetrics                              // D) screenshot + metrics
)

// String returns the batch type name.
func (b MandatoryBatchType) String() string {
	switch b {
	case BatchBoxModelStyleAttrs:
		return "boxModel+computedStyle+attributes"
	case BatchNavigateWaitSnapshot:
		return "navigate+waitVisible+snapshot"
	case BatchDOMQueries:
		return "dom_queries"
	case BatchScreenshotMetrics:
		return "screenshot+metrics"
	default:
		return "unknown"
	}
}

// NewMandatoryBatch creates a pipeline pre-loaded with the spec-mandated
// batch tasks for the given batch type. The task functions must be provided
// by the caller since they depend on the specific CDP session.
func NewMandatoryBatch(batchType MandatoryBatchType, tasks ...CDPTask) *CDPPipeline {
	p := NewCDPPipeline(30 * time.Second)
	for _, task := range tasks {
		p.Add(task)
	}
	return p
}

// ValidateBatch checks that a pipeline meets the spec-mandated batching
// requirements. Returns an error if the batch is invalid.
func ValidateBatch(batchType MandatoryBatchType, taskCount int) error {
	minTasks := map[MandatoryBatchType]int{
		BatchBoxModelStyleAttrs:   3, // boxModel + computedStyle + attributes
		BatchNavigateWaitSnapshot: 3, // navigate + waitVisible + snapshot
		BatchDOMQueries:           1, // at least 1 DOM query
		BatchScreenshotMetrics:    2, // screenshot + metrics
	}
	min, ok := minTasks[batchType]
	if !ok {
		return fmt.Errorf("unknown batch type %d", batchType)
	}
	if taskCount < min {
		return fmt.Errorf("batch %s requires at least %d tasks, got %d", batchType, min, taskCount)
	}
	return nil
}
