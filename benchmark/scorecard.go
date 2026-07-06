package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EngineName identifies which engine produced a result.
type EngineName string

const (
	EngineArtemis    EngineName = "artemis"
	EngineCompetitor EngineName = "lightpanda"
)

// ScenarioResult is the measured outcome of running one engine against
// one scenario. Times are wall-clock; memory is peak RSS delta if
// available, otherwise total bytes allocated.
type ScenarioResult struct {
	ScenarioID string     `json:"scenarioId"`
	Engine     EngineName `json:"engine"`
	WallMs     float64    `json:"wallMs"`
	AllocBytes int64      `json:"allocBytes"`
	AllocCount int64      `json:"allocCount"`
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
}

// Scorecard is the full head-to-head result set across all scenarios.
type Scorecard struct {
	Version       string           `json:"version"`
	MatrixVersion string           `json:"matrixVersion"`
	Date          time.Time        `json:"date"`
	Host          string           `json:"host"`
	Results       []ScenarioResult `json:"results"`
}

// NewScorecard creates an empty scorecard with the current timestamp.
func NewScorecard() *Scorecard {
	host, _ := os.Hostname()
	return &Scorecard{
		Version:       "1.0.0",
		MatrixVersion: ScenarioMatrixVersion,
		Date:          time.Now().UTC(),
		Host:          host,
		Results:       []ScenarioResult{},
	}
}

// AddResult appends a scenario result to the scorecard.
func (s *Scorecard) AddResult(r ScenarioResult) {
	s.Results = append(s.Results, r)
}

// Summary returns per-engine aggregate stats.
type EngineSummary struct {
	Engine    EngineName `json:"engine"`
	TotalMs   float64    `json:"totalMs"`
	AvgMs     float64    `json:"avgMs"`
	Wins      int        `json:"wins"`
	Losses    int        `json:"losses"`
	Errors    int        `json:"errors"`
	Scenarios int        `json:"scenarios"`
}

// Summarize computes per-engine aggregate stats and win/loss counts.
// A "win" is a scenario where the engine had a lower wall time than
// the other engine. Only scenarios where both engines ran successfully
// are counted for win/loss.
func (s *Scorecard) Summarize() []EngineSummary {
	byEngine := map[EngineName]*EngineSummary{}
	byScenario := map[string]map[EngineName]ScenarioResult{}

	for _, r := range s.Results {
		if _, ok := byEngine[r.Engine]; !ok {
			byEngine[r.Engine] = &EngineSummary{Engine: r.Engine}
		}
		su := byEngine[r.Engine]
		su.Scenarios++
		if !r.OK {
			su.Errors++
			continue
		}
		su.TotalMs += r.WallMs
		if _, ok := byScenario[r.ScenarioID]; !ok {
			byScenario[r.ScenarioID] = map[EngineName]ScenarioResult{}
		}
		byScenario[r.ScenarioID][r.Engine] = r
	}

	// Count wins/losses
	for _, engines := range byScenario {
		if len(engines) < 2 {
			continue
		}
		var best EngineName
		var bestMs float64 = -1
		for name, r := range engines {
			if bestMs < 0 || r.WallMs < bestMs {
				best = name
				bestMs = r.WallMs
			}
		}
		for name := range engines {
			if name == best {
				byEngine[name].Wins++
			} else {
				byEngine[name].Losses++
			}
		}
	}

	// Compute averages
	for _, su := range byEngine {
		completed := su.Scenarios - su.Errors
		if completed > 0 {
			su.AvgMs = su.TotalMs / float64(completed)
		}
	}

	out := make([]EngineSummary, 0, len(byEngine))
	for _, su := range byEngine {
		out = append(out, *su)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Engine < out[j].Engine })
	return out
}

// WriteJSON writes the scorecard as JSON to the given path.
func (s *Scorecard) WriteJSON(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("scorecard json: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("scorecard json mkdir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("scorecard json write: %w", err)
	}
	return nil
}

// WriteMarkdown writes the scorecard as a human-readable markdown report.
func (s *Scorecard) WriteMarkdown(path string) error {
	var b strings.Builder
	b.WriteString("# Artemis Benchmark Scorecard\n\n")
	b.WriteString(fmt.Sprintf("- **Date:** %s\n", s.Date.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- **Host:** %s\n", s.Host))
	b.WriteString(fmt.Sprintf("- **Matrix Version:** %s\n", s.MatrixVersion))
	b.WriteString(fmt.Sprintf("- **Scorecard Version:** %s\n\n", s.Version))

	// Summary table
	summaries := s.Summarize()
	b.WriteString("## Summary\n\n")
	b.WriteString("| Engine | Scenarios | Wins | Losses | Errors | Total ms | Avg ms |\n")
	b.WriteString("|--------|-----------|------|--------|--------|----------|--------|\n")
	for _, su := range summaries {
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %d | %.2f | %.2f |\n",
			su.Engine, su.Scenarios, su.Wins, su.Losses, su.Errors, su.TotalMs, su.AvgMs))
	}
	b.WriteString("\n")

	// Per-scenario results
	b.WriteString("## Per-Scenario Results\n\n")
	b.WriteString("| Scenario | Engine | Wall ms | Allocs | Bytes | OK | Error |\n")
	b.WriteString("|----------|--------|---------|--------|-------|----|-------|\n")

	// Sort results by scenario ID then engine
	sorted := make([]ScenarioResult, len(s.Results))
	copy(sorted, s.Results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ScenarioID != sorted[j].ScenarioID {
			return sorted[i].ScenarioID < sorted[j].ScenarioID
		}
		return sorted[i].Engine < sorted[j].Engine
	})

	for _, r := range sorted {
		errMsg := r.Error
		if errMsg == "" {
			errMsg = "-"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %.3f | %d | %d | %v | %s |\n",
			r.ScenarioID, r.Engine, r.WallMs, r.AllocCount, r.AllocBytes, r.OK, errMsg))
	}
	b.WriteString("\n")

	// Win/loss detail
	b.WriteString("## Win/Loss Detail\n\n")
	byScenario := map[string]map[EngineName]ScenarioResult{}
	for _, r := range s.Results {
		if _, ok := byScenario[r.ScenarioID]; !ok {
			byScenario[r.ScenarioID] = map[EngineName]ScenarioResult{}
		}
		byScenario[r.ScenarioID][r.Engine] = r
	}
	scenarioIDs := make([]string, 0, len(byScenario))
	for id := range byScenario {
		scenarioIDs = append(scenarioIDs, id)
	}
	sort.Strings(scenarioIDs)

	for _, id := range scenarioIDs {
		engines := byScenario[id]
		if len(engines) < 2 {
			continue
		}
		var winner EngineName
		var winnerMs float64 = -1
		for name, r := range engines {
			if r.OK && (winnerMs < 0 || r.WallMs < winnerMs) {
				winner = name
				winnerMs = r.WallMs
			}
		}
		b.WriteString(fmt.Sprintf("- **%s**: winner = %s (%.3f ms)\n", id, winner, winnerMs))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("scorecard md mkdir: %w", err)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
