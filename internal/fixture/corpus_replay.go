package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func bindCorpusCapture(scenario Scenario, runner string, manifest fixtureasset.Manifest) (*fixtureasset.Manifest, error) {
	data, err := DefaultCorpusBytes()
	if err != nil {
		return nil, err
	}
	if err := ValidateCorpusAsset(manifest, data); err != nil {
		return nil, err
	}
	expected, err := corpusManifest(data, runner, scenario)
	if err != nil || expected.ID != manifest.ID {
		return nil, fmt.Errorf("replay: source manifest does not identify the captured case and runner")
	}
	requested, err := json.Marshal(corpusCase{Scenario: scenario, HasHandler: scenario.Handler != nil})
	if err != nil {
		return nil, err
	}
	for _, builtin := range DefaultScenarios() {
		if builtin.ID != scenario.ID {
			continue
		}
		actual, err := json.Marshal(corpusCase{Scenario: builtin, HasHandler: builtin.Handler != nil})
		if err != nil || !bytes.Equal(actual, requested) {
			return nil, fmt.Errorf("replay: source manifest and captured descriptor differ")
		}
		owned := manifest.Detached()
		return &owned, nil
	}
	return nil, fmt.Errorf("replay: captured source is not in the pinned corpus")
}

func (r *Replay) validateCorpusReference() error {
	m := r.Result.FixtureManifest
	if m == nil {
		return nil
	}
	if err := m.ValidateReference(); err != nil {
		return err
	}
	if !knownFixtureRunner(r.Runner) || len(m.CapabilityRefs) != 1 || m.CapabilityRefs[0] != "artemis:fixture:"+string(r.Scenario.Kind) {
		return fmt.Errorf("replay: inconsistent corpus runner or kind requirement")
	}
	if m.OwnerSubsystem != "artemis-fixture" || m.FixtureKind != fixtureasset.KindBenchmark || m.ReplayMode != fixtureasset.ReplayIsolated ||
		m.RunnerRef != "artemis:fixture:"+r.Runner || m.AssetPath != "content://sha256/"+m.AssetHash ||
		!strings.HasPrefix(m.ID, "corpus:") || !strings.HasSuffix(m.ID, ":"+m.AssetHash+":"+r.Runner+":"+r.ScenarioID) ||
		m.QualityGateRef != "go-test:internal/fixture:TestFixtureCorpusThroughAllPaths/"+r.Runner+"/"+r.ScenarioID {
		return fmt.Errorf("replay: inconsistent inert corpus reference")
	}
	return nil
}

func corpusReplayFields(manifest *fixtureasset.Manifest) []replayField {
	if manifest == nil {
		return nil
	}
	fields := []replayField{
		{&manifest.ID, replayIdentityField}, {&manifest.OwnerSubsystem, replayIdentityField},
		{&manifest.AssetPath, replayIdentityField}, {&manifest.AssetHash, replayIdentityField},
		{&manifest.RunnerRef, replayIdentityField}, {&manifest.QualityGateRef, replayIdentityField}, {&manifest.Retention, replayIdentityField},
	}
	for _, refs := range [][]string{manifest.DomainRefs, manifest.CapabilityRefs, manifest.ConnectorRefs, manifest.ExpectedArtifactRefs} {
		for index := range refs {
			fields = append(fields, replayField{&refs[index], replayIdentityField})
		}
	}
	return fields
}
