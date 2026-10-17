package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCPUPlacementUsesOnlyPermittedAndGrantedCPUs(t *testing.T) {
	for _, test := range []struct {
		name    string
		allowed []int
		granted int
		browser []int
		worker  []int
	}{
		{"eight", []int{7, 6, 5, 4, 3, 2, 1, 0}, 8, []int{0, 1, 2, 3}, []int{4, 5, 6, 7}},
		{"sparse", []int{19, 8, 16, 3}, 4, []int{3, 8}, []int{16, 19}},
		{"budget", []int{2, 4, 6, 8, 10, 12}, 2, []int{2}, []int{4}},
		{"cpuset", []int{8, 12}, 8, []int{8}, []int{12}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := append([]int(nil), test.allowed...)
			plan, err := CompileCPUPlacement(test.allowed, test.granted)
			if err != nil || !reflect.DeepEqual(plan.Browser, test.browser) || !reflect.DeepEqual(plan.Worker, test.worker) || !reflect.DeepEqual(test.allowed, original) {
				t.Fatalf("placement escaped cpuset/grant or changed input: %+v %v", plan, err)
			}
			plan.Browser[0] = 0
			if !reflect.DeepEqual(test.allowed, original) {
				t.Fatal("placement shares caller storage")
			}
		})
	}
}

func TestCPUPlacementRejectsInvalidAdmission(t *testing.T) {
	for _, test := range []struct {
		name  string
		cpus  []int
		grant int
	}{
		{"missing", nil, 8},
		{"single", []int{1}, 8},
		{"no_grant", []int{0, 1}, 0},
		{"single_grant", []int{0, 1}, 1},
		{"negative_id", []int{-1, 2}, 2},
		{"duplicate", []int{2, 2}, 2},
		{"oversized_id", []int{1, maxAffinityCPUs}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, err := CompileCPUPlacement(test.cpus, test.grant)
			if err == nil || len(plan.Browser) != 0 || len(plan.Worker) != 0 {
				t.Fatalf("invalid placement admitted: %+v %v", plan, err)
			}
		})
	}
}

func TestCPUAffinityRejectsBeforeExecutingCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
		cpus []int
	}{
		{"missing_context", nil, []int{0}},
		{"canceled", ctx, []int{0}},
		{"negative", t.Context(), []int{-1}},
		{"duplicate", t.Context(), []int{0, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			evidence, err := RunCPUAffinity(test.ctx, test.cpus, func(context.Context) error { called = true; return errors.New("must not execute") })
			if err == nil || evidence.State == StateAvailable || called {
				t.Fatalf("rejected placement executed: %+v %v", evidence, err)
			}
		})
	}
}

func TestCPUAffinityUnrequestedPathExecutesActualCallback(t *testing.T) {
	want := errors.New("actual operation failed")
	called := false
	evidence, err := RunCPUAffinity(t.Context(), nil, func(ctx context.Context) error {
		called = ctx == t.Context()
		return want
	})
	if !called || !errors.Is(err, want) || evidence.State != StateUnavailable || evidence.Value != 0 {
		t.Fatalf("ordinary callback altered: %+v %v", evidence, err)
	}
}

func TestCPUAffinityOrdinaryCallbackCannotHideCancellationOrPanic(t *testing.T) {
	for _, test := range []struct {
		name  string
		panic bool
	}{
		{"cancellation", false},
		{"panic", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err := RunCPUAffinity(ctx, nil, func(context.Context) error {
				if test.panic {
					panic("controlled callback fault")
				}
				cancel()
				return nil
			})
			if err == nil || (!test.panic && !errors.Is(err, context.Canceled)) {
				t.Fatalf("failed callback reported success: %v", err)
			}
		})
	}
}
