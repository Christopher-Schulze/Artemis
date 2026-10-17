package benchmark

import (
	"bytes"
	"context"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

// FixtureResolver returns exact currently admitted bytes from an embedding
// authority. Requested inputs are immutable; changing assertions is rejected.
type FixtureResolver func(context.Context, Scenario) (EncodedFixture, error)

func (h *Harness) prepareFixtures(ctx context.Context, workloads []Workload) error {
	selected := make(map[string]bool, len(workloads))
	for _, workload := range workloads {
		selected[workload.ScenarioID] = true
	}
	if len(selected) == 0 || len(selected) > 1024 {
		return fmt.Errorf("benchmark: bounded nonempty fixture selection required")
	}
	prepared := make(map[string]EncodedFixture, len(selected))
	var total int
	for _, scenario := range h.scenarios {
		if !selected[scenario.ID] {
			continue
		}
		if _, duplicate := prepared[scenario.ID]; duplicate {
			return fmt.Errorf("benchmark: duplicate selected fixture %q", scenario.ID)
		}
		input, err := h.resolveFixture(ctx, scenario)
		if err != nil {
			return err
		}
		if len(input.Content) > 64<<20-total {
			return fmt.Errorf("benchmark: aggregate fixture byte limit exceeded")
		}
		total += len(input.Content)
		prepared[scenario.ID] = input
	}
	if len(prepared) != len(selected) {
		return fmt.Errorf("benchmark: selected fixture missing from matrix")
	}
	h.fixtures = prepared
	return nil
}

func (h *Harness) resolveFixture(ctx context.Context, scenario Scenario) (EncodedFixture, error) {
	if err := ctx.Err(); err != nil {
		return EncodedFixture{}, err
	}
	var input EncodedFixture
	var err error
	if h.cfg.FixtureResolver != nil {
		input, err = h.cfg.FixtureResolver(ctx, scenario)
	} else {
		input, err = EncodeFixture(scenario, fixtureasset.Manifest{OwnerSubsystem: "artemis-benchmark",
			FixtureKind: fixtureasset.KindBenchmark, PrivacyClass: fixtureasset.PrivacyPublic, ReplayMode: fixtureasset.ReplayIsolated,
			RunnerRef: FixtureRunnerRef, QualityGateRef: "artemis:benchmark:semantic-equivalence", Retention: "benchmark:run-evidence"})
	}
	if err != nil {
		return EncodedFixture{}, fmt.Errorf("benchmark fixture %s admission: %w", scenario.ID, err)
	}
	actual, err := DecodeFixture(input)
	if err != nil {
		return EncodedFixture{}, err
	}
	if actual != scenario {
		return EncodedFixture{}, fmt.Errorf("benchmark fixture %s differs from the requested matrix input", scenario.ID)
	}
	if err := ctx.Err(); err != nil {
		return EncodedFixture{}, err
	}
	return EncodedFixture{Manifest: input.Manifest.Detached(), Content: bytes.Clone(input.Content)}, nil
}

func (h *Harness) fixtureForWorkload(ctx context.Context, scenario Scenario) (EncodedFixture, error) {
	prepared, ok := h.fixtures[scenario.ID]
	if !ok {
		return EncodedFixture{}, fmt.Errorf("benchmark: workload fixture was not admitted")
	}
	if h.cfg.FixtureResolver == nil {
		return prepared, nil
	}
	current, err := h.resolveFixture(ctx, scenario)
	if err != nil {
		return EncodedFixture{}, err
	}
	before, err := fixtureasset.Encode(prepared.Manifest, prepared.Content)
	if err != nil {
		return EncodedFixture{}, err
	}
	after, err := fixtureasset.Encode(current.Manifest, current.Content)
	if err != nil {
		return EncodedFixture{}, err
	}
	if !bytes.Equal(current.Content, prepared.Content) || !bytes.Equal(before, after) {
		return EncodedFixture{}, fmt.Errorf("benchmark: admitted fixture changed before workload execution")
	}
	return current, nil
}

func (h *Harness) fixtureEvidence() []ScenarioEvidence {
	var evidence []ScenarioEvidence
	for _, scenario := range h.scenarios {
		input, admitted := h.fixtures[scenario.ID]
		if !admitted {
			continue
		}
		item := scenarioEvidence([]Scenario{scenario})[0]
		manifest := input.Manifest.Detached()
		item.FixtureManifest = &manifest
		evidence = append(evidence, item)
	}
	return evidence
}
