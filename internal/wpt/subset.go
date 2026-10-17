package wpt

import (
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"
)

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
	if !fs.ValidPath(tc.Path) || strings.ContainsAny(tc.Path, "\\?#\x00") || !strings.HasSuffix(tc.Path, ".html") || len(tc.Path) > 1024 {
		return fmt.Errorf("wpt TestCase: Path must identify a canonical local HTML asset")
	}
	if !utf8.ValidString(tc.Name) || !utf8.ValidString(tc.CapabilityID) || len(tc.Name) > 4096 || len(tc.CapabilityID) > 256 {
		return fmt.Errorf("wpt TestCase: identity is oversized or invalid UTF-8")
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
	if len(s.Tests) > 1024 || len(s.Revision) > 256 || len(s.OriginURL) > 4096 {
		return fmt.Errorf("wpt Subset: metadata or test count exceeds limit")
	}
	identities := make(map[string]map[string]bool)
	for i, tc := range s.Tests {
		if err := tc.Validate(); err != nil {
			return fmt.Errorf("wpt Subset test %d: %w", i, err)
		}
		if identities[tc.Path] == nil {
			identities[tc.Path] = make(map[string]bool)
		}
		if identities[tc.Path][tc.Name] {
			return fmt.Errorf("wpt Subset: duplicate test %q/%q", tc.Path, tc.Name)
		}
		identities[tc.Path][tc.Name] = true
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
