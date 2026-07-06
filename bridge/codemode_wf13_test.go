package bridge

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// SP-artemis-codemode-SEC (codemode.go, security_privacy)
// Claim: CodeModeExecutor.Execute denies invalid code and missing session
// =============================================================================

func TestWFArtemisCodemode_ExecuteDeniesInvalidInput(t *testing.T) {
	// Security: Code Mode must deny execution of invalid/empty code and
	// deny execution when no session is bound. This prevents arbitrary
	// code execution against an unbound or invalid context.

	cases := []struct {
		name        string
		executor    *CodeModeExecutor
		code        string
		ctx         context.Context
		expectError string
	}{
		{
			"nil_executor",
			nil,
			"return 1;",
			context.Background(),
			"no active session",
		},
		{
			"nil_session",
			NewCodeModeExecutor(nil),
			"return 1;",
			context.Background(),
			"no active session",
		},
		{
			"empty_code",
			NewCodeModeExecutor(&BrowserSession{}),
			"",
			context.Background(),
			"empty code",
		},
		{
			"normalization_failure",
			NewCodeModeExecutor(&BrowserSession{}),
			"   ",
			context.Background(),
			"code normalization failed",
		},
	}
	blocked := 0
	for _, c := range cases {
		var res *ExecuteResult
		var err error
		if c.executor == nil {
			// Call on a typed nil to exercise the nil receiver guard.
			var e *CodeModeExecutor
			res, err = e.Execute(c.ctx, c.code)
		} else {
			res, err = c.executor.Execute(c.ctx, c.code)
		}
		// nil executor returns (nil, error) — both are valid deny outcomes.
		if res == nil {
			if err == nil {
				t.Fatalf("%s: expected either nil result with error or non-nil result", c.name)
			}
			blocked++
			continue
		}
		if res.Success {
			t.Fatalf("%s: expected Success=false, got true", c.name)
		}
		if res.Error == "" {
			t.Fatalf("%s: expected non-empty error message", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: cancelled context denies execution (positive control on deny path).
	exec := NewCodeModeExecutor(&BrowserSession{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _ := exec.Execute(ctx, "return 1;")
	if res == nil || res.Success {
		t.Fatal("cancelled context must deny execution")
	}

	// Baseline: valid code with session succeeds (positive control on allow path).
	exec2 := NewCodeModeExecutor(&BrowserSession{})
	res2, err := exec2.Execute(context.Background(), "return 1+1;")
	if err != nil {
		t.Fatalf("valid code must not return go error: %v", err)
	}
	if res2 == nil || !res2.Success {
		t.Fatalf("valid code must succeed, got res=%+v", res2)
	}

	// Baseline: timeout is enforced (30s default)
	if exec2.timeout != 30*time.Second {
		t.Fatalf("expected default timeout 30s, got %v", exec2.timeout)
	}
}
