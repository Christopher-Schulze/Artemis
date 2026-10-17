package replay

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReplayResultEvidencePreservesActualOutcomesAndExplicitProfiles(t *testing.T) {
	for _, profile := range []struct {
		name   string
		policy Policy
	}{{"default", Policy{}}, {"custom", Policy{Patterns: []string{"Captured"}, Mask: "[MASK]"}}, {"raw", Policy{AllowUnredacted: true}}} {
		t.Run(profile.name, func(t *testing.T) {
			manifest, content := capturedAssetForTest(t, profile.policy)
			result, err := Run(t.Context(), content, manifest, profile.policy)
			if err != nil || !result.Passed {
				t.Fatalf("actual replay failed: %v", err)
			}
			prepared, err := PrepareResultEvidence(t.Context(), content, manifest, profile.policy, result, "")
			if err != nil || prepared.Result != result || prepared.Diagnostic != "" {
				t.Fatalf("actual replay observation changed: %+v %v", prepared, err)
			}
			if profile.name != "default" {
				if _, err := PrepareResultEvidence(t.Context(), content, manifest, Policy{}, result, ""); err == nil {
					t.Fatal("stored profile replaced the caller's explicit policy")
				}
			}
		})
	}
}

func TestReplayResultEvidenceRejectsChangedAssertionsIdentityAndBounds(t *testing.T) {
	manifest, content := capturedAssetForTest(t, Policy{})
	result, err := Run(t.Context(), content, manifest, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*Result)
	}{
		{"capture", func(r *Result) { r.CaptureID = "foreign" }},
		{"hash", func(r *Result) { r.AssetHash = strings.Repeat("0", 64) }},
		{"status", func(r *Result) { r.StatusCode = 0 }},
		{"title", func(r *Result) { r.Title = "foreign" }},
		{"text", func(r *Result) { r.Text = "" }},
		{"URL", func(r *Result) { r.URL = "http://127.0.0.1/foreign" }},
		{"duration", func(r *Result) { r.DurationMs = -1 }},
		{"UTF-8", func(r *Result) { r.Text = "\xff" }},
		{"oversized", func(r *Result) { r.Text = strings.Repeat("x", (8<<20)+(64<<10)+1) }},
		{"invented page", func(r *Result) { r.CaptureID = "" }},
		{"failed without diagnostic", func(r *Result) { r.Passed = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := result
			test.edit(&changed)
			if _, err := PrepareResultEvidence(t.Context(), content, manifest, Policy{}, changed, ""); err == nil {
				t.Fatal("changed result acquired passing evidence")
			}
		})
	}
	if _, err := PrepareResultEvidence(nil, content, manifest, Policy{}, result, ""); err == nil {
		t.Fatal("missing owner admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := PrepareResultEvidence(ctx, content, manifest, Policy{}, result, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if prepared, err := PrepareResultEvidence(t.Context(), content, manifest, Policy{}, Result{DurationMs: 1}, "actual early failure"); err != nil || prepared.Result.CaptureID != "" || prepared.Result.Passed {
		t.Fatalf("early failure gained an invented page: %+v %v", prepared, err)
	}
}

func TestReplayResultEvidenceUsesVerifiedCustomProfileWithoutRescuingFalseAssertions(t *testing.T) {
	profile := Policy{Patterns: []string{"Captured"}, Mask: "[MASK]"}
	manifest, content := capturedAssetForTest(t, profile)
	result, err := Run(t.Context(), content, manifest, profile)
	if err != nil || result.Title != profile.Mask {
		t.Fatalf("actual custom-profile control failed: %v", err)
	}
	changed := result
	changed.Title = "Captured"
	if _, err := PrepareResultEvidence(t.Context(), content, manifest, profile, changed, ""); err == nil {
		t.Fatal("redaction converted a false original assertion into passing evidence")
	}
	changed.Passed = false
	prepared, err := PrepareResultEvidence(t.Context(), content, manifest, profile, changed, "actual Captured failure")
	if err != nil || prepared.Result.Passed || prepared.Result.Title != profile.Mask || strings.Contains(prepared.Diagnostic, "Captured") || changed.Title != "Captured" {
		t.Fatalf("new observations/diagnostic escaped the verified profile or mutated the original: %+v %v", prepared, err)
	}
	for _, diagnostic := range []string{strings.Repeat("x", 65537), "\xff", "invalid\x00diagnostic"} {
		if _, err := PrepareResultEvidence(t.Context(), content, manifest, profile, changed, diagnostic); err == nil {
			t.Fatal("unbounded or invalid diagnostic admitted")
		}
	}
}
