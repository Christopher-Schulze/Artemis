package benchmark

import (
	"fmt"
	"strings"
)

// Locality indicates whether a run uses the local fixture server or the real network.
type Locality string

const (
	LocalityLocal   Locality = "local"
	LocalityNetwork Locality = "network"
)

// Warmth indicates whether a scenario is run cold (fresh engine/process) or warm (reused state).
type Warmth string

const (
	WarmthCold Warmth = "cold"
	WarmthWarm Warmth = "warm"
)

// Workload identifies a single benchmark execution unit: a scenario, engine mode,
// locality, warmth, and repetition count.
type Workload struct {
	ScenarioID string     `json:"scenarioId"`
	EngineMode EngineMode `json:"engineMode"`
	Locality   Locality   `json:"locality"`
	Warmth     Warmth     `json:"warmth"`
	Iterations int        `json:"iterations"`
}

// NewWorkload returns a workload for the given scenario with sensible defaults.
func NewWorkload(s Scenario, mode EngineMode, locality Locality, warmth Warmth, iterations int) Workload {
	if s.EngineMode != "" {
		mode = s.EngineMode
	}
	if mode == "" {
		mode = ModeRenderless
	}
	if locality == "" {
		locality = LocalityLocal
	}
	if warmth == "" {
		warmth = WarmthCold
	}
	if iterations <= 0 {
		iterations = 5
	}
	return Workload{
		ScenarioID: s.ID,
		EngineMode: mode,
		Locality:   locality,
		Warmth:     warmth,
		Iterations: iterations,
	}
}

// Key returns a stable string key for grouping results.
func (w Workload) Key() string {
	parts := []string{w.ScenarioID, string(w.EngineMode), string(w.Locality), string(w.Warmth)}
	return strings.Join(parts, "-")
}

// String returns a human-readable workload description.
func (w Workload) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", w.ScenarioID, w.EngineMode, w.Locality, w.Warmth)
}

// IsValid returns true if the workload has all required fields.
func (w Workload) IsValid() bool {
	return w.ScenarioID != "" && w.EngineMode != "" && w.Locality != "" && w.Warmth != "" && w.Iterations > 0
}

// DefaultWorkloads returns the standard workload matrix for the given scenarios:
// local cold and warm runs for renderless, chromium, and hybrid modes.
func DefaultWorkloads(scenarios []Scenario) []Workload {
	workloads := []Workload{}
	for _, s := range scenarios {
		for _, warmth := range []Warmth{WarmthCold, WarmthWarm} {
			mode := s.EngineMode
			if mode == "" {
				mode = ModeRenderless
			}
			workloads = append(workloads, Workload{
				ScenarioID: s.ID,
				EngineMode: mode,
				Locality:   LocalityLocal,
				Warmth:     warmth,
				Iterations: 5,
			})
		}
	}
	return workloads
}
