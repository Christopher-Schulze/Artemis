package benchmark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

const FixtureRunnerRef = "artemis:benchmark:renderless"

// EncodedFixture contains the complete benchmark input, including HTML omitted
// from scorecards. The embedding owner must grant current access separately.
type EncodedFixture struct {
	Manifest fixtureasset.Manifest
	Content  []byte
}

type benchmarkFixture struct {
	Version  string   `json:"version"`
	Scenario Scenario `json:"scenario"`
	HTML     string   `json:"html"`
}

// EncodeFixture binds declared governance metadata to actual HTML and every
// expectation. An omitted writer ID derives an immutable payload-version ID;
// explicit legacy case IDs remain unchanged. Existing bindings must match.
func EncodeFixture(scenario Scenario, manifest fixtureasset.Manifest) (EncodedFixture, error) {
	if err := validateFixtureScenario(scenario); err != nil {
		return EncodedFixture{}, err
	}
	content, err := json.Marshal(benchmarkFixture{Version: ScenarioMatrixVersion, Scenario: scenario, HTML: scenario.HTML})
	if err != nil {
		return EncodedFixture{}, err
	}
	hash := sha256.Sum256(content)
	digest := hex.EncodeToString(hash[:])
	path := "content://sha256/" + digest
	versionID := "benchmark:" + ScenarioMatrixVersion + ":" + digest + ":" + scenario.ID
	if manifest.ID == "" {
		manifest.ID = versionID
	}
	if (manifest.ID != scenario.ID && manifest.ID != versionID) || manifest.FixtureKind != fixtureasset.KindBenchmark || manifest.RunnerRef != FixtureRunnerRef ||
		manifest.AssetHash != "" && manifest.AssetHash != digest || manifest.AssetPath != "" && manifest.AssetPath != path {
		return EncodedFixture{}, fmt.Errorf("benchmark fixture: manifest and actual input differ")
	}
	manifest.AssetHash, manifest.AssetPath = digest, path
	if err := manifest.Validate(content); err != nil {
		return EncodedFixture{}, err
	}
	return EncodedFixture{Manifest: manifest.Detached(), Content: content}, nil
}

// DecodeFixture admits canonical, bounded data and retains all original
// expectations; transport validity grants no permission or passing coverage.
func DecodeFixture(encoded EncodedFixture) (Scenario, error) {
	if err := encoded.Manifest.Validate(encoded.Content); err != nil {
		return Scenario{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded.Content))
	decoder.DisallowUnknownFields()
	var fixture benchmarkFixture
	if err := decoder.Decode(&fixture); err != nil {
		return Scenario{}, err
	}
	if _, err := decoder.Token(); err != io.EOF || fixture.Version != ScenarioMatrixVersion {
		return Scenario{}, fmt.Errorf("benchmark fixture: version or trailing input denied")
	}
	fixture.Scenario.HTML = fixture.HTML
	canonical, err := EncodeFixture(fixture.Scenario, encoded.Manifest)
	if err != nil {
		return Scenario{}, err
	}
	if !bytes.Equal(canonical.Content, encoded.Content) {
		return Scenario{}, fmt.Errorf("benchmark fixture: noncanonical or incomplete input")
	}
	return fixture.Scenario, nil
}

func validateFixtureScenario(scenario Scenario) error {
	if scenario.ID == "" || len(scenario.ID) > 128 || scenario.ID == "." || scenario.ID == ".." ||
		strings.Trim(scenario.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.") != "" ||
		scenario.EngineMode != ModeRenderless || scenario.HTML == "" || len(scenario.HTML) > 8<<20 ||
		scenario.ScriptCount < 0 || scenario.ScriptCount > 1024 || scenario.BodyBytes != 0 && scenario.BodyBytes != len(scenario.HTML) ||
		scenario.ExpectTitle == "" || scenario.ExpectLinks < 0 || scenario.ExpectLinks > 1<<20 || scenario.ExpectParagraphs < 0 || scenario.ExpectParagraphs > 1<<20 {
		return fmt.Errorf("benchmark fixture: bounded substantive local renderless scenario required")
	}
	switch scenario.Kind {
	case KindNavigation, KindScrape, KindDOM, KindMarkdown, KindScriptHeavy:
	default:
		return fmt.Errorf("benchmark fixture: unknown scenario kind")
	}
	for _, value := range []string{scenario.Description, scenario.ExpectTitle, scenario.HTML} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return fmt.Errorf("benchmark fixture: NUL-free UTF-8 required")
		}
	}
	if len(scenario.Description) > 4096 || len(scenario.ExpectTitle) > 4096 {
		return fmt.Errorf("benchmark fixture: metadata byte limit exceeded")
	}
	return nil
}

// NewArtemisRunnerFromFixtures executes only decoded, byte-bound inputs.
// Current permission/scanner/retention admission belongs to the embedding owner.
func NewArtemisRunnerFromFixtures(fixtures []EncodedFixture, warmPool bool) (*ArtemisRunner, error) {
	if len(fixtures) == 0 || len(fixtures) > 1024 {
		return nil, fmt.Errorf("benchmark fixture: nonempty bounded input batch required")
	}
	var total int
	for _, encoded := range fixtures {
		if len(encoded.Content) > 64<<20-total {
			return nil, fmt.Errorf("benchmark fixture: aggregate input byte limit exceeded")
		}
		total += len(encoded.Content)
	}
	scenarios := make([]Scenario, len(fixtures))
	for index, encoded := range fixtures {
		scenario, err := DecodeFixture(encoded)
		if err != nil {
			return nil, err
		}
		scenarios[index] = scenario
	}
	runner := NewArtemisRunner(scenarios, warmPool)
	if runner.initializationErr != nil {
		return nil, runner.initializationErr
	}
	return runner, nil
}
