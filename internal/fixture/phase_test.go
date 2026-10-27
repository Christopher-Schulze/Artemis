package fixture

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPhaseFailureNamesThePhaseAndKeepsTheCause(t *testing.T) {
	if PhaseFailure(PhaseNavigation, time.Now(), nil) != nil {
		t.Fatal("a nil error gained a phase")
	}
	started := time.Now().Add(-1500 * time.Millisecond)
	cause := fmt.Errorf("navigate: %w", context.DeadlineExceeded)
	err := PhaseFailure(PhaseNavigation, started, cause)
	var phase *PhaseError
	if !errors.As(err, &phase) || phase.Phase != PhaseNavigation || phase.Elapsed < 1500*time.Millisecond {
		t.Fatalf("phase error = %#v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, cause) {
		t.Fatalf("the cause chain was lost: %v", err)
	}
	if text := err.Error(); !strings.HasPrefix(text, "navigation failed after 1.5") || !strings.HasSuffix(text, "context deadline exceeded") {
		t.Fatalf("message = %q", text)
	}
	outer := PhaseFailure(PhaseObservation, started, fmt.Errorf("scenario: %w", err))
	if !errors.As(outer, &phase) || phase.Phase != PhaseNavigation {
		t.Fatalf("an outer step replaced the innermost phase: %v", outer)
	}
	joined := errors.Join(err, errors.New("close: broken pipe"))
	if again := PhaseFailure(PhaseClose, started, joined); !errors.As(again, &phase) || phase.Phase != PhaseNavigation {
		t.Fatalf("a joined phase error lost its phase: %v", again)
	}
}

// TestRunNativeScenarioNamesTheFailedPhase runs a real Chromium page: a
// scenario whose owning context is already cancelled fails in the trace start
// phase with the cancellation preserved, and the same page then completes the
// scenario.
func TestRunNativeScenarioNamesTheFailedPhase(t *testing.T) {
	server := NewServer()
	defer server.Close()
	if err := server.RegisterHTML("/phase", "<html><body>phase observation</body></html>"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	native := &bridgeRunner{}
	defer native.stop(context.Background(), t)
	native.start(ctx, t, server)
	cancelled, cancelScenario := context.WithCancel(ctx)
	cancelScenario()
	scenario := Scenario{Expect: Expect{Status: 200, Contains: []string{"phase observation"}}}
	_, err := RunNativeScenario(cancelled, native.page, scenario, server.URL("/phase"))
	var phase *PhaseError
	if !errors.As(err, &phase) || phase.Phase != PhaseTraceStart || !errors.Is(phase, context.Canceled) {
		t.Fatalf("cancelled scenario = %v", err)
	}
	if _, err := RunNativeScenario(ctx, native.page, scenario, server.URL("/phase")); err != nil {
		t.Fatalf("control scenario: %v", err)
	}
}
