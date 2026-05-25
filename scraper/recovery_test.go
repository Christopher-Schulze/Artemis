package scraper

import (
	"context"
	"errors"
	"testing"
)

func TestScraperRecoveryChain(t *testing.T) {
	var order []RecoveryStep
	exec := &RecoveryExecutor{
		Backoff: func(ctx context.Context, max int) error {
			order = append(order, StepBackoff)
			return errors.New("backoff")
		},
		Rotate: func(ctx context.Context) error {
			order = append(order, StepRotateProxy)
			return errors.New("rotate")
		},
		Static: func(ctx context.Context, url string) error {
			order = append(order, StepStaticFetch)
			return nil
		},
		Render: func(ctx context.Context, url string) error {
			order = append(order, StepEngineRender)
			return nil
		},
	}
	step, err := exec.Run(context.Background(), "https://example.com", DefaultRecoveryPlan())
	if err != nil {
		t.Fatal(err)
	}
	if step != StepStaticFetch {
		t.Fatalf("expected static success, got %s", step)
	}
	want := []RecoveryStep{StepBackoff, StepRotateProxy, StepStaticFetch}
	if len(order) != len(want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
}
