package bridge

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorType classifies an error for agent consumption (spec L4265).
type ErrorType string

const (
	ErrorTypeElementNotVisible ErrorType = "ELEMENT_NOT_VISIBLE"
	ErrorTypeIntercept         ErrorType = "INTERCEPT"
	ErrorTypeTimeout           ErrorType = "TIMEOUT"
	ErrorTypeStrictMode        ErrorType = "STRICT_MODE"
	ErrorTypeNotFound          ErrorType = "NOT_FOUND"
	ErrorTypeStaleElement      ErrorType = "STALE_ELEMENT"
)

// ErrorContext carries the surrounding facts of a failed action so the
// mapper can produce an agent-actionable message.
type ErrorContext struct {
	ElementType string
	Selector    string
	URL         string
	Action      string
}

// AIErrorMessage is an AI-friendly error surface: every field is something
// an autonomous agent can reason about to choose its next action.
type AIErrorMessage struct {
	Code            ErrorType
	Message         string
	SuggestedAction string
	Element         string
	Recoverable     bool
}

// Error implements the error interface so AIErrorMessage flows through
// normal Go error plumbing.
func (m AIErrorMessage) Error() string {
	return FormatAIError(m)
}

// sentinel error substrings used by MapToAIError to classify raw errors.
const (
	visErrNotVisible = "not visible"
	visErrInvisible  = "invisible"
	visErrIntercept  = "intercept"
	visErrTimeout    = "timeout"
	visErrTimedOut   = "timed out"
	visErrStrict     = "strict mode"
	visErrStrictFail = "strict mode violation"
	visErrNotFound   = "not found"
	visErrNoUnit     = "no node"
	visErrStale      = "stale"
	visErrDetached   = "detached"
)

// MapToAIError maps a raw error to an agent-actionable AIErrorMessage using
// the supplied context. Unknown errors are still mapped to a recoverable
// generic message so the agent always gets a structured surface.
func MapToAIError(err error, ctx ErrorContext) AIErrorMessage {
	if err == nil {
		return AIErrorMessage{
			Code:        ErrorTypeTimeout,
			Message:     "nil error mapped",
			Recoverable: false,
		}
	}

	msg := err.Error()
	lower := strings.ToLower(msg)
	element := formatElement(ctx)

	switch {
	case strings.Contains(lower, visErrNotVisible) || strings.Contains(lower, visErrInvisible):
		return AIErrorMessage{
			Code:            ErrorTypeElementNotVisible,
			Message:         fmt.Sprintf("%s is not visible on page %s", element, ctx.URL),
			SuggestedAction: "Scroll element into view or wait for visibility.",
			Element:         element,
			Recoverable:     true,
		}

	case strings.Contains(lower, visErrIntercept):
		return AIErrorMessage{
			Code:            ErrorTypeIntercept,
			Message:         fmt.Sprintf("%s is intercepted by another element on page %s", element, ctx.URL),
			SuggestedAction: "Dismiss overlay, scroll, or wait for the intercepting element to clear.",
			Element:         element,
			Recoverable:     true,
		}

	case strings.Contains(lower, visErrTimeout) || strings.Contains(lower, visErrTimedOut):
		return AIErrorMessage{
			Code:            ErrorTypeTimeout,
			Message:         fmt.Sprintf("Action %q timed out on page %s", ctx.Action, ctx.URL),
			SuggestedAction: "Increase wait timeout or retry after checking page load state.",
			Element:         element,
			Recoverable:     true,
		}

	case strings.Contains(lower, visErrStrict) || strings.Contains(lower, visErrStrictFail):
		return AIErrorMessage{
			Code:            ErrorTypeStrictMode,
			Message:         fmt.Sprintf("Strict mode violation: %s matched multiple elements on page %s", element, ctx.URL),
			SuggestedAction: "Refine selector to match exactly one element or disable strict mode.",
			Element:         element,
			Recoverable:     true,
		}

	case strings.Contains(lower, visErrNotFound) || strings.Contains(lower, visErrNoUnit):
		return AIErrorMessage{
			Code:            ErrorTypeNotFound,
			Message:         fmt.Sprintf("%s was not found on page %s", element, ctx.URL),
			SuggestedAction: "Verify selector, wait for navigation, or retry with an alternate selector.",
			Element:         element,
			Recoverable:     true,
		}

	case strings.Contains(lower, visErrStale) || strings.Contains(lower, visErrDetached):
		return AIErrorMessage{
			Code:            ErrorTypeStaleElement,
			Message:         fmt.Sprintf("%s is stale or detached from the DOM on page %s", element, ctx.URL),
			SuggestedAction: "Re-query the element and retry the action.",
			Element:         element,
			Recoverable:     true,
		}
	}

	return AIErrorMessage{
		Code:            ErrorTypeTimeout,
		Message:         fmt.Sprintf("Unclassified error during action %q on page %s: %s", ctx.Action, ctx.URL, msg),
		SuggestedAction: "Inspect page state and retry; if persistent, restart the browser session.",
		Element:         element,
		Recoverable:     true,
	}
}

// formatElement renders a human-readable element description from context.
func formatElement(ctx ErrorContext) string {
	if ctx.ElementType != "" && ctx.Selector != "" {
		return fmt.Sprintf("%s '%s'", ctx.ElementType, ctx.Selector)
	}
	if ctx.Selector != "" {
		return fmt.Sprintf("Element '%s'", ctx.Selector)
	}
	if ctx.ElementType != "" {
		return ctx.ElementType
	}
	return "Element"
}

// FormatAIError renders an AIErrorMessage in the canonical agent-readable
// format: "Error [CODE]: <message>. Suggested action: <action>."
func FormatAIError(m AIErrorMessage) string {
	if m.SuggestedAction == "" {
		return fmt.Sprintf("Error [%s]: %s.", m.Code, m.Message)
	}
	return fmt.Sprintf("Error [%s]: %s. Suggested action: %s", m.Code, m.Message, m.SuggestedAction)
}

// MapToAIErrorWithRetry wraps MapToAIError and records the attempt count in
// the message so agents can reason about retry exhaustion.
func MapToAIErrorWithRetry(err error, ctx ErrorContext, attempt int) AIErrorMessage {
	m := MapToAIError(err, ctx)
	if attempt > 0 {
		m.Message = fmt.Sprintf("%s (attempt %d)", m.Message, attempt)
	}
	return m
}

// IsRecoverable reports whether a raw error maps to a recoverable AI error.
func IsRecoverable(err error, ctx ErrorContext) bool {
	return MapToAIError(err, ctx).Recoverable
}

// NewAIError constructs an AIErrorMessage directly from a code and context.
func NewAIError(code ErrorType, ctx ErrorContext, recoverable bool) AIErrorMessage {
	element := formatElement(ctx)
	var message, action string
	switch code {
	case ErrorTypeElementNotVisible:
		message = fmt.Sprintf("%s is not visible on page %s", element, ctx.URL)
		action = "Scroll element into view or wait for visibility."
	case ErrorTypeIntercept:
		message = fmt.Sprintf("%s is intercepted by another element on page %s", element, ctx.URL)
		action = "Dismiss overlay, scroll, or wait for the intercepting element to clear."
	case ErrorTypeTimeout:
		message = fmt.Sprintf("Action %q timed out on page %s", ctx.Action, ctx.URL)
		action = "Increase wait timeout or retry after checking page load state."
	case ErrorTypeStrictMode:
		message = fmt.Sprintf("Strict mode violation: %s matched multiple elements on page %s", element, ctx.URL)
		action = "Refine selector to match exactly one element or disable strict mode."
	case ErrorTypeNotFound:
		message = fmt.Sprintf("%s was not found on page %s", element, ctx.URL)
		action = "Verify selector, wait for navigation, or retry with an alternate selector."
	case ErrorTypeStaleElement:
		message = fmt.Sprintf("%s is stale or detached from the DOM on page %s", element, ctx.URL)
		action = "Re-query the element and retry the action."
	default:
		message = fmt.Sprintf("Unclassified error during action %q on page %s", ctx.Action, ctx.URL)
		action = "Inspect page state and retry; if persistent, restart the browser session."
	}
	return AIErrorMessage{
		Code:            code,
		Message:         message,
		SuggestedAction: action,
		Element:         element,
		Recoverable:     recoverable,
	}
}

// AsAIError unwraps err chains to find an AIErrorMessage, returning ok=true
// when one is present.
func AsAIError(err error) (AIErrorMessage, bool) {
	var m AIErrorMessage
	if errors.As(err, &m) {
		return m, true
	}
	return AIErrorMessage{}, false
}
