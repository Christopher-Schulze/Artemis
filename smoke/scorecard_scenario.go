package smoke

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/serve"
)

func validateScenarioResult(ctx context.Context, result ScenarioResult, source Scenario, read ArtifactReader, state *smokeArtifactValidation) error {
	if result.ID != source.ID || result.Site != source.Site || result.Description != source.Description || result.Duration < 0 ||
		result.Outcome == OutcomeInterrupted || result.Pass && (result.Skipped || result.NetworkFail) {
		return fmt.Errorf("source identity, duration or completion outcome differs")
	}
	expected := result
	expected.finalize(0, nil)
	if result.Outcome != expected.Outcome || result.Pass != expected.Pass || result.Skipped != expected.Skipped {
		return fmt.Errorf("scenario flags and typed outcome differ")
	}
	if result.Outcome == OutcomeMissingEnvironment {
		return validateMissingEnvironment(result, source)
	}
	if result.ServeOwner != nil {
		if err := result.ServeOwner.Validate(); err != nil {
			return err
		}
	} else if result.Outcome != OutcomeHarnessFailure {
		return fmt.Errorf("actual bound Serve owner required")
	}
	if err := validateScenarioSteps(result, source); err != nil {
		return err
	}
	return validateScenarioArtifacts(ctx, result, source, read, state)
}

func validateMissingEnvironment(result ScenarioResult, source Scenario) error {
	if !result.Skipped || result.Pass || result.NetworkFail || result.HarnessError != "" || result.ServeOwner != nil || len(result.Steps) != 0 || len(result.Artifacts) != 0 {
		return fmt.Errorf("missing-environment skip contains invented execution")
	}
	for _, key := range source.Requires {
		if result.SkipReason == "missing env var "+key {
			return nil
		}
	}
	return fmt.Errorf("missing-environment skip is not bound to an input requirement")
}

func validateScenarioSteps(result ScenarioResult, source Scenario) error {
	position, harness := 0, 0
	failed, network := false, false
	for _, step := range result.Steps {
		if step.Cmd == "harness" {
			if step.OK || step.Outcome != OutcomeHarnessFailure || step.Name == "" || step.Error == "" || step.Duration != 0 || step.Index != 0 ||
				step.Got != nil || step.ValueArtifact != nil || step.Assert != nil || step.ErrorCode != "" || step.FailureKind != "" {
				return fmt.Errorf("invalid harness failure observation")
			}
			harness++
			failed = true
			continue
		}
		if failed || position >= len(source.Steps) || step.Index != position || step.Name != source.Steps[position].Name || step.Cmd != source.Steps[position].Cmd || step.Duration < 0 {
			return fmt.Errorf("executed step order or input assertion differs")
		}
		if err := validateStepOutcome(step, source.Steps[position]); err != nil {
			return err
		}
		position++
		failed = !step.OK
		if !step.OK && step.Cmd == "page.open" && step.ErrorCode == string(serve.ErrFetchFailed) && step.FailureKind == serve.FailureNetwork {
			network = true
		}
	}
	if !result.Pass && !failed || result.Pass != (!failed && position == len(source.Steps)) || (harness > 0) != (result.HarnessError != "") || result.NetworkFail != network {
		return fmt.Errorf("scenario coverage, harness diagnostics or network cause differs")
	}
	if result.Skipped && (result.Outcome != OutcomeNetworkSkip || !network || source.NetworkFail == "strict" || result.SkipReason == "") || !result.Skipped && result.SkipReason != "" {
		return fmt.Errorf("network tolerance or skip reason differs")
	}
	return nil
}

func validateStepOutcome(step StepResult, source Step) error {
	if step.OK {
		if step.Outcome != OutcomePass || step.Error != "" || step.ErrorCode != "" || step.FailureKind != "" {
			return fmt.Errorf("successful step contains failure evidence")
		}
	} else if step.Error == "" || step.Outcome != OutcomeProductFailure && step.Outcome != OutcomeNetworkFailure && step.Outcome != OutcomeHarnessFailure {
		return fmt.Errorf("failed step lacks its actual typed failure")
	}
	if (step.Outcome == OutcomeNetworkFailure) != (step.ErrorCode == string(serve.ErrFetchFailed) && step.FailureKind == serve.FailureNetwork) {
		return fmt.Errorf("unstructured failure cannot establish a network outage")
	}
	if step.Cmd != "page.assert" && step.Assert != nil {
		return fmt.Errorf("non-assertion step contains an assertion outcome")
	}
	if step.Cmd == "page.assert" && step.OK {
		want := source.AssertPass == nil || *source.AssertPass
		if step.Assert == nil || *step.Assert != want {
			return fmt.Errorf("successful assertion differs from declared expectation")
		}
	}
	if step.Cmd == "page.assert" && !step.OK && step.Outcome == OutcomeProductFailure && step.ErrorCode == "" {
		want := source.AssertPass == nil || *source.AssertPass
		if step.Assert == nil || *step.Assert == want {
			return fmt.Errorf("assertion failure does not contradict its declared expectation")
		}
	}
	return nil
}

func validateStepValue(step StepResult, value any) error {
	if step.OK {
		if err := validateSmokeIdentity(step.Cmd, value); err != nil {
			return err
		}
	}
	if step.Cmd == "page.assert" && step.Assert != nil {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("actual assertion result object required")
		}
		pass, ok := object["pass"].(bool)
		if !ok || step.Assert == nil || *step.Assert != pass {
			return fmt.Errorf("assertion observation and outcome differ")
		}
	}
	return nil
}

func validateInlineStep(step StepResult) error {
	if step.Got != nil {
		encoded, err := json.Marshal(step.Got)
		if err != nil || len(encoded) > smokeResultPreviewBytesLimit {
			return fmt.Errorf("inline result exceeds the actual preview contract")
		}
	}
	if step.ValueArtifact == nil {
		return validateStepValue(step, step.Got)
	}
	return nil
}
