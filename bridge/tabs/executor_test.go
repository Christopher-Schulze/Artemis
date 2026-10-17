package tabs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutorQueuedCancellation(t *testing.T) {
	for _, sameTab := range []bool{true, false} {
		t.Run(fmt.Sprintf("same_tab_%v", sameTab), func(t *testing.T) {
			executor := NewTabExecutor(nil, 1)
			release, err := executor.Acquire(t.Context(), "first")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			probe := &doneProbeContext{Context: ctx, doneCalled: make(chan struct{})}
			id := "second"
			if sameTab {
				id = "first"
			}
			done := make(chan error, 1)
			go func() {
				free, err := executor.Acquire(probe, id)
				if free != nil {
					free()
				}
				done <- err
			}()
			<-probe.doneCalled
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("queued admission error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("queued cancellation waited for active work")
			}
		})
	}
}

func TestExecutorCleanupDoesNotWaitForAnotherTabsCapacity(t *testing.T) {
	executor := NewTabExecutor(nil, 1)
	release, err := executor.Acquire(t.Context(), "busy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := executor.WaitTabIdle(ctx, "idle"); err != nil {
		t.Fatalf("unrelated active tab prevented idle target cleanup: %v", err)
	}
	if err := executor.WaitTabIdle(ctx, "busy"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active target was not joined: %v", err)
	}
}

func TestExecutorSerializesTabsAndBoundsParallelism(t *testing.T) {
	executor := NewTabExecutor(nil, 2)
	var workers sync.WaitGroup
	var mu sync.Mutex
	active := make(map[string]int)
	var current, maximum int
	violations := make(chan string, 40)
	for i := range 40 {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			release, err := executor.Acquire(t.Context(), id)
			if err != nil {
				violations <- err.Error()
				return
			}
			mu.Lock()
			active[id]++
			current++
			maximum = max(maximum, current)
			if active[id] != 1 || current > 2 {
				violations <- fmt.Sprintf("overlap tab=%s per_tab=%d total=%d", id, active[id], current)
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			active[id]--
			current--
			mu.Unlock()
			release()
		}(fmt.Sprintf("tab-%d", i%4))
	}
	workers.Wait()
	close(violations)
	for violation := range violations {
		t.Error(violation)
	}
	if maximum != 2 {
		t.Fatalf("cross-tab parallelism=%d, want 2", maximum)
	}
	if len(executor.locks.owners) != 0 || len(executor.locks.waiters) != 0 || len(executor.locks.changed) != 0 {
		t.Fatal("completed work retained tab lock entries")
	}
}

func TestExecutorBatchesHaveIndependentCompletion(t *testing.T) {
	registry := newTestRegistry()
	first := registry.CreateTab("owner", "https://first.example")
	second := registry.CreateTab("owner", "https://second.example")
	executor := NewTabExecutor(registry, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	blockingDone := make(chan []TabTaskResult, 1)
	go func() {
		blockingDone <- executor.ExecuteConcurrent(t.Context(), []TabTask{{TabID: first.ID, Action: func(ctx context.Context, tab *Tab) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}})
	}()
	<-entered
	defer func() { close(release); <-blockingDone }()
	var ran atomic.Bool
	done := make(chan []TabTaskResult, 1)
	go func() {
		done <- executor.ExecuteConcurrent(t.Context(), []TabTask{{TabID: second.ID, Action: func(ctx context.Context, tab *Tab) error {
			ran.Store(tab.ID == second.ID)
			return nil
		}}})
	}()
	select {
	case results := <-done:
		if len(results) != 1 || !results[0].Success || !ran.Load() {
			t.Fatalf("independent batch=%+v ran=%v", results, ran.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("independent batch waited for another batch")
	}
}

func TestExecutorRejectsInvalidActionsAndExpiredQueue(t *testing.T) {
	registry := newTestRegistry()
	tab := registry.CreateTab("owner", "https://example.com")
	executor := NewTabExecutor(registry, 1)
	if result := executor.ExecuteTask(t.Context(), TabTask{TabID: tab.ID}); result.Success || result.Error != "executor: action required" {
		t.Fatalf("nil action result=%+v", result)
	}
	release, err := executor.Acquire(t.Context(), tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var invoked bool
	result := executor.ExecuteTask(t.Context(), TabTask{TabID: tab.ID, Timeout: 10 * time.Millisecond, Action: func(context.Context, *Tab) error {
		invoked = true
		return nil
	}})
	if result.Success || result.Error != context.DeadlineExceeded.Error() || invoked {
		t.Fatalf("expired queued action ran=%v result=%+v", invoked, result)
	}
}
