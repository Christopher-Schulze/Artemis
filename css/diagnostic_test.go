package css

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCSSLoadDiagnosticRetainsCauseWithoutLeakingTransportText(t *testing.T) {
	cause := errors.New("secret transport details")
	diagnostic := Diagnostic{Code: DiagnosticLoad, Reason: "external stylesheet load failed", Cause: cause}
	for _, wrapped := range []error{fmt.Errorf("execution: %w", diagnostic), errors.Join(errors.New("route"), &diagnostic)} {
		if !errors.Is(wrapped, cause) || strings.Contains(wrapped.Error(), "secret") || !IsDiagnostic(wrapped) {
			t.Fatalf("load diagnostic lost typed cause or exposed raw transport text: %v", wrapped)
		}
	}
	if IsDiagnostic(nil) || IsDiagnostic(cause) {
		t.Fatal("ordinary or absent errors became CSS diagnostics")
	}
}

func TestCSSBudgetFailureChecksEveryCauseAndBoundsWrapperWork(t *testing.T) {
	deep := error(Diagnostic{Code: DiagnosticSyntax})
	for index := 0; index < 257; index++ {
		deep = fmt.Errorf("wrapper: %w", deep)
	}
	var absent *Diagnostic
	for _, test := range []struct {
		name   string
		cause  error
		denied bool
	}{
		{"nil", nil, false}, {"ordinary", errors.New("ordinary"), false},
		{"syntax", Diagnostic{Code: DiagnosticSyntax}, false},
		{"value budget", Diagnostic{Code: DiagnosticBudget}, true},
		{"joined budget", errors.Join(Diagnostic{Code: DiagnosticSyntax}, &Diagnostic{Code: DiagnosticBudget}), true},
		{"nested budget", Diagnostic{Code: DiagnosticLoad, Cause: &Diagnostic{Code: DiagnosticBudget}}, true},
		{"wrapper work limit", deep, true}, {"typed absent diagnostic", absent, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if actual := IsBudgetFailure(test.cause); actual != test.denied {
				t.Fatalf("budget denial=%v want=%v", actual, test.denied)
			}
		})
	}
}
