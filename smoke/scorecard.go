package smoke

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

// ArtifactReader supplies exact declared bytes from the caller's admitted
// filesystem or canonical store. The decoder independently verifies each hash.
type ArtifactReader func(context.Context, EvidenceArtifact) ([]byte, error)

// DecodeScorecard verifies the complete existing scorecard, source matrix and
// artifact cohort. It executes no scenario and authenticates no producer.
func DecodeScorecard(ctx context.Context, manifest fixtureasset.Manifest, source, data []byte, read ArtifactReader) (Scorecard, error) {
	scenarios, err := DecodeFixture(ctx, manifest, source)
	if err != nil {
		return Scorecard{}, err
	}
	if len(data) == 0 || len(data) > smokeArtifactBytesLimit {
		return Scorecard{}, fmt.Errorf("smoke scorecard: bounded completed JSON required")
	}
	var card Scorecard
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&card); err != nil {
		return Scorecard{}, err
	}
	canonical, err := json.MarshalIndent(card, "", "  ")
	if err != nil || !bytes.Equal(canonical, data) {
		return Scorecard{}, fmt.Errorf("smoke scorecard: exact canonical fields and JSON required")
	}
	if err := validateScorecardHeader(card, scenarios, manifest, source); err != nil {
		return Scorecard{}, err
	}
	if err := validateScorecardResults(ctx, card, scenarios, read, int64(len(data))); err != nil {
		return Scorecard{}, err
	}
	if err := ctx.Err(); err != nil {
		return Scorecard{}, err
	}
	return card, nil
}

func validateScorecardHeader(card Scorecard, source *ScenarioFile, manifest fixtureasset.Manifest, content []byte) error {
	if card.SchemaVersion != 2 || card.Version != source.Version || card.Total != len(source.Scenarios) || len(card.Results) != card.Total ||
		card.StartedAt.IsZero() || card.FinishedAt.IsZero() || card.FinishedAt.Before(card.StartedAt) {
		return fmt.Errorf("smoke scorecard: schema, complete matrix or completion time differs")
	}
	if !filepath.IsAbs(card.OutputDir) || filepath.Clean(card.OutputDir) != card.OutputDir || filepath.Base(card.OutputDir) != "evidence" ||
		card.RunID != filepath.Base(filepath.Dir(card.OutputDir)) || !strings.HasPrefix(card.RunID, "run-") || len(card.RunID) <= 4 ||
		!boundedSmokeIdentity(card.OutputDir, 4096, false) || !boundedSmokeIdentity(card.BinaryPath, 4096, true) {
		return fmt.Errorf("smoke scorecard: run identity or publication path differs")
	}
	if card.Inputs.ScenarioSHA256 != source.sourceSHA256 || card.Inputs.MatrixSHA256 != source.normalizedSHA256 || !validSmokeHash(card.Inputs.ConfigSHA256) {
		return fmt.Errorf("smoke scorecard: source, matrix or configuration binding differs")
	}
	expected, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		return err
	}
	actual, err := fixtureasset.Encode(card.Inputs.FixtureManifest, content)
	if err != nil || !bytes.Equal(expected, actual) {
		return fmt.Errorf("smoke scorecard: admitted manifest differs")
	}
	return validateScorecardBinary(card.Binary)
}

func validateScorecardBinary(binary BinaryEvidence) error {
	if !validSmokeHash(binary.SHA256) || binary.Bytes <= 0 || binary.Bytes > smokeBinaryBytesLimit ||
		!boundedSmokeIdentity(binary.Version, 256, false) || !boundedSmokeIdentity(binary.GoVersion, 256, false) ||
		!boundedSmokeIdentity(binary.Commit, 256, true) || binary.CommitKnown != (binary.Commit != "") || !binary.ModifiedKnown && binary.Modified {
		return fmt.Errorf("smoke scorecard: actual binary identity or VCS availability differs")
	}
	return nil
}

func boundedSmokeIdentity(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && !strings.ContainsAny(value, "\x00\r\n")
}

func validSmokeHash(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateScorecardResults(ctx context.Context, card Scorecard, source *ScenarioFile, read ArtifactReader, encodedBytes int64) error {
	state := smokeArtifactValidation{seen: make(map[string]bool), bytes: encodedBytes}
	var passed, failed, skipped, product, network, harness int
	for index, result := range card.Results {
		if err := validateScenarioResult(ctx, result, source.Scenarios[index], read, &state); err != nil {
			return fmt.Errorf("smoke scorecard: scenario %s: %w", source.Scenarios[index].ID, err)
		}
		if result.Skipped {
			skipped++
		} else if result.Pass {
			passed++
		} else {
			failed++
		}
		switch result.Outcome {
		case OutcomeProductFailure:
			product++
		case OutcomeNetworkFailure:
			network++
		case OutcomeHarnessFailure:
			harness++
		}
	}
	if card.Passed != passed || card.Failed != failed || card.Skipped != skipped || card.ProductFailed != product || card.NetworkFailed != network || card.HarnessFailed != harness {
		return fmt.Errorf("smoke scorecard: counters differ from actual scenario outcomes")
	}
	return ctx.Err()
}
