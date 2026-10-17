package benchmark

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ExecutionOptions records command controls alongside their raw evidence.
type ExecutionOptions struct {
	Iterations          int                `json:"iterations"`
	Timeout             string             `json:"timeout"`
	OutputDir           string             `json:"outputDir"`
	SkipCompetitor      bool               `json:"skipCompetitor"`
	RequireHeadToHead   bool               `json:"requireHeadToHead"`
	BenchmarkTag        string             `json:"benchmarkTag"`
	CPUProfile          string             `json:"cpuProfile"`
	MemProfile          string             `json:"memProfile"`
	RegressionBudgets   []RegressionBudget `json:"regressionBudgets"`
	OutputFormat        string             `json:"outputFormat"`
	CompetitorDirectory string             `json:"competitorDirectory"`
	DownloadOrigin      string             `json:"downloadOrigin"`
	DownloadURLSHA256   string             `json:"downloadURLSHA256"`
	ExpectedSHA256      string             `json:"expectedSHA256"`
	CompetitorPort      int                `json:"competitorPort"`
	CompetitorTimeout   string             `json:"competitorTimeout"`
	MaxDownloadBytes    int64              `json:"maxDownloadBytes"`
	VersionArgs         []string           `json:"versionArgs"`
}

func (h *Harness) executeWorkload(ctx context.Context, sc *Scorecard, workload Workload, unavailable error) (returnErr error) {
	scenario := *scenarioByID(h.scenarios, workload.ScenarioID)
	scenario.EngineMode = workload.EngineMode
	input, err := h.fixtureForWorkload(ctx, scenario)
	if err != nil {
		return err
	}
	runner, err := NewArtemisRunnerFromFixtures([]EncodedFixture{input}, workload.Warmth == WarmthWarm)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, runner.Close()) }()
	if workload.Warmth == WarmthWarm {
		// One declared unmeasured prime for each engine and workload.
		sc.Priming = append(sc.Priming, sampleIdentity(runner.RunScenario(ctx, scenario), workload, 0))
		if h.competitor != nil && unavailable == nil {
			unavailable = h.competitor.Start(ctx)
			if unavailable == nil {
				sc.Priming = append(sc.Priming, sampleIdentity(h.competitor.RunScenario(ctx, scenario, runner.BaseURL()+"/"+scenario.ID), workload, 0))
			}
		}
	}
	var artemisSamples, competitorSamples []ScenarioResult
	for index := 1; index <= workload.Iterations; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if workload.Warmth == WarmthCold && index > 1 {
			if err := runner.Reset(false); err != nil {
				return err
			}
		}
		artemis := func() {
			result := sampleIdentity(runner.RunScenario(ctx, scenario), workload, index)
			artemisSamples = append(artemisSamples, result)
			sc.Samples = append(sc.Samples, result)
		}
		competitor := func() {
			if h.competitor == nil || unavailable != nil {
				return
			}
			if workload.Warmth == WarmthCold {
				unavailable = h.competitor.Start(ctx)
				if unavailable != nil {
					return
				}
			}
			result := h.competitor.RunScenario(ctx, scenario, runner.BaseURL()+"/"+scenario.ID)
			if workload.Warmth == WarmthCold {
				if err := h.competitor.Stop(); err != nil {
					result.OK, result.Error = false, fmt.Sprintf("competitor cleanup: %v", err)
				}
			}
			result = sampleIdentity(result, workload, index)
			competitorSamples = append(competitorSamples, result)
			sc.Samples = append(sc.Samples, result)
		}
		if index%2 == 0 {
			competitor()
			artemis()
		} else {
			artemis()
			competitor()
		}
	}
	sc.AddResult(summarizeSamples(artemisSamples, workload, EngineArtemis))
	if h.competitor != nil {
		result := summarizeSamples(competitorSamples, workload, EngineCompetitor)
		if unavailable != nil {
			result.OK = false
			result.Error = fmt.Sprintf("competitor unavailable: %v", unavailable)
		}
		sc.AddResult(result)
		if err := h.competitor.Stop(); err != nil {
			return err
		}
	}
	return nil
}

func sampleIdentity(result ScenarioResult, workload Workload, index int) ScenarioResult {
	result.Workload, result.Execution, result.Sample = workload.Identity(), workload, index
	return result
}

func summarizeSamples(samples []ScenarioResult, workload Workload, engine EngineName) ScenarioResult {
	result := medianResult(samples)
	if len(samples) == 0 {
		result = ScenarioResult{ScenarioID: workload.ScenarioID, Engine: engine, EngineMode: string(workload.EngineMode), Timestamp: time.Now().UTC()}
	}
	result.Workload, result.Execution = workload.Identity(), workload
	result.Sample, result.SampleCount = 0, len(samples)
	result.OK = len(samples) == workload.Iterations
	result.Validated = result.OK
	for _, sample := range samples {
		if !sample.OK || !sample.Validated {
			result.OK, result.Validated = false, false
			result.Error = sample.Error
			result.FailedSamples++
		}
	}
	return result
}
