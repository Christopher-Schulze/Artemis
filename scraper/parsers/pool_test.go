package parsers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolProcessAll(t *testing.T) {
	pool := NewWorkerPool(4)
	jobs := []ParseJob{
		{ID: "j1", HTML: "<html>1</html>", Parse: func(ctx context.Context, html string) (interface{}, error) {
			return "result1", nil
		}},
		{ID: "j2", HTML: "<html>2</html>", Parse: func(ctx context.Context, html string) (interface{}, error) {
			return "result2", nil
		}},
		{ID: "j3", HTML: "<html>3</html>", Parse: func(ctx context.Context, html string) (interface{}, error) {
			return "result3", nil
		}},
	}
	results := pool.ProcessAll(context.Background(), jobs)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Results may arrive in any order
	found := make(map[string]bool)
	for _, r := range results {
		found[r.JobID] = true
	}
	if !found["j1"] || !found["j2"] || !found["j3"] {
		t.Fatalf("missing results, got %v", found)
	}
}

func TestWorkerPoolWithError(t *testing.T) {
	pool := NewWorkerPool(2)
	jobs := []ParseJob{
		{ID: "j1", Parse: func(ctx context.Context, html string) (interface{}, error) {
			return nil, errors.New("parse error")
		}},
	}
	results := pool.ProcessAll(context.Background(), jobs)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Fatal("expected error")
	}
}

func TestWorkerPoolDefaultWorkers(t *testing.T) {
	pool := NewWorkerPool(0)
	if pool.Workers() <= 0 {
		t.Fatal("expected default workers > 0")
	}
}

func TestWorkerPoolExplicitWorkers(t *testing.T) {
	pool := NewWorkerPool(8)
	if pool.Workers() != 8 {
		t.Fatalf("expected 8 workers, got %d", pool.Workers())
	}
}

func TestWorkerPoolStop(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Start()
	pool.Stop()
	// Submit after stop should return false
	if pool.Submit(ParseJob{ID: "late"}) {
		t.Fatal("expected Submit to return false after Stop")
	}
}

func TestWorkerPoolSubmitAfterStop(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Stop()
	if pool.Submit(ParseJob{ID: "j1"}) {
		t.Fatal("expected Submit to return false after Stop")
	}
}

func TestWorkerPoolResults(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Start()
	pool.Submit(ParseJob{
		ID: "j1",
		Parse: func(ctx context.Context, html string) (interface{}, error) {
			return "ok", nil
		},
	})
	select {
	case r := <-pool.Results():
		if r.Value != "ok" {
			t.Fatalf("expected 'ok', got %v", r.Value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result")
	}
	pool.Stop()
}

func TestWorkerPoolWorkerID(t *testing.T) {
	pool := NewWorkerPool(4)
	pool.Start()
	for i := 0; i < 10; i++ {
		pool.Submit(ParseJob{
			ID: "j",
			Parse: func(ctx context.Context, html string) (interface{}, error) {
				return nil, nil
			},
		})
	}
	// Collect results and verify worker IDs are valid
	for i := 0; i < 10; i++ {
		select {
		case r := <-pool.Results():
			if r.Worker < 0 || r.Worker >= 4 {
				t.Fatalf("worker ID %d out of range", r.Worker)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}
	pool.Stop()
}

func TestWorkerPoolContextCancellation(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Start()

	// Submit a slow job that checks context cancellation
	pool.Submit(ParseJob{
		ID: "slow",
		Parse: func(ctx context.Context, html string) (interface{}, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return "done", nil
			}
		},
	})

	// Stop the pool (cancels context)
	pool.Stop()
}

func TestWorkerPoolConcurrentSubmit(t *testing.T) {
	pool := NewWorkerPool(4)
	pool.Start()

	var submitted atomic.Int32
	for i := 0; i < 20; i++ {
		go func() {
			if pool.Submit(ParseJob{
				ID: "j",
				Parse: func(ctx context.Context, html string) (interface{}, error) {
					return nil, nil
				},
			}) {
				submitted.Add(1)
			}
		}()
	}

	// Collect results
	collected := 0
	timeout := time.After(5 * time.Second)
	for collected < 20 {
		select {
		case <-pool.Results():
			collected++
		case <-timeout:
			t.Fatalf("timeout, collected %d", collected)
		}
	}
	pool.Stop()
}

func TestWorkerPoolEmptyJobs(t *testing.T) {
	pool := NewWorkerPool(2)
	results := pool.ProcessAll(context.Background(), nil)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestWorkerPoolDoubleStop(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Start()
	pool.Stop()
	pool.Stop() // should not panic
}
