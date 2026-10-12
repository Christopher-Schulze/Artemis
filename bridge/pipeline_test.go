package bridge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCDPPipelineAdd(t *testing.T) {
	p := NewCDPPipeline(10 * time.Second)
	p.AddTask("task1", func(ctx context.Context) (interface{}, error) {
		return "result1", nil
	})
	if p.Len() != 1 {
		t.Fatalf("expected 1 task, got %d", p.Len())
	}
}

func TestCDPPipelineRun(t *testing.T) {
	p := NewCDPPipeline(10 * time.Second)
	p.AddTask("task1", func(ctx context.Context) (interface{}, error) {
		return "result1", nil
	})
	p.AddTask("task2", func(ctx context.Context) (interface{}, error) {
		return "result2", nil
	})
	results := p.Run(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Value != "result1" {
		t.Fatalf("expected result1, got %v", results[0].Value)
	}
	if results[1].Value != "result2" {
		t.Fatalf("expected result2, got %v", results[1].Value)
	}
}

func TestCDPPipelineRunWithError(t *testing.T) {
	p := NewCDPPipeline(10 * time.Second)
	p.AddTask("task1", func(ctx context.Context) (interface{}, error) {
		return nil, errors.New("task1 error")
	})
	p.AddTask("task2", func(ctx context.Context) (interface{}, error) {
		return "result2", nil
	})
	results := p.Run(context.Background())
	if results[0].Error == nil {
		t.Fatal("expected error on task1")
	}
	if results[1].Value != "result2" {
		t.Fatal("expected task2 to still execute")
	}
}

func TestCDPPipelineRunParallel(t *testing.T) {
	p := NewCDPPipeline(10 * time.Second)
	p.AddTask("task1", func(ctx context.Context) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return "result1", nil
	})
	p.AddTask("task2", func(ctx context.Context) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return "result2", nil
	})
	start := time.Now()
	results := p.RunParallel(context.Background())
	elapsed := time.Since(start)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Parallel should be faster than sequential (100ms)
	if elapsed >= 90*time.Millisecond {
		t.Fatalf("expected parallel execution <90ms, got %v", elapsed)
	}
}

func TestCDPPipelineEmpty(t *testing.T) {
	p := NewCDPPipeline(10 * time.Second)
	results := p.Run(context.Background())
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestCDPPipelineNil(t *testing.T) {
	var p *CDPPipeline
	if p.Len() != 0 {
		t.Fatal("expected 0 for nil pipeline")
	}
	if p.Run(context.Background()) != nil {
		t.Fatal("expected nil for nil pipeline")
	}
}

func TestHasErrors(t *testing.T) {
	results := []CDPTaskResult{
		{Name: "task1", Error: nil},
		{Name: "task2", Error: errors.New("error")},
	}
	if !HasErrors(results) {
		t.Fatal("expected HasErrors true")
	}
	results[1].Error = nil
	if HasErrors(results) {
		t.Fatal("expected HasErrors false")
	}
}

func TestFirstError(t *testing.T) {
	results := []CDPTaskResult{
		{Name: "task1", Error: nil},
		{Name: "task2", Error: errors.New("error2")},
	}
	err := FirstError(results)
	if err == nil || err.Error() != "error2" {
		t.Fatalf("expected error2, got %v", err)
	}
}

func TestFirstErrorNone(t *testing.T) {
	results := []CDPTaskResult{
		{Name: "task1", Error: nil},
	}
	if err := FirstError(results); err != nil {
		t.Fatal("expected nil error")
	}
}

func TestTotalDuration(t *testing.T) {
	results := []CDPTaskResult{
		{Duration: 100 * time.Millisecond},
		{Duration: 200 * time.Millisecond},
	}
	total := TotalDuration(results)
	if total != 300*time.Millisecond {
		t.Fatalf("expected 300ms, got %v", total)
	}
}

func TestMandatoryBatchTypeString(t *testing.T) {
	tests := []struct {
		bt   MandatoryBatchType
		want string
	}{
		{BatchBoxModelStyleAttrs, "boxModel+computedStyle+attributes"},
		{BatchNavigateWaitSnapshot, "navigate+waitVisible+snapshot"},
		{BatchDOMQueries, "dom_queries"},
		{BatchScreenshotMetrics, "screenshot+metrics"},
	}
	for _, tt := range tests {
		if got := tt.bt.String(); got != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
	}
}

func TestValidateBatchBoxModel(t *testing.T) {
	err := ValidateBatch(BatchBoxModelStyleAttrs, 3)
	if err != nil {
		t.Fatalf("expected no error for 3 tasks, got %v", err)
	}
	err = ValidateBatch(BatchBoxModelStyleAttrs, 2)
	if err == nil {
		t.Fatal("expected error for 2 tasks (needs 3)")
	}
}

func TestValidateBatchNavigate(t *testing.T) {
	err := ValidateBatch(BatchNavigateWaitSnapshot, 3)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateBatch(BatchNavigateWaitSnapshot, 2)
	if err == nil {
		t.Fatal("expected error for 2 tasks")
	}
}

func TestValidateBatchDOMQueries(t *testing.T) {
	err := ValidateBatch(BatchDOMQueries, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateBatch(BatchDOMQueries, 0)
	if err == nil {
		t.Fatal("expected error for 0 tasks")
	}
}

func TestValidateBatchScreenshot(t *testing.T) {
	err := ValidateBatch(BatchScreenshotMetrics, 2)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateBatch(BatchScreenshotMetrics, 1)
	if err == nil {
		t.Fatal("expected error for 1 task")
	}
}

func TestValidateBatchUnknown(t *testing.T) {
	err := ValidateBatch(MandatoryBatchType(99), 1)
	if err == nil {
		t.Fatal("expected error for unknown batch type")
	}
}

func TestNewMandatoryBatch(t *testing.T) {
	tasks := []CDPTask{
		{Name: "boxModel", Execute: func(ctx context.Context) (interface{}, error) { return "box", nil }},
		{Name: "computedStyle", Execute: func(ctx context.Context) (interface{}, error) { return "style", nil }},
		{Name: "attributes", Execute: func(ctx context.Context) (interface{}, error) { return "attrs", nil }},
	}
	p := NewMandatoryBatch(BatchBoxModelStyleAttrs, tasks...)
	if p.Len() != 3 {
		t.Fatalf("expected 3 tasks, got %d", p.Len())
	}
	results := p.Run(context.Background())
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestCDPPipelineDefaultTimeout(t *testing.T) {
	p := NewCDPPipeline(0)
	if p.timeout != 30*time.Second {
		t.Fatalf("expected 30s default, got %v", p.timeout)
	}
}

func TestCDPPipelineContextTimeout(t *testing.T) {
	p := NewCDPPipeline(50 * time.Millisecond)
	p.AddTask("slow", func(ctx context.Context) (interface{}, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return "done", nil
		}
	})
	results := p.Run(context.Background())
	if results[0].Error == nil {
		t.Fatal("expected timeout error")
	}
}
