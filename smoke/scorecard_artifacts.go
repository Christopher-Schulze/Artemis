package smoke

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
)

type smokeArtifactValidation struct {
	seen  map[string]bool
	bytes int64
}

func validateScenarioArtifacts(ctx context.Context, result ScenarioResult, source Scenario, read ArtifactReader, state *smokeArtifactValidation) error {
	values, err := scenarioValueArtifacts(result, source)
	if err != nil {
		return err
	}
	log := false
	for _, artifact := range result.Artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		step, isValue := values[artifact.Path]
		if !isValue && artifact.Path != source.ID+"/serve.log" {
			return fmt.Errorf("undeclared scenario artifact")
		}
		if !isValue {
			log = true
		}
		content, err := readScorecardArtifact(ctx, artifact, read, state)
		if err != nil {
			return err
		}
		if isValue {
			if *step.ValueArtifact != artifact {
				return fmt.Errorf("step and cohort artifact references differ")
			}
			if err := validateStepArtifact(step, content); err != nil {
				return err
			}
			delete(values, artifact.Path)
		}
	}
	if len(values) != 0 || result.Outcome != OutcomeHarnessFailure && !log {
		return fmt.Errorf("complete step or joined-process evidence missing")
	}
	return nil
}

func scenarioValueArtifacts(result ScenarioResult, source Scenario) (map[string]StepResult, error) {
	values := make(map[string]StepResult)
	for _, step := range result.Steps {
		if err := validateInlineStep(step); err != nil {
			return nil, err
		}
		if step.ValueArtifact != nil {
			expected := fmt.Sprintf("%s/step-%02d-%s.json", source.ID, step.Index, sanitize(step.Name))
			if step.ValueArtifact.Path != expected {
				return nil, fmt.Errorf("step artifact differs from its actual input identity")
			}
			values[expected] = step
		} else if step.OK && step.Cmd == "page.dump" {
			return nil, fmt.Errorf("successful dump lacks its complete artifact")
		}
	}
	return values, nil
}

func readScorecardArtifact(ctx context.Context, artifact EvidenceArtifact, read ArtifactReader, state *smokeArtifactValidation) ([]byte, error) {
	if read == nil || artifact.Path == "" || path.IsAbs(artifact.Path) || path.Clean(artifact.Path) != artifact.Path ||
		!validSmokeHash(artifact.SHA256) || artifact.Bytes < 0 || artifact.Bytes > smokeArtifactBytesLimit ||
		artifact.Bytes > smokeOutputBytesLimit-state.bytes || state.seen[artifact.Path] {
		return nil, fmt.Errorf("bounded unique artifact reference required")
	}
	content, err := read(ctx, artifact)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	if int64(len(content)) != artifact.Bytes || hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return nil, fmt.Errorf("artifact bytes, size or hash differs")
	}
	state.seen[artifact.Path] = true
	state.bytes += artifact.Bytes
	return content, ctx.Err()
}

func validateStepArtifact(step StepResult, content []byte) error {
	if step.Cmd != "page.dump" && len(content) <= smokeResultPreviewBytesLimit || (step.Got == nil) != (len(content) > smokeResultPreviewBytesLimit) {
		return fmt.Errorf("retained result and inline preview disposition differ")
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return err
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, content) {
		return fmt.Errorf("step artifact is not the exact canonical command result")
	}
	if step.Got != nil {
		inline, err := json.Marshal(step.Got)
		if err != nil || !bytes.Equal(inline, content) {
			return fmt.Errorf("inline and retained command observations differ")
		}
	}
	return validateStepValue(step, value)
}
