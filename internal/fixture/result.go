package fixture

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

// CrossResult is the observable evidence a runner can return for a fixture.
type CrossResult struct {
	FixtureManifest *fixtureasset.Manifest `json:"fixture_manifest,omitempty"`
	URL             string
	StatusCode      int
	Title           string
	Text            string
	HTML            string
	Markdown        string
	EvalResult      string
	EvalErr         string
	Links           []string
}

// ValidateCrossResult retains every declared semantic requirement.
func ValidateCrossResult(sc Scenario, got CrossResult) error {
	if err := sc.Expect.Validate(); err != nil {
		return err
	}
	wantStatus := sc.Expect.Status
	if wantStatus == 0 {
		wantStatus = 200
	}
	var failures []error
	if got.StatusCode != wantStatus {
		failures = append(failures, fmt.Errorf("status = %d, want %d", got.StatusCode, wantStatus))
	}
	if sc.Expect.Title != "" && got.Title != sc.Expect.Title {
		failures = append(failures, fmt.Errorf("title = %q, want %q", got.Title, sc.Expect.Title))
	}
	for _, expected := range sc.Expect.Contains {
		if !strings.Contains(got.Text, expected) {
			failures = append(failures, fmt.Errorf("text missing %q", expected))
		}
	}
	for _, forbidden := range sc.Expect.NotContains {
		if strings.Contains(got.Text, forbidden) {
			failures = append(failures, fmt.Errorf("text contains %q", forbidden))
		}
	}
	if sc.Expect.URL != "" && !strings.Contains(got.URL, sc.Expect.URL) {
		failures = append(failures, fmt.Errorf("url = %q, want containing %q", got.URL, sc.Expect.URL))
	}
	if sc.Expect.Eval != "" {
		if got.EvalErr != "" {
			failures = append(failures, fmt.Errorf("eval %q error: %s", sc.Expect.Eval, got.EvalErr))
		}
		if !strings.Contains(got.EvalResult, sc.Expect.EvalContains) {
			failures = append(failures, fmt.Errorf("eval %q = %q, want containing %q", sc.Expect.Eval, got.EvalResult, sc.Expect.EvalContains))
		}
	}
	return errors.Join(failures...)
}
