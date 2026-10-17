package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

type corpusCase struct {
	Scenario   Scenario `json:"scenario"`
	HasHandler bool     `json:"has_handler"`
}

type corpusInput struct {
	SchemaVersion int          `json:"schema_version"`
	CorpusVersion string       `json:"corpus_version"`
	SourceSHA256  string       `json:"source_sha256"`
	GoVersion     string       `json:"go_version"`
	Cases         []corpusCase `json:"cases"`
}

// DefaultCorpusBytes binds every descriptor, assertion and supporting route to
// the current embedded handler/server implementation and Go runtime version.
func DefaultCorpusBytes() ([]byte, error) {
	pin, err := corpusSourceHash()
	if err != nil {
		return nil, err
	}
	input := corpusInput{SchemaVersion: 1, CorpusVersion: Version, SourceSHA256: pin, GoVersion: runtime.Version()}
	var total int64
	for _, scenario := range DefaultScenarios() {
		prepared, size, err := prepareScenario(scenario)
		if err != nil {
			return nil, err
		}
		total += size
		if total > maxRegisteredBytes || len(input.Cases) == maxRegisteredScenarios {
			return nil, fmt.Errorf("fixture corpus aggregate input limit exceeded")
		}
		input.Cases = append(input.Cases, corpusCase{Scenario: prepared, HasHandler: prepared.Handler != nil})
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(data) > fixtureasset.MaxFixtureAssetBytes {
		return nil, fmt.Errorf("fixture corpus encoded byte limit exceeded")
	}
	return data, nil
}

// DecodeCorpus rejects even correctly resealed mutations before rebinding any
// built-in handler. It cannot deserialize or execute caller-provided Go code.
func DecodeCorpus(data []byte) ([]Scenario, error) {
	if len(data) > fixtureasset.MaxFixtureAssetBytes {
		return nil, fmt.Errorf("fixture corpus byte limit exceeded")
	}
	expected, err := DefaultCorpusBytes()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, expected) {
		return nil, fmt.Errorf("fixture corpus source, runtime, routes or assertions differ from the pinned corpus")
	}
	var input corpusInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, err
	}
	builtins := DefaultScenarios()
	scenarios := make([]Scenario, len(input.Cases))
	for index, entry := range input.Cases {
		entry.Scenario.Handler = builtins[index].Handler
		scenarios[index] = cloneScenario(entry.Scenario)
	}
	return scenarios, nil
}

func corpusManifest(data []byte, runner string, scenario Scenario) (fixtureasset.Manifest, error) {
	if !knownFixtureRunner(runner) {
		return fixtureasset.Manifest{}, fmt.Errorf("fixture corpus: unknown runner")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	manifest := fixtureasset.Manifest{
		ID:             "corpus:" + Version + ":" + hash + ":" + runner + ":" + scenario.ID,
		OwnerSubsystem: "artemis-fixture", AssetPath: "content://sha256/" + hash, AssetHash: hash,
		FixtureKind: fixtureasset.KindBenchmark, PrivacyClass: fixtureasset.PrivacyPublic,
		CapabilityRefs: []string{"artemis:fixture:" + string(scenario.Kind)},
		ReplayMode:     fixtureasset.ReplayIsolated, RunnerRef: "artemis:fixture:" + runner,
		QualityGateRef: "go-test:internal/fixture:TestFixtureCorpusThroughAllPaths/" + runner + "/" + scenario.ID,
		Retention:      "fixture:run-evidence",
	}.Detached()
	return manifest, manifest.Validate(data)
}

// CorpusAssets returns individual case/runner identities sharing one complete
// content-addressed payload, so supporting routes do not become hidden inputs.
func CorpusAssets(runner string) ([]fixtureasset.Manifest, []byte, error) {
	data, err := DefaultCorpusBytes()
	if err != nil {
		return nil, nil, err
	}
	scenarios, err := DecodeCorpus(data)
	if err != nil {
		return nil, nil, err
	}
	manifests := make([]fixtureasset.Manifest, 0, len(scenarios))
	for _, scenario := range scenarios {
		manifest, err := corpusManifest(data, runner, scenario)
		if err != nil {
			return nil, nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, data, nil
}

// ValidateCorpusAsset admits an individual manifest to the complete immutable
// corpus. Declared privacy and retention are checked by the embedding authority.
func ValidateCorpusAsset(manifest fixtureasset.Manifest, data []byte) error {
	_, err := CorpusScenario(manifest, data)
	return err
}

// CorpusScenario returns only the exact admitted case and its built-in handler.
func CorpusScenario(manifest fixtureasset.Manifest, data []byte) (Scenario, error) {
	if err := manifest.Validate(data); err != nil {
		return Scenario{}, err
	}
	scenarios, err := DecodeCorpus(data)
	if err != nil {
		return Scenario{}, err
	}
	for _, runner := range []string{"renderless", "chromium", "hybrid", "serve", "agent", "artemis"} {
		if manifest.RunnerRef != "artemis:fixture:"+runner {
			continue
		}
		for _, scenario := range scenarios {
			expected, err := corpusManifest(data, runner, scenario)
			if err != nil {
				return Scenario{}, err
			}
			if manifest.ID == expected.ID && manifest.OwnerSubsystem == expected.OwnerSubsystem && manifest.AssetPath == expected.AssetPath && manifest.FixtureKind == expected.FixtureKind && manifest.ReplayMode == expected.ReplayMode && manifest.QualityGateRef == expected.QualityGateRef && slices.Equal(manifest.CapabilityRefs, expected.CapabilityRefs) {
				return scenario, nil
			}
		}
	}
	return Scenario{}, fmt.Errorf("fixture corpus: manifest case, runner or assertion contract differs")
}
