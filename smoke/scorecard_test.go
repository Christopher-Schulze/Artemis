package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeScorecardVerifiesRealSourceStepsAndArtifactCohort(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	_, raw := loadSmokeLifecycle(t)
	manifest, err := DefaultFixture(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := DecodeFixture(t.Context(), manifest, raw)
	if err != nil {
		t.Fatal(err)
	}
	runner := prepareEvidenceRunner(t, binary, t.TempDir())
	card, err := runner.RunAll(t.Context(), scenarios)
	if err != nil || card.Passed != 1 || card.Failed != 0 {
		t.Fatalf("actual source and full page run required: %+v %v", card, err)
	}
	data, err := os.ReadFile(filepath.Join(card.OutputDir, "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context, artifact EvidenceArtifact) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.ReadFile(filepath.Join(card.OutputDir, filepath.FromSlash(artifact.Path)))
	}
	decoded, err := DecodeScorecard(t.Context(), manifest, raw, data, read)
	if err != nil || decoded.Passed != 1 || decoded.Binary.SHA256 != card.Binary.SHA256 {
		t.Fatalf("real complete artifact cohort rejected: %+v %v", decoded, err)
	}
	if len(decoded.Results[0].Artifacts) != 1 || decoded.Results[0].Artifacts[0].Path != "lifecycle/serve.log" {
		t.Fatal("actual joined process log was not verified")
	}
	changes := []struct {
		name string
		edit func(*Scorecard)
	}{
		{"schema", func(c *Scorecard) { c.SchemaVersion++ }},
		{"version", func(c *Scorecard) { c.Version += "-foreign" }},
		{"source hash", func(c *Scorecard) { c.Inputs.ScenarioSHA256 = strings.Repeat("0", 64) }},
		{"matrix hash", func(c *Scorecard) { c.Inputs.MatrixSHA256 = strings.Repeat("0", 64) }},
		{"manifest runner", func(c *Scorecard) { c.Inputs.FixtureManifest.RunnerRef = "foreign" }},
		{"configuration hash", func(c *Scorecard) { c.Inputs.ConfigSHA256 = "not-a-digest" }},
		{"missing result", func(c *Scorecard) { c.Results = nil }},
		{"counter", func(c *Scorecard) { c.Passed = 0 }},
		{"failure counter", func(c *Scorecard) { c.ProductFailed++ }},
		{"finish", func(c *Scorecard) { c.FinishedAt = c.StartedAt.Add(-1) }},
		{"run identity", func(c *Scorecard) { c.RunID += "-foreign" }},
		{"binary hash", func(c *Scorecard) { c.Binary.SHA256 = "foreign" }},
		{"binary bytes", func(c *Scorecard) { c.Binary.Bytes = 0 }},
		{"invented commit", func(c *Scorecard) { c.Binary.CommitKnown, c.Binary.Commit = false, "foreign" }},
		{"scenario identity", func(c *Scorecard) { c.Results[0].ID = "foreign" }},
		{"scenario site", func(c *Scorecard) { c.Results[0].Site = "https://foreign.test" }},
		{"missing steps", func(c *Scorecard) { c.Results[0].Steps = nil }},
		{"step identity", func(c *Scorecard) { c.Results[0].Steps[0].Cmd = "page.eval" }},
		{"step order", func(c *Scorecard) { c.Results[0].Steps[0].Index++ }},
		{"step outcome", func(c *Scorecard) { c.Results[0].Steps[0].Outcome = OutcomeNetworkFailure }},
		{"missing owner", func(c *Scorecard) { c.Results[0].ServeOwner = nil }},
		{"invalid owner", func(c *Scorecard) { c.Results[0].ServeOwner.ProcessID = 0 }},
		{"missing artifacts", func(c *Scorecard) { c.Results[0].Artifacts = nil }},
		{"duplicate artifact", func(c *Scorecard) { c.Results[0].Artifacts = append(c.Results[0].Artifacts, c.Results[0].Artifacts[0]) }},
		{"artifact hash", func(c *Scorecard) { c.Results[0].Artifacts[0].SHA256 = strings.Repeat("0", 64) }},
		{"artifact size", func(c *Scorecard) { c.Results[0].Artifacts[0].Bytes++ }},
		{"artifact path", func(c *Scorecard) { c.Results[0].Artifacts[0].Path = "../outside" }},
	}
	for _, test := range changes {
		t.Run(test.name, func(t *testing.T) {
			var changed Scorecard
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			test.edit(&changed)
			encoded, err := json.MarshalIndent(changed, "", "  ")
			if err != nil || bytes.Equal(encoded, data) {
				t.Fatalf("negative control must alter real evidence: %v", err)
			}
			if _, err := DecodeScorecard(t.Context(), manifest, raw, encoded, read); err == nil {
				t.Fatal("changed evidence acquired the original complete proof")
			}
		})
	}
	for _, changed := range [][]byte{append([]byte(`{"unknown":true,`), data[1:]...), append([]byte(`{"schema_version":2,`), data[1:]...)} {
		if _, err := DecodeScorecard(t.Context(), manifest, raw, changed, read); err == nil {
			t.Fatal("unknown or duplicate JSON acquired original proof")
		}
	}
	if _, err := DecodeScorecard(t.Context(), manifest, raw, data, nil); err == nil {
		t.Fatal("unread artifact cohort acquired proof")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := DecodeScorecard(ctx, manifest, raw, data, read); !errors.Is(err, context.Canceled) {
		t.Fatalf("owning cancellation lost: %v", err)
	}
}

func TestSmokeScorecardArtifactValidationUsesActualLargeProtocolValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprint(w, "<!doctype html><title>Actual smoke evidence</title><body>"+strings.Repeat("actual observed text ", 8192)+"</body>"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	address, cleanup := startServeServer(t)
	defer cleanup()
	root := t.TempDir()
	runner := NewRunner(RunnerConfig{OutDir: root, AuthToken: smokeTestToken})
	scenario := Scenario{ID: "actual", Site: server.URL, Steps: []Step{
		{Name: "create", Cmd: "session.new"},
		{Name: "open", Cmd: "page.open", Params: map[string]any{"url": server.URL, "runScripts": false}},
		{Name: "assert", Cmd: "page.assert", Params: map[string]any{"mode": "title_contains", "substring": "Actual smoke evidence"}},
		{Name: "dump", Cmd: "page.dump"},
		{Name: "close", Cmd: "session.close"},
	}}
	result := runner.runScenarioWithServeAddr(t.Context(), &scenario, address)
	if !result.Pass {
		t.Fatalf("real explicitly network-configured protocol run failed: %+v", result)
	}
	var verified int
	for _, step := range result.Steps {
		if err := validateStepOutcome(step, scenario.Steps[step.Index]); err != nil {
			t.Fatal(err)
		}
		if err := validateInlineStep(step); err != nil {
			t.Fatal(err)
		}
		if step.Cmd == "page.assert" {
			changed := step
			pass := !*step.Assert
			changed.Assert = &pass
			if err := validateStepValue(changed, step.Got); err == nil {
				t.Fatal("changed assertion outcome replaced actual observed result")
			}
		}
		if step.ValueArtifact == nil {
			continue
		}
		artifact := *step.ValueArtifact
		read := func(ctx context.Context, ref EvidenceArtifact) ([]byte, error) {
			return os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		}
		state := smokeArtifactValidation{seen: make(map[string]bool)}
		content, err := readScorecardArtifact(t.Context(), artifact, read, &state)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateStepArtifact(step, content); err != nil {
			t.Fatal(err)
		}
		changed := step
		changed.Got = map[string]any{"foreign": "observation"}
		if err := validateStepArtifact(changed, content); err == nil {
			t.Fatal("foreign inline value replaced actual retained protocol bytes")
		}
		if _, err := readScorecardArtifact(t.Context(), artifact, read, &state); err == nil {
			t.Fatal("same artifact counted twice toward complete evidence")
		}
		verified++
	}
	if verified < 1 {
		t.Fatal("large actual dump failed to exercise retained command values")
	}
}

func TestDecodeScorecardPreservesActualFailureAndUnexecutedRequirement(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	for _, test := range []struct {
		name    string
		source  string
		outcome Outcome
	}{
		{"product failure", "version: failure-1\nscenarios:\n  - id: rejected\n    site: file:///nonexistent\n    steps:\n      - name: create\n        cmd: session.new\n      - name: open\n        cmd: page.open\n        params:\n          url: file:///nonexistent\n          runScripts: false\n", OutcomeProductFailure},
		{"missing requirement", "version: skipped-1\nscenarios:\n  - id: unexecuted\n    site: https://example.test\n    requires: [ARTEMIS_SMOKE_SHARED_SCHEMA_MISSING_ENV]\n    steps:\n      - name: create\n        cmd: session.new\n", OutcomeMissingEnvironment},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ARTEMIS_SMOKE_SHARED_SCHEMA_MISSING_ENV", "")
			source := []byte(test.source)
			manifest, err := DefaultFixture(t.Context(), source)
			if err != nil {
				t.Fatal(err)
			}
			scenarios, err := DecodeFixture(t.Context(), manifest, source)
			if err != nil {
				t.Fatal(err)
			}
			runner := prepareEvidenceRunner(t, binary, t.TempDir())
			card, err := runner.RunAll(t.Context(), scenarios)
			if err != nil || card.Passed != 0 || card.Results[0].Outcome != test.outcome {
				t.Fatalf("actual typed failure/skip observation required: %+v %v", card, err)
			}
			data, err := os.ReadFile(filepath.Join(card.OutputDir, "scorecard.json"))
			if err != nil {
				t.Fatal(err)
			}
			read := func(ctx context.Context, artifact EvidenceArtifact) ([]byte, error) {
				return os.ReadFile(filepath.Join(card.OutputDir, filepath.FromSlash(artifact.Path)))
			}
			decoded, err := DecodeScorecard(t.Context(), manifest, source, data, read)
			if err != nil || decoded.Passed != 0 || decoded.Results[0].Outcome != test.outcome {
				t.Fatalf("real failed/skipped evidence lost or became passing: %+v %v", decoded, err)
			}
		})
	}
}
