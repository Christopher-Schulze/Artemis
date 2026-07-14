package wpt

// Outcome is the expected result for a WPT test.
type Outcome string

const (
	OutcomePass  Outcome = "PASS"
	OutcomeFail  Outcome = "FAIL"
	OutcomeSkip  Outcome = "SKIP"
	OutcomeError Outcome = "ERROR"
)

// TestCase identifies a single WPT subtest by its source path and test name.
type TestCase struct {
	Path         string
	Name         string
	Expected     Outcome
	CapabilityID string
}

// Subset is a pinned, bounded collection of WPT test cases.
type Subset struct {
	Revision  string
	OriginURL string
	Tests     []TestCase
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
