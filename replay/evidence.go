package replay

import (
	"context"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

// ResultEvidence is a detached inert result sanitized under the input's exact
// verified profile. It grants neither execution nor producer authenticity.
type ResultEvidence struct {
	Result     Result
	Diagnostic string
}

// PrepareResultEvidence admits input and result without starting a runtime,
// preserves passing assertions and redacts new observations and diagnostics.
func PrepareResultEvidence(ctx context.Context, content []byte, manifest fixtureasset.Manifest, policy Policy, result Result, diagnostic string) (ResultEvidence, error) {
	if ctx == nil {
		return ResultEvidence{}, fmt.Errorf("replay evidence: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return ResultEvidence{}, err
	}
	record, err := admitAsset(content, manifest, policy)
	if err != nil {
		return ResultEvidence{}, err
	}
	if result.Passed != (diagnostic == "") || result.DurationMs < 0 || (result.StatusCode != 0 && (result.StatusCode < 100 || result.StatusCode > 599)) {
		return ResultEvidence{}, fmt.Errorf("replay evidence: consistent outcome, status, duration and diagnostic required")
	}
	fields, err := record.RedactEvidenceFields(fixture.ReplayEvidenceFields{URL: result.URL, Title: result.Title, Text: result.Text, Diagnostic: diagnostic})
	if err != nil {
		return ResultEvidence{}, err
	}
	if err := validateEvidenceResult(record, manifest, result); err != nil {
		return ResultEvidence{}, err
	}
	result.URL, result.Title, result.Text = fields.URL, fields.Title, fields.Text
	if err := validateEvidenceResult(record, manifest, result); err != nil {
		return ResultEvidence{}, err
	}
	if err := ctx.Err(); err != nil {
		return ResultEvidence{}, err
	}
	return ResultEvidence{Result: result, Diagnostic: fields.Diagnostic}, nil
}

func validateEvidenceResult(record *fixture.Replay, manifest fixtureasset.Manifest, result Result) error {
	if result.CaptureID == "" {
		absent := result
		absent.DurationMs = 0
		if absent != (Result{}) {
			return fmt.Errorf("replay evidence: absent page cannot carry invented observation or coverage")
		}
		return nil
	}
	if result.CaptureID != record.ID || result.AssetHash != manifest.AssetHash {
		return fmt.Errorf("replay evidence: exact admitted capture/hash required")
	}
	if !result.Passed {
		return nil
	}
	scenario, err := record.AsRegression()
	if err != nil {
		return err
	}
	return fixture.ValidateCrossResult(scenario, fixture.CrossResult{URL: result.URL, StatusCode: result.StatusCode, Title: result.Title, Text: result.Text})
}
