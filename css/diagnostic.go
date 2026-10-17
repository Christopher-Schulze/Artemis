package css

import (
	"errors"
	"fmt"
)

// DiagnosticCode identifies an actual unsupported, malformed or bounded CSS
// input. It never converts partial parsing into computed-style proof.
type DiagnosticCode string

const (
	DiagnosticSyntax      DiagnosticCode = "css_syntax"
	DiagnosticUnsupported DiagnosticCode = "css_unsupported"
	DiagnosticBudget      DiagnosticCode = "css_budget"
	DiagnosticLoad        DiagnosticCode = "css_load"
)

// Diagnostic contains fixed reason text and a byte offset, never source values.
type Diagnostic struct {
	Code   DiagnosticCode `json:"code"`
	Offset int            `json:"offset"`
	Reason string         `json:"reason"`
	Cause  error          `json:"-"`
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s at byte %d: %s", d.Code, d.Offset, d.Reason)
}

// Unwrap preserves cancellation, policy and resource-budget identity without
// interpolating potentially sensitive transport details into the diagnostic.
func (d Diagnostic) Unwrap() error { return d.Cause }

// IsDiagnostic recognizes typed CSS failures through execution/routing wrappers.
func IsDiagnostic(err error) bool {
	var value Diagnostic
	var pointer *Diagnostic
	return errors.As(err, &value) || errors.As(err, &pointer)
}

// IsBudgetFailure checks every joined cause. Excessive wrapper graphs deny
// retry as well; a later budget diagnostic cannot hide behind an earlier one.
func IsBudgetFailure(err error) bool {
	pending := []error{err}
	for work := 0; len(pending) > 0; work++ {
		if work >= 256 {
			return true
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current == nil {
			continue
		}
		switch diagnostic := current.(type) {
		case Diagnostic:
			if diagnostic.Code == DiagnosticBudget {
				return true
			}
		case *Diagnostic:
			if diagnostic == nil || diagnostic.Code == DiagnosticBudget {
				return true
			}
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			causes := wrapped.Unwrap()
			if len(causes) > 256-len(pending) {
				return true
			}
			pending = append(pending, causes...)
		case interface{ Unwrap() error }:
			pending = append(pending, wrapped.Unwrap())
		}
	}
	return false
}

// ExecutionFailure marks a CSS failure observed after script execution began.
// Retrying that operation requires effect/replay authority; a CSS diagnostic
// alone cannot prove that scripts made no external changes.
type ExecutionFailure struct{ Cause error }

func (e *ExecutionFailure) Error() string { return "CSS failure after script execution" }

func (e *ExecutionFailure) Unwrap() error { return e.Cause }

// MaxStylesheetBytes bounds each admitted CSS source before lexer allocation.
const MaxStylesheetBytes = 8 << 20
const maxCSSTokens = 65536
const maxCSSNesting = 128
const maxCSSRules = 8192
const maxCSSSelectorBytes = 16 << 10
const maxCSSSelectors = 64
const maxCSSValueBytes = 64 << 10

func cssDiagnostic(code DiagnosticCode, offset int, reason string) *Diagnostic {
	return &Diagnostic{Code: code, Offset: offset, Reason: reason}
}
