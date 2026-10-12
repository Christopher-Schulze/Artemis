package wpt

import (
	"testing"
)

func TestOutcomeValues(t *testing.T) {
	for _, o := range []Outcome{OutcomePass, OutcomeFail, OutcomeSkip, OutcomeError} {
		if o == "" {
			t.Fatal("Outcome constant must not be empty")
		}
	}
}

func TestDefaultSubsetWellFormed(t *testing.T) {
	sub := DefaultSubset()
	if sub.Revision == "" {
		t.Fatal("DefaultSubset Revision must be pinned")
	}
	if sub.OriginURL == "" {
		t.Fatal("DefaultSubset OriginURL must be set")
	}
	if len(sub.Tests) == 0 {
		t.Fatal("DefaultSubset must contain at least one TestCase")
	}
	seen := make(map[string]bool)
	for i, tc := range sub.Tests {
		if tc.Path == "" {
			t.Fatalf("test case %d: Path is empty", i)
		}
		if tc.Name == "" {
			t.Fatalf("test case %d: Name is empty", i)
		}
		if tc.Expected == "" {
			t.Fatalf("test case %d: Expected outcome is empty", i)
		}
		if tc.CapabilityID == "" {
			t.Fatalf("test case %d: CapabilityID is empty", i)
		}
		key := tc.Path + "|" + tc.Name
		if seen[key] {
			t.Fatalf("test case %d: duplicate test path/name %q", i, key)
		}
		seen[key] = true
	}
}

func TestDefaultSubsetCapabilityMapping(t *testing.T) {
	for _, tc := range DefaultSubset().Tests {
		if tc.CapabilityID != "renderless.javascript" {
			t.Fatalf("TestCase %s/%s maps to unknown capability %q", tc.Path, tc.Name, tc.CapabilityID)
		}
	}
}

func TestOutcomeValid(t *testing.T) {
	for _, o := range []Outcome{OutcomePass, OutcomeFail, OutcomeSkip, OutcomeError} {
		if !o.Valid() {
			t.Fatalf("Outcome %q should be valid", o)
		}
	}
	if Outcome("UNKNOWN").Valid() {
		t.Fatal("Outcome UNKNOWN should not be valid")
	}
}

func TestTestCaseValidate(t *testing.T) {
	valid := TestCase{Path: "a.html", Name: "n", Expected: OutcomePass, CapabilityID: "renderless.javascript"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid TestCase failed validation: %v", err)
	}

	invalid := []TestCase{
		{Path: "", Name: "n", Expected: OutcomePass, CapabilityID: "renderless.javascript"},
		{Path: "a.html", Name: "", Expected: OutcomePass, CapabilityID: "renderless.javascript"},
		{Path: "a.html", Name: "n", Expected: Outcome("UNKNOWN"), CapabilityID: "renderless.javascript"},
		{Path: "a.html", Name: "n", Expected: OutcomePass, CapabilityID: ""},
	}
	for i, tc := range invalid {
		if err := tc.Validate(); err == nil {
			t.Fatalf("invalid TestCase %d should fail validation", i)
		}
	}
}

func TestSubsetValidate(t *testing.T) {
	if err := DefaultSubset().Validate(); err != nil {
		t.Fatalf("DefaultSubset validation failed: %v", err)
	}

	invalid := []Subset{
		{Revision: "", OriginURL: "https://wpt", Tests: []TestCase{{Path: "a", Name: "n", Expected: OutcomePass, CapabilityID: "c"}}},
		{Revision: "r", OriginURL: "", Tests: []TestCase{{Path: "a", Name: "n", Expected: OutcomePass, CapabilityID: "c"}}},
		{Revision: "r", OriginURL: "https://wpt", Tests: []TestCase{}},
	}
	for i, s := range invalid {
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid Subset %d should fail validation", i)
		}
	}
}
