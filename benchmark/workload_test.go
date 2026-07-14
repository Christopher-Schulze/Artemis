package benchmark

import "testing"

func TestNewWorkloadDefaults(t *testing.T) {
	s := Scenario{ID: "s1", EngineMode: ModeChromium}
	w := NewWorkload(s, "", "", "", 0)
	if w.ScenarioID != "s1" {
		t.Errorf("scenario id = %s", w.ScenarioID)
	}
	if w.EngineMode != ModeChromium {
		t.Errorf("engine mode = %s, want chromium", w.EngineMode)
	}
	if w.Locality != LocalityLocal {
		t.Errorf("locality = %s", w.Locality)
	}
	if w.Warmth != WarmthCold {
		t.Errorf("warmth = %s", w.Warmth)
	}
	if w.Iterations != 5 {
		t.Errorf("iterations = %d", w.Iterations)
	}
}

func TestWorkloadKeyAndString(t *testing.T) {
	w := Workload{ScenarioID: "s1", EngineMode: ModeRenderless, Locality: LocalityLocal, Warmth: WarmthWarm, Iterations: 3}
	if w.Key() != "s1-renderless-local-warm" {
		t.Errorf("key = %s", w.Key())
	}
	if w.String() != "s1/renderless/local/warm" {
		t.Errorf("string = %s", w.String())
	}
}

func TestWorkloadIsValid(t *testing.T) {
	if ok := (Workload{ScenarioID: "s1", EngineMode: ModeRenderless, Locality: LocalityLocal, Warmth: WarmthCold, Iterations: 1}.IsValid()); !ok {
		t.Error("expected valid workload")
	}
	if ok := (Workload{ScenarioID: "s1", EngineMode: "", Locality: LocalityLocal, Warmth: WarmthCold, Iterations: 1}.IsValid()); ok {
		t.Error("expected invalid because engine mode empty")
	}
}
