package bridge

import (
	"errors"
	"strings"
	"testing"
)

func TestMapToAIErrorElementNotVisible(t *testing.T) {
	ctx := ErrorContext{ElementType: "Button", Selector: "#submit", URL: "https://example.com", Action: "click"}
	m := MapToAIError(errors.New("element is not visible"), ctx)
	if m.Code != ErrorTypeElementNotVisible {
		t.Fatalf("code=%s want %s", m.Code, ErrorTypeElementNotVisible)
	}
	if !m.Recoverable {
		t.Fatal("should be recoverable")
	}
	if !strings.Contains(m.Message, "Button '#submit'") {
		t.Fatalf("message=%q", m.Message)
	}
	if !strings.Contains(m.SuggestedAction, "Scroll") {
		t.Fatalf("action=%q", m.SuggestedAction)
	}
}

func TestMapToAIErrorInvisibleVariant(t *testing.T) {
	ctx := ErrorContext{ElementType: "div", Selector: ".x", URL: "https://example.com"}
	m := MapToAIError(errors.New("node invisible"), ctx)
	if m.Code != ErrorTypeElementNotVisible {
		t.Fatalf("code=%s", m.Code)
	}
}

func TestMapToAIErrorIntercept(t *testing.T) {
	ctx := ErrorContext{ElementType: "Button", Selector: "#go", URL: "https://example.com"}
	m := MapToAIError(errors.New("other element intercepts clicks"), ctx)
	if m.Code != ErrorTypeIntercept {
		t.Fatalf("code=%s", m.Code)
	}
	if !m.Recoverable {
		t.Fatal("should be recoverable")
	}
	if !strings.Contains(m.SuggestedAction, "overlay") {
		t.Fatalf("action=%q", m.SuggestedAction)
	}
}

func TestMapToAIErrorTimeout(t *testing.T) {
	ctx := ErrorContext{Action: "waitForSelector", URL: "https://example.com"}
	m := MapToAIError(errors.New("operation timed out"), ctx)
	if m.Code != ErrorTypeTimeout {
		t.Fatalf("code=%s", m.Code)
	}
	if !strings.Contains(m.Message, "waitForSelector") {
		t.Fatalf("message=%q", m.Message)
	}
}

func TestMapToAIErrorTimeoutWord(t *testing.T) {
	ctx := ErrorContext{Action: "click", URL: "https://example.com"}
	m := MapToAIError(errors.New("timeout 30000ms exceeded"), ctx)
	if m.Code != ErrorTypeTimeout {
		t.Fatalf("code=%s", m.Code)
	}
}

func TestMapToAIErrorStrictMode(t *testing.T) {
	ctx := ErrorContext{ElementType: "Button", Selector: "button", URL: "https://example.com"}
	m := MapToAIError(errors.New("strict mode violation: resolved to 3 elements"), ctx)
	if m.Code != ErrorTypeStrictMode {
		t.Fatalf("code=%s", m.Code)
	}
	if !strings.Contains(m.SuggestedAction, "Refine selector") {
		t.Fatalf("action=%q", m.SuggestedAction)
	}
}

func TestMapToAIErrorNotFound(t *testing.T) {
	ctx := ErrorContext{ElementType: "Input", Selector: "#email", URL: "https://example.com"}
	m := MapToAIError(errors.New("element not found in DOM"), ctx)
	if m.Code != ErrorTypeNotFound {
		t.Fatalf("code=%s", m.Code)
	}
	if !strings.Contains(m.Message, "Input '#email'") {
		t.Fatalf("message=%q", m.Message)
	}
}

func TestMapToAIErrorNoNodeVariant(t *testing.T) {
	ctx := ErrorContext{Selector: ".missing", URL: "https://example.com"}
	m := MapToAIError(errors.New("no node found for selector"), ctx)
	if m.Code != ErrorTypeNotFound {
		t.Fatalf("code=%s", m.Code)
	}
}

func TestMapToAIErrorStaleElement(t *testing.T) {
	ctx := ErrorContext{ElementType: "Button", Selector: "#submit", URL: "https://example.com"}
	m := MapToAIError(errors.New("element is stale"), ctx)
	if m.Code != ErrorTypeStaleElement {
		t.Fatalf("code=%s", m.Code)
	}
	if !strings.Contains(m.SuggestedAction, "Re-query") {
		t.Fatalf("action=%q", m.SuggestedAction)
	}
}

func TestMapToAIErrorDetachedVariant(t *testing.T) {
	ctx := ErrorContext{ElementType: "span", Selector: ".x", URL: "https://example.com"}
	m := MapToAIError(errors.New("node detached from document"), ctx)
	if m.Code != ErrorTypeStaleElement {
		t.Fatalf("code=%s", m.Code)
	}
}

func TestMapToAIErrorUnclassified(t *testing.T) {
	ctx := ErrorContext{Action: "click", URL: "https://example.com"}
	m := MapToAIError(errors.New("something weird happened"), ctx)
	if !m.Recoverable {
		t.Fatal("unclassified should be recoverable by default")
	}
	if !strings.Contains(m.Message, "Unclassified") {
		t.Fatalf("message=%q", m.Message)
	}
}

func TestFormatAIError(t *testing.T) {
	m := AIErrorMessage{
		Code:            ErrorTypeElementNotVisible,
		Message:         "Button '#submit' is not visible on page https://example.com",
		SuggestedAction: "Scroll element into view or wait for visibility.",
	}
	got := FormatAIError(m)
	want := "Error [ELEMENT_NOT_VISIBLE]: Button '#submit' is not visible on page https://example.com. Suggested action: Scroll element into view or wait for visibility."
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestFormatAIErrorNoSuggestion(t *testing.T) {
	m := AIErrorMessage{Code: ErrorTypeTimeout, Message: "timed out"}
	got := FormatAIError(m)
	if !strings.HasSuffix(got, "timed out.") {
		t.Fatalf("got=%q", got)
	}
}

func TestAIErrorMessageImplementsError(t *testing.T) {
	m := AIErrorMessage{Code: ErrorTypeNotFound, Message: "missing", SuggestedAction: "retry"}
	var err error = m
	if err.Error() != FormatAIError(m) {
		t.Fatal("Error() mismatch")
	}
}

func TestMapToAIErrorWithRetry(t *testing.T) {
	ctx := ErrorContext{Action: "click", URL: "https://example.com"}
	m := MapToAIErrorWithRetry(errors.New("timeout"), ctx, 3)
	if !strings.Contains(m.Message, "attempt 3") {
		t.Fatalf("message=%q", m.Message)
	}
}

func TestIsRecoverable(t *testing.T) {
	ctx := ErrorContext{URL: "https://example.com"}
	if !IsRecoverable(errors.New("not visible"), ctx) {
		t.Fatal("not visible should be recoverable")
	}
}

func TestNewAIErrorDirect(t *testing.T) {
	ctx := ErrorContext{ElementType: "Button", Selector: "#x", URL: "https://example.com"}
	m := NewAIError(ErrorTypeIntercept, ctx, true)
	if m.Code != ErrorTypeIntercept || !m.Recoverable {
		t.Fatalf("code=%s rec=%v", m.Code, m.Recoverable)
	}
	if !strings.Contains(m.Message, "intercepted") {
		t.Fatalf("message=%q", m.Message)
	}
}

func TestAsAIError(t *testing.T) {
	m := NewAIError(ErrorTypeTimeout, ErrorContext{URL: "https://example.com"}, true)
	wrapped := errors.Join(m, errors.New("ctx"))
	got, ok := AsAIError(wrapped)
	if !ok {
		t.Fatal("AsAIError should find AIErrorMessage in chain")
	}
	if got.Code != ErrorTypeTimeout {
		t.Fatalf("code=%s", got.Code)
	}
}

func TestFormatElementVariants(t *testing.T) {
	if got := formatElement(ErrorContext{Selector: ".x"}); got != "Element '.x'" {
		t.Fatalf("got=%q", got)
	}
	if got := formatElement(ErrorContext{ElementType: "Button"}); got != "Button" {
		t.Fatalf("got=%q", got)
	}
	if got := formatElement(ErrorContext{}); got != "Element" {
		t.Fatalf("got=%q", got)
	}
}
