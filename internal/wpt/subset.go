package wpt

import "fmt"

// Outcome is the expected result for a WPT test.
type Outcome string

const (
	OutcomePass  Outcome = "PASS"
	OutcomeFail  Outcome = "FAIL"
	OutcomeSkip  Outcome = "SKIP"
	OutcomeError Outcome = "ERROR"
)

// Valid reports whether the outcome is a known value.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomePass, OutcomeFail, OutcomeSkip, OutcomeError:
		return true
	}
	return false
}

// TestCase identifies a single WPT subtest by its source path and test name.
type TestCase struct {
	Path         string
	Name         string
	Expected     Outcome
	CapabilityID string
}

// Validate returns an error if the test case is not self-consistent.
func (tc TestCase) Validate() error {
	if tc.Path == "" {
		return fmt.Errorf("wpt TestCase: Path is empty")
	}
	if tc.Name == "" {
		return fmt.Errorf("wpt TestCase %q: Name is empty", tc.Path)
	}
	if !tc.Expected.Valid() {
		return fmt.Errorf("wpt TestCase %q: Expected %q is not a valid Outcome", tc.Name, tc.Expected)
	}
	if tc.CapabilityID == "" {
		return fmt.Errorf("wpt TestCase %q: CapabilityID is empty", tc.Name)
	}
	return nil
}

// Subset is a pinned, bounded collection of WPT test cases.
type Subset struct {
	Revision  string
	OriginURL string
	Tests     []TestCase
}

// Validate returns an error if the subset metadata or any test case is invalid.
func (s Subset) Validate() error {
	if s.Revision == "" {
		return fmt.Errorf("wpt Subset: Revision is empty")
	}
	if s.OriginURL == "" {
		return fmt.Errorf("wpt Subset: OriginURL is empty")
	}
	if len(s.Tests) == 0 {
		return fmt.Errorf("wpt Subset: Tests is empty")
	}
	for i, tc := range s.Tests {
		if err := tc.Validate(); err != nil {
			return fmt.Errorf("wpt Subset test %d: %w", i, err)
		}
	}
	return nil
}

// DefaultSubset returns the Artemis WPT subset pinned at the upstream WPT
// commit below. Tests are chosen from the local testdata mirror and mapped
// to the capability registry claims they exercise.
func DefaultSubset() Subset {
	return Subset{
		Revision:  "2c705104a295c48053eeddf7fe0170d790a4e853",
		OriginURL: "https://github.com/web-platform-tests/wpt",
		Tests: []TestCase{
			{
				Path:         "html/dom/elements/global-attributes/dataset.html",
				Name:         "HTML elements should have a .dataset",
				Expected:     OutcomePass,
				CapabilityID: "renderless.javascript",
			},
			{
				Path:         "html/dom/elements/global-attributes/dataset.html",
				Name:         "Should return 'value' if that's the value",
				Expected:     OutcomePass,
				CapabilityID: "renderless.javascript",
			},
			{
				Path:         "dom/nodes/CharacterData-data.html",
				Name:         "Text.data initial value",
				Expected:     OutcomePass,
				CapabilityID: "renderless.javascript",
			},
			{
				Path:         "dom/nodes/CharacterData-data.html",
				Name:         "Text.data = null",
				Expected:     OutcomePass,
				CapabilityID: "renderless.javascript",
			},
		},
	}
}
