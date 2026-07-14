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
