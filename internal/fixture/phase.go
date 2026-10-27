package fixture

import (
	"errors"
	"fmt"
	"time"
)

// RunPhase names one step of a native corpus run, so a failure, and above all
// a deadline that expires, identifies the step it happened in.
type RunPhase string

// The phases of a Chromium corpus run in execution order.
const (
	PhaseLaunch      RunPhase = "browser launch"
	PhaseContext     RunPhase = "browser context"
	PhasePage        RunPhase = "page"
	PhaseTraceStart  RunPhase = "trace start"
	PhaseNavigation  RunPhase = "navigation"
	PhaseReadiness   RunPhase = "document readiness"
	PhaseObservation RunPhase = "observation"
	PhaseTraceStop   RunPhase = "trace stop"
	PhaseClose       RunPhase = "close"
)

// PhaseError is the failure of one run phase with the time elapsed since its
// owning run began. errors.Is and errors.As reach the cause, so a deadline stays
// recognizable as context.DeadlineExceeded.
type PhaseError struct {
	Phase   RunPhase
	Elapsed time.Duration
	Err     error
}

func (err *PhaseError) Error() string {
	return fmt.Sprintf("%s failed after %s: %v", err.Phase, err.Elapsed.Round(time.Millisecond), err.Err)
}

func (err *PhaseError) Unwrap() error { return err.Err }

// PhaseFailure wraps err with its phase and the time since started; nil stays
// nil. An error that already names a phase keeps that innermost phase.
func PhaseFailure(phase RunPhase, started time.Time, err error) error {
	if err == nil {
		return nil
	}
	var named *PhaseError
	if errors.As(err, &named) {
		return err
	}
	return &PhaseError{Phase: phase, Elapsed: time.Since(started), Err: err}
}
