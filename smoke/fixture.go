package smoke

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

// DefaultFixture describes the exact admitted YAML without creating resources.
func DefaultFixture(ctx context.Context, source []byte) (fixtureasset.Manifest, error) {
	if ctx == nil {
		return fixtureasset.Manifest{}, fmt.Errorf("smoke fixture: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return fixtureasset.Manifest{}, err
	}
	scenarios, err := decodeScenarios(source)
	if err != nil {
		return fixtureasset.Manifest{}, err
	}
	manifest, err := scenarios.fixtureAsset()
	if err != nil {
		return fixtureasset.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return fixtureasset.Manifest{}, err
	}
	return manifest, nil
}

// DecodeFixture admits exact bytes and governance through the existing owner.
// Transport admission alone grants no execution or durable storage authority.
func DecodeFixture(ctx context.Context, manifest fixtureasset.Manifest, source []byte) (*ScenarioFile, error) {
	if ctx == nil {
		return nil, fmt.Errorf("smoke fixture: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scenarios, err := decodeScenarios(source)
	if err != nil {
		return nil, err
	}
	scenarios.fixture = manifest.Detached()
	if _, err := scenarios.fixtureAsset(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return scenarios, nil
}

func (sf *ScenarioFile) commandCapabilities() []string {
	commands := make(map[string]bool)
	for _, scenario := range sf.Scenarios {
		for _, step := range scenario.Steps {
			commands["artemis:serve:"+step.Cmd] = true
		}
	}
	refs := make([]string, 0, len(commands))
	for command := range commands {
		refs = append(refs, command)
	}
	sort.Strings(refs)
	return refs
}

func (sf *ScenarioFile) defaultFixtureManifest() fixtureasset.Manifest {
	return fixtureasset.Manifest{
		ID: "smoke:" + sf.sourceSHA256, OwnerSubsystem: "artemis-smoke",
		AssetPath: "content://sha256/" + sf.sourceSHA256, AssetHash: sf.sourceSHA256,
		FixtureKind: fixtureasset.KindCustomerAcceptance, PrivacyClass: fixtureasset.PrivacyInternal,
		CapabilityRefs: sf.commandCapabilities(), ExpectedArtifactRefs: []string{"smoke:scorecard"},
		ReplayMode: fixtureasset.ReplayIsolated, RunnerRef: "artemis:smoke:serve",
		QualityGateRef: "artemis-smoke:scenario-assertions", Retention: "smoke:run-evidence",
	}.Detached()
}

func (sf *ScenarioFile) fixtureAsset() (fixtureasset.Manifest, error) {
	if sf == nil {
		return fixtureasset.Manifest{}, fmt.Errorf("smoke fixture: loaded input required")
	}
	m := sf.fixture
	if err := m.Validate(sf.source); err != nil {
		return fixtureasset.Manifest{}, err
	}
	if m.ID != "smoke:"+sf.sourceSHA256 || m.AssetHash != sf.sourceSHA256 || m.AssetPath != "content://sha256/"+m.AssetHash || m.OwnerSubsystem != "artemis-smoke" || m.FixtureKind != fixtureasset.KindCustomerAcceptance || m.RunnerRef != "artemis:smoke:serve" || m.ReplayMode != fixtureasset.ReplayIsolated || !slices.Equal(m.CapabilityRefs, sf.commandCapabilities()) {
		return fixtureasset.Manifest{}, fmt.Errorf("smoke fixture: source, identity, runner or capability mapping differs")
	}
	if m.QualityGateRef != "artemis-smoke:scenario-assertions" || !slices.Equal(m.ExpectedArtifactRefs, []string{"smoke:scorecard"}) {
		return fixtureasset.Manifest{}, fmt.Errorf("smoke fixture: assertion gate or expected scorecard differs")
	}
	return m.Detached(), nil
}

// bindFixtureManifest admits a supplied descriptor against the actual loaded
// YAML. Transport metadata alone grants no registry or execution authority.
func (sf *ScenarioFile) bindFixtureManifest(path string) error {
	data, err := readSmokeInput(path, 128<<10)
	if err != nil {
		return err
	}
	manifest, err := fixtureasset.Decode(data, sf.source)
	if err != nil {
		return err
	}
	previous := sf.fixture
	sf.fixture = manifest
	if _, err := sf.fixtureAsset(); err != nil {
		sf.fixture = previous
		return err
	}
	return nil
}
