package benchmark

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func comparisonScorecard(t *testing.T, count int) *Scorecard {
	t.Helper()
	sc := NewScorecard()
	sc.Mode = ModeHeadToHead
	sc.CompetitorSHA256 = strings.Repeat("a", 64)
	sc.CompetitorVersion = "comparison-domain-fixture"
	var err error
	sc.Provenance, err = currentProvenance()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < count; index++ {
		scenario := Scenario{ID: []string{"s1", "s2"}[index], HTML: "<title>Case</title><p>Actual content</p>", ExpectTitle: "Case", ExpectParagraphs: 1}
		document, err := parser.ParseHTML(strings.NewReader(scenario.HTML), "http://benchmark.test")
		if err != nil {
			t.Fatal(err)
		}
		workload := NewWorkload(scenario, ModeRenderless, LocalityLocal, WarmthCold, 5)
		sc.Workloads = append(sc.Workloads, workload)
		sc.Scenarios = append(sc.Scenarios, scenarioEvidence([]Scenario{scenario})...)
		for _, engine := range []EngineName{EngineArtemis, EngineCompetitor} {
			var samples []ScenarioResult
			for sample := 1; sample <= workload.Iterations; sample++ {
				wall := 1.0
				if engine == EngineCompetitor {
					wall = 2
				}
				result := sampleIdentity(ScenarioResult{ScenarioID: scenario.ID, Engine: engine, EngineMode: string(workload.EngineMode), WallMs: wall, Throughput: 1000 / wall, OK: true, Validated: true, Validation: validateDocument(scenario, document)}, workload, sample)
				samples = append(samples, result)
				sc.Samples = append(sc.Samples, result)
			}
			sc.AddResult(summarizeSamples(samples, workload, engine))
		}
	}
	sc.Finalize(nil)
	return sc
}

func TestComparisonRejectsInconsistentEvidence(t *testing.T) {
	cases := []struct {
		name          string
		breakEvidence func(*Scorecard)
	}{
		{"unvalidated summary", func(s *Scorecard) { s.Results[0].Validated = false }},
		{"forged success flag", func(s *Scorecard) { s.Samples[0].OK = false }},
		{"failed nonmedian sample", func(s *Scorecard) { s.Samples[4].Error = "actual sample failed" }},
		{"duplicate index", func(s *Scorecard) { s.Samples[1].Sample = s.Samples[0].Sample }},
		{"missing sample", func(s *Scorecard) { s.Samples = s.Samples[1:] }},
		{"extra sample", func(s *Scorecard) { s.Samples = append(s.Samples, s.Samples[0]) }},
		{"different warmth", func(s *Scorecard) { s.Results[1].Execution.Warmth = WarmthWarm }},
		{"different mode", func(s *Scorecard) { s.Results[1].EngineMode = string(ModeChromium) }},
		{"different sample policy", func(s *Scorecard) { s.Results[1].Execution.Iterations = 7 }},
		{"missing provenance", func(s *Scorecard) { s.CompetitorSHA256 = "" }},
		{"missing Artemis provenance", func(s *Scorecard) { s.Provenance.ArtemisSHA256 = "" }},
		{"stale evidence", func(s *Scorecard) { s.Date = s.Date.Add(-8 * 24 * time.Hour) }},
		{"missing corpus", func(s *Scorecard) { s.Scenarios = nil }},
		{"false scenario expectation", func(s *Scorecard) { s.Scenarios[0].ExpectedTitle = "must differ" }},
		{"profile instrumentation", func(s *Scorecard) { s.Options.CPUProfile = "instrumented.pprof" }},
		{"malformed semantic digest", func(s *Scorecard) { s.Results[0].Validation.TextSHA256 = strings.Repeat("z", 64) }},
		{"different title", func(s *Scorecard) { s.Results[1].Validation.Title = "different" }},
		{"different extracted text", func(s *Scorecard) { s.Results[1].Validation.TextSHA256 = strings.Repeat("b", 64) }},
		{"wrong median", func(s *Scorecard) { s.Results[0].WallMs = 9 }},
		{"forged throughput summary", func(s *Scorecard) { s.Results[0].Throughput = 1 }},
		{"forged CPU summary", func(s *Scorecard) { s.Results[0].CPUMs = 1 }},
		{"hidden failed sample count", func(s *Scorecard) { s.Results[0].FailedSamples = 1 }},
		{"raw sample as summary", func(s *Scorecard) { s.Results[0].Sample = 1 }},
		{"non-finite latency", func(s *Scorecard) { s.Results[0].WallMs = math.NaN() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := comparisonScorecard(t, 1)
			if !sc.Honest {
				t.Fatalf("valid fixture rejected: %s", sc.HonestReason)
			}
			tc.breakEvidence(sc)
			sc.Finalize(nil)
			if sc.Honest {
				t.Fatal("inconsistent evidence admitted")
			}
			for _, summary := range sc.Summarize() {
				if summary.Wins != 0 || summary.Losses != 0 {
					t.Fatal("invalid pair emitted comparative claims")
				}
			}
		})
	}
}

func TestUnavailableMetricsAndRawSampleStatistics(t *testing.T) {
	sc := comparisonScorecard(t, 1)
	sc.Finalize([]RegressionBudget{{MetricKind: MetricCPUMs, MaxDelta: 1}})
	if len(sc.BudgetChecks) != 1 || sc.BudgetChecks[0].OK || sc.BudgetChecks[0].Reason == "" {
		t.Fatal("unsupported metric treated as zero baseline")
	}
	for _, aggregate := range sc.Aggregates {
		if aggregate.Aggregate.Count != 5 {
			t.Fatalf("aggregate discarded raw samples: %+v", aggregate)
		}
		if aggregate.Engine == EngineCompetitor && aggregate.Kind == MetricCPUMs {
			t.Fatal("competitor CPU fabricated")
		}
		if aggregate.Kind == MetricErrorRate && aggregate.Mean != 0 {
			t.Fatal("successful samples counted as errors")
		}
	}
}

func TestFailedSampleCannotDisappearBehindMedian(t *testing.T) {
	sc := comparisonScorecard(t, 1)
	samples := append([]ScenarioResult(nil), sc.Samples[:5]...)
	samples[4].OK, samples[4].Validated, samples[4].Error = false, false, "failure"
	summary := summarizeSamples(samples, sc.Workloads[0], EngineArtemis)
	if summary.OK || summary.Validated || summary.FailedSamples != 1 || summary.Error != "failure" {
		t.Fatalf("failure hidden: %+v", summary)
	}
}
