package benchmark

import (
	"encoding/hex"
	"fmt"
	"math"
	"time"
)

// MinimumComparisonSamples limits claims to explicitly sampled workloads.
const MinimumComparisonSamples = 5

type comparisonPair struct {
	workload   Workload
	artemis    ScenarioResult
	competitor ScenarioResult
}

func (s *Scorecard) comparisonPairs() ([]comparisonPair, error) {
	if s.Mode != ModeHeadToHead || len(s.Workloads) == 0 {
		return nil, fmt.Errorf("head-to-head workload manifest required")
	}
	if s.Options.CPUProfile != "" || s.Options.MemProfile != "" {
		return nil, fmt.Errorf("profiled runs cannot substantiate comparative performance claims")
	}
	if err := s.validateRunMetadata(); err != nil {
		return nil, err
	}
	digest, err := hex.DecodeString(s.CompetitorSHA256)
	if err != nil || len(digest) != 32 || s.CompetitorVersion == "" {
		return nil, fmt.Errorf("verified competitor provenance required")
	}
	if len(s.Results) != 2*len(s.Workloads) {
		return nil, fmt.Errorf("complete paired workload summaries required")
	}
	pairs := make([]comparisonPair, 0, len(s.Workloads))
	seen := make(map[string]bool)
	for _, workload := range s.Workloads {
		if !workload.IsValid() || workload.EngineMode != ModeRenderless || workload.Locality != LocalityLocal || workload.Iterations < MinimumComparisonSamples || workload.Iterations > 1000 || seen[workload.Identity()] {
			return nil, fmt.Errorf("invalid, duplicated or insufficiently sampled workload %s", workload.Identity())
		}
		seen[workload.Identity()] = true
		pair := comparisonPair{workload: workload}
		expectation, err := s.scenarioExpectation(workload.ScenarioID)
		if err != nil {
			return nil, err
		}
		for _, engine := range []EngineName{EngineArtemis, EngineCompetitor} {
			result, err := s.validatedSummary(workload, engine)
			if err != nil {
				return nil, err
			}
			if engine == EngineArtemis {
				pair.artemis = result
			} else {
				pair.competitor = result
			}
		}
		if pair.artemis.Validation != pair.competitor.Validation {
			return nil, fmt.Errorf("semantic extraction differs for %s", workload.Identity())
		}
		if pair.artemis.Validation.Title != expectation.ExpectedTitle || pair.artemis.Validation.Links < expectation.ExpectedLinks || pair.artemis.Validation.Paragraphs < expectation.ExpectedParagraphs {
			return nil, fmt.Errorf("scenario expectation differs for %s", workload.Identity())
		}
		pairs = append(pairs, pair)
	}
	if len(s.Samples) != expectedSampleCount(s.Workloads)*2 {
		return nil, fmt.Errorf("unpaired or extraneous raw samples")
	}
	if err := s.validatePriming([]EngineName{EngineArtemis, EngineCompetitor}); err != nil {
		return nil, err
	}
	return pairs, nil
}

func (s *Scorecard) validateRunMetadata() error {
	if s.Version != "2.0.0" || !validSHA256(s.Provenance.ArtemisSHA256) || s.Provenance.SamplePolicy != SamplePolicy || s.Provenance.OSCacheState == "" {
		return fmt.Errorf("executed Artemis provenance and cache policy required")
	}
	if s.Date.IsZero() || time.Since(s.Date) > 7*24*time.Hour || s.Date.After(time.Now().Add(time.Minute)) {
		return fmt.Errorf("benchmark evidence is stale or has an invalid run date")
	}
	return nil
}

func (s *Scorecard) validateArtemisObservations() error {
	if err := s.validateRunMetadata(); err != nil {
		return err
	}
	if len(s.Workloads) == 0 || len(s.Results) != len(s.Workloads) || len(s.Samples) != expectedSampleCount(s.Workloads) {
		return fmt.Errorf("complete Artemis workload evidence required")
	}
	seen := make(map[string]bool)
	for _, workload := range s.Workloads {
		if !workload.IsValid() || workload.Iterations > 1000 || seen[workload.Identity()] {
			return fmt.Errorf("invalid Artemis workload manifest")
		}
		seen[workload.Identity()] = true
		if _, err := s.validatedSummary(workload, EngineArtemis); err != nil {
			return err
		}
	}
	return s.validatePriming([]EngineName{EngineArtemis})
}

func (s *Scorecard) validatePriming(engines []EngineName) error {
	count := 0
	for _, workload := range s.Workloads {
		if workload.Warmth != WarmthWarm {
			continue
		}
		for _, engine := range engines {
			count++
			matches := 0
			for _, prime := range s.Priming {
				if prime.Workload == workload.Identity() && prime.Engine == engine {
					matches++
					if prime.Sample != 0 || !validObservation(prime, workload) {
						return fmt.Errorf("invalid warm prime for %s/%s", workload.Identity(), engine)
					}
				}
			}
			if matches != 1 {
				return fmt.Errorf("exactly one warm prime required for %s/%s", workload.Identity(), engine)
			}
		}
	}
	if len(s.Priming) != count {
		return fmt.Errorf("extraneous priming evidence")
	}
	return nil
}

func expectedSampleCount(workloads []Workload) int {
	count := 0
	for _, workload := range workloads {
		count += workload.Iterations
	}
	return count
}

func (s *Scorecard) scenarioExpectation(id string) (ScenarioEvidence, error) {
	var result ScenarioEvidence
	count := 0
	for _, scenario := range s.Scenarios {
		if scenario.ID == id {
			result, count = scenario, count+1
		}
	}
	if count != 1 || result.ExpectedTitle == "" || !validSHA256(result.HTMLSHA256) {
		return result, fmt.Errorf("unique fixture provenance required for %s", id)
	}
	return result, nil
}

func (s *Scorecard) validatedSummary(workload Workload, engine EngineName) (ScenarioResult, error) {
	var summary ScenarioResult
	count := 0
	for _, result := range s.Results {
		if result.Workload == workload.Identity() && result.Engine == engine {
			summary, count = result, count+1
		}
	}
	if count != 1 || summary.Sample != 0 || summary.FailedSamples != 0 || summary.SampleCount != workload.Iterations || !validObservation(summary, workload) {
		return summary, fmt.Errorf("invalid summary for %s/%s", workload.Identity(), engine)
	}
	seen := make(map[int]bool, workload.Iterations)
	var samples []ScenarioResult
	for _, sample := range s.Samples {
		if sample.Workload != workload.Identity() || sample.Engine != engine {
			continue
		}
		if !validObservation(sample, workload) || sample.Sample < 1 || sample.Sample > workload.Iterations || seen[sample.Sample] || sample.Validation != summary.Validation {
			return summary, fmt.Errorf("invalid or duplicate raw sample for %s/%s", workload.Identity(), engine)
		}
		seen[sample.Sample] = true
		samples = append(samples, sample)
	}
	if len(samples) != workload.Iterations || medianResult(samples).ToMetricSet() != summary.ToMetricSet() {
		return summary, fmt.Errorf("raw sample count or median mismatch for %s/%s", workload.Identity(), engine)
	}
	return summary, nil
}

func validObservation(result ScenarioResult, workload Workload) bool {
	return result.OK && result.Validated && result.Validation.Passed && result.Error == "" &&
		result.Execution == workload && result.ScenarioID == workload.ScenarioID && result.EngineMode == string(workload.EngineMode) &&
		result.WallMs > 0 && !math.IsNaN(result.WallMs) && !math.IsInf(result.WallMs, 0) &&
		validSHA256(result.Validation.TextSHA256) && validSHA256(result.Validation.MarkdownSHA256)
}

func validSHA256(value string) bool {
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == 32
}

func (r ScenarioResult) metricAvailable(kind MetricKind) bool {
	if kind == MetricWallMs || kind == MetricThroughput || kind == MetricErrorRate {
		return true
	}
	return r.Engine == EngineArtemis
}
