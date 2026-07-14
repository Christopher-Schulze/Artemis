package fixture

import (
	"testing"
)

func TestScenariosByKindFilters(t *testing.T) {
	scenarios := DefaultScenarios()
	if len(scenarios) == 0 {
		t.Fatal("DefaultScenarios must not be empty")
	}

	html := ScenariosByKind(scenarios, KindHTML)
	if len(html) == 0 {
		t.Fatal("expected at least one HTML scenario")
	}
	for _, sc := range html {
		if sc.Kind != KindHTML {
			t.Fatalf("ScenariosByKind returned wrong kind %q for %s", sc.Kind, sc.ID)
		}
	}

	empty := ScenariosByKind(scenarios, KindUnknown)
	if len(empty) != 0 {
		t.Fatalf("expected no scenarios for KindUnknown, got %d", len(empty))
	}
}

func TestScenarioByID(t *testing.T) {
	sc := ScenarioByID("html-001")
	if sc == nil {
		t.Fatal("ScenarioByID returned nil for html-001")
	}
	if sc.ID != "html-001" {
		t.Fatalf("ID = %q, want html-001", sc.ID)
	}

	if ScenarioByID("does-not-exist") != nil {
		t.Fatal("ScenarioByID should return nil for unknown ID")
	}
}

func TestScenariosByPathSorted(t *testing.T) {
	scenarios := DefaultScenarios()
	sorted := ScenariosByPath(scenarios)
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Path < sorted[i-1].Path {
			t.Fatalf("ScenariosByPath not sorted: %q before %q", sorted[i-1].Path, sorted[i].Path)
		}
	}
}

func TestDefaultScenariosExpectHasDefaults(t *testing.T) {
	for _, sc := range DefaultScenarios() {
		if sc.ID == "" {
			t.Fatal("scenario missing ID")
		}
		if sc.Path == "" {
			t.Fatalf("scenario %s missing Path", sc.ID)
		}
		if sc.Status != 0 && (sc.Status < 100 || sc.Status > 599) {
			t.Fatalf("scenario %s has invalid HTTP Status %d", sc.ID, sc.Status)
		}
		if sc.Kind == "" {
			t.Fatalf("scenario %s missing Kind", sc.ID)
		}
	}
}
