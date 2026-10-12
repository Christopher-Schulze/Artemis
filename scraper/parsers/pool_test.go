package parsers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testArtifact(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
	return ParsedArtifact{URL: input.URL, HTML: []byte(input.HTML), Title: input.ID}, nil
}

func TestWorkerPoolProcessAll(t *testing.T) {
	pool := NewWorkerPool(4)
	jobs := []ParseJob{
		{ID: "j1", HTML: "<html>1</html>", Parse: testArtifact},
		{ID: "j2", HTML: "<html>2</html>", Parse: testArtifact},
		{ID: "j3", HTML: "<html>3</html>", Parse: testArtifact},
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
		{ID: "j1", Parse: func(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
			return ParsedArtifact{}, errors.New("parse error")
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
		Parse: func(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
			return ParsedArtifact{Title: "ok"}, nil
		},
	})
	select {
	case r := <-pool.Results():
		if r.Artifact.Title != "ok" {
			t.Fatalf("expected 'ok', got %v", r.Artifact.Title)
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
			Parse: func(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
				return ParsedArtifact{}, nil
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
		Parse: func(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
			select {
			case <-ctx.Done():
				return ParsedArtifact{}, ctx.Err()
			case <-time.After(10 * time.Second):
				return ParsedArtifact{Title: "done"}, nil
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
				Parse: func(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
					return ParsedArtifact{}, nil
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

func TestWorkerPoolStartIsIdempotentAndDefaultParserIsReal(t *testing.T) {
	pool := NewWorkerPool(1)
	pool.Start()
	pool.Start()
	if !pool.Submit(ParseJob{ID: "real", URL: "https://example.test", HTML: `<html><head><title>Title</title></head><body><p>Body</p><script>hidden()</script></body></html>`}) {
		t.Fatal("expected real parse job to be accepted")
	}
	result := <-pool.Results()
	if result.Error != nil {
		t.Fatalf("default parser: %v", result.Error)
	}
	if result.Artifact.Title != "Title" || result.Artifact.Text != "Body" || result.Artifact.Document == nil {
		t.Fatalf("unexpected artifact: %+v", result.Artifact)
	}
	pool.Stop()
}

func TestWorkerPoolStopSubmitRaceDoesNotPanic(t *testing.T) {
	pool := NewWorkerPool(2)
	pool.Start()
	for i := 0; i < 32; i++ {
		go pool.Submit(ParseJob{ID: "race", Parse: testArtifact})
	}
	pool.Stop()
}

func TestWorkerPoolProcessAllCancellationReturnsExplicitFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := NewWorkerPool(1).ProcessAll(ctx, []ParseJob{{ID: "cancelled", Parse: testArtifact}})
	if len(results) != 1 || results[0].Error == nil {
		t.Fatalf("cancellation must return an explicit failure result: %+v", results)
	}
}

func TestWorkerPoolProcessAllRestoresInputOrder(t *testing.T) {
	jobs := []ParseJob{
		{ID: "first", Parse: func(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
			time.Sleep(20 * time.Millisecond)
			return ParsedArtifact{Title: input.ID}, nil
		}},
		{ID: "second", Parse: testArtifact},
	}
	results := NewWorkerPool(2).ProcessAll(context.Background(), jobs)
	if len(results) != 2 || results[0].JobID != "first" || results[1].JobID != "second" {
		t.Fatalf("results are not input ordered: %+v", results)
	}
}
