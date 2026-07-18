package process

import (
	"errors"
	"fmt"
)

// ErrorCode classifies Chromium process failures without string matching.
type ErrorCode string

const (
	ErrorInvalidConfig  ErrorCode = "invalid_config"
	ErrorBinaryNotFound ErrorCode = "binary_not_found"
	ErrorLaunchFailed   ErrorCode = "launch_failed"
	ErrorLaunchTimeout  ErrorCode = "launch_timeout"
	ErrorBrowserCrash   ErrorCode = "browser_crash"
	ErrorCancelled      ErrorCode = "cancelled"
	ErrorResourceBudget ErrorCode = "resource_budget_exceeded"
	ErrorDiagnostics    ErrorCode = "diagnostics_unavailable"
)

// Error is a typed Chromium process failure.
type Error struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "artemis chromium process error"
	}
	return fmt.Sprintf("artemis chromium process: %s: %s: %v", e.Op, e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsCode reports whether err contains a process Error with code.
func IsCode(err error, code ErrorCode) bool {
	var processErr *Error
	return errors.As(err, &processErr) && processErr.Code == code
}
