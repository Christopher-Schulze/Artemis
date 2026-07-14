package wpt

import (
	"context"
	"testing"
)

func TestDefaultSubset(t *testing.T) {
	sub := DefaultSubset()
	if sub.Revision == "" {
		t.Fatal("DefaultSubset revision must be pinned")
	}
	if sub.OriginURL == "" {
		t.Fatal("DefaultSubset origin URL must be set")
	}
	if len(sub.Tests) == 0 {
		t.Fatal("DefaultSubset must contain at least one test case")
	}
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
	}
}

func TestRunner(t *testing.T) {
	r, err := NewRunner()
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	results, err := r.Run(context.Background(), DefaultSubset())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	sub := DefaultSubset()
	if len(results) != len(sub.Tests) {
		t.Fatalf("expected %d results, got %d", len(sub.Tests), len(results))
	}

	for i, res := range results {
		tc := sub.Tests[i]
		if res.Name != tc.Name || res.Path != tc.Path {
			t.Fatalf("result %d mismatch: got %s/%s want %s/%s", i, res.Path, res.Name, tc.Path, tc.Name)
		}
		if res.Status != string(tc.Expected) {
			t.Fatalf("%s/%s: expected %s, got %s (message: %s, harness: %s)", tc.Path, tc.Name, tc.Expected, res.Status, res.Message, res.HarnessStatus)
		}
		if res.HarnessStatus != "OK" {
			t.Fatalf("%s/%s: harness did not complete OK: %s", tc.Path, tc.Name, res.HarnessStatus)
		}
	}
}

func TestRunnerUnregisteredPath(t *testing.T) {
	r, err := NewRunner()
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	_, err = r.RunCase(context.Background(), TestCase{
		Path:     "not-in-testdata.html",
		Name:     "missing",
		Expected: OutcomeFail,
	})
	if err == nil {
		t.Fatal("expected error for unregistered WPT path")
	}
}
