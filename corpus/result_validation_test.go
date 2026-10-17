package corpus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func TestValidateResultKeepsEveryPinnedRenderlessAssertion(t *testing.T) {
	manifests, content, err := DefaultAssets("renderless")
	if err != nil || len(manifests) != 37 {
		t.Fatalf("complete original corpus plus three CSS cases required: %d %v", len(manifests), err)
	}
	for _, manifest := range manifests {
		t.Run(manifest.ID, func(t *testing.T) {
			result, err := RunRenderless(t.Context(), manifest, content)
			if err != nil || !result.Passed {
				t.Fatalf("actual original run failed: %v", err)
			}
			if err := ValidateResult(t.Context(), manifest, content, result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateResultRejectsChangedIdentityObservationsAndBounds(t *testing.T) {
	manifests, content, err := DefaultAssets("renderless")
	if err != nil {
		t.Fatal(err)
	}
	var manifest fixtureasset.Manifest
	for _, candidate := range manifests {
		if strings.HasSuffix(candidate.ID, ":js-001") {
			manifest = candidate
		}
	}
	result, err := RunRenderless(t.Context(), manifest, content)
	if err != nil || !result.Passed {
		t.Fatalf("actual original evaluation required: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Result)
	}{
		{"foreign identity", func(r *Result) { r.FixtureManifest.ID += ":foreign" }},
		{"foreign runner", func(r *Result) { r.FixtureManifest.RunnerRef = "artemis:fixture:agent" }},
		{"omitted capability", func(r *Result) { r.FixtureManifest.CapabilityRefs = nil }},
		{"missing manifest", func(r *Result) { r.FixtureManifest = fixtureasset.Manifest{} }},
		{"missing status", func(r *Result) { r.StatusCode = 0 }},
		{"wrong title", func(r *Result) { r.Title = "foreign title" }},
		{"missing text", func(r *Result) { r.Text = "" }},
		{"missing evaluation", func(r *Result) { r.EvalResult = "" }},
		{"evaluation failed", func(r *Result) { r.EvalError = "actual evaluation failed" }},
		{"invalid duration", func(r *Result) { r.DurationMs = -1 }},
		{"invalid UTF-8", func(r *Result) { r.Text = "\xff" }},
		{"NUL observation", func(r *Result) { r.Title += "\x00" }},
		{"aggregate overflow", func(r *Result) { r.HTML = strings.Repeat("x", MaxResultBytes) }},
		{"link overflow", func(r *Result) { r.Links = make([]string, 4097) }},
		{"failed foreign identity", func(r *Result) { r.Passed = false; r.FixtureManifest.Retention = "foreign" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := result
			changed.FixtureManifest = result.FixtureManifest.Detached()
			test.edit(&changed)
			if err := ValidateResult(t.Context(), manifest, content, changed); err == nil {
				t.Fatal("changed result acquired a valid original proof contract")
			}
		})
	}
	if err := ValidateResult(nil, manifest, content, result); err == nil {
		t.Fatal("missing owner admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ValidateResult(ctx, manifest, content, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	changed := append([]byte(nil), content...)
	changed[0] ^= 1
	if err := ValidateResult(t.Context(), manifest, changed, result); err == nil {
		t.Fatal("changed source retained passing result")
	}
	if err := ValidateResult(t.Context(), manifest, content, Result{DurationMs: 1}); err != nil {
		t.Fatalf("actual early failure requires no invented observation: %v", err)
	}
	if err := ValidateResult(t.Context(), manifest, content, Result{Passed: true}); err == nil {
		t.Fatal("absent observation acquired passing coverage")
	}
}
