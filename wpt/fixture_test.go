package wpt

import (
	"context"
	"errors"
	"testing"
	"time"

	internalwpt "github.com/Christopher-Schulze/Artemis/internal/wpt"
)

func TestWPTPublicFixtureAdmissionAndMissingCompletionTruth(t *testing.T) {
	manifest, content, err := DefaultFixture()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFixture(t.Context(), manifest, content); err != nil {
		t.Fatal(err)
	}
	changed := manifest.Detached()
	changed.RunnerRef = "other"
	if results, err := RunFixture(t.Context(), changed, content); err == nil || len(results) != 0 {
		t.Fatal("wrong runner acquired public conformance evidence")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if results, err := RunFixture(cancelled, manifest, content); !errors.Is(err, context.Canceled) || len(results) != 0 {
		t.Fatal("cancelled owner acquired public conformance evidence")
	}
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	results, err := RunFixture(ctx, manifest, content)
	if err != nil {
		t.Fatalf("actual public WPT execution failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("completed public conformance omitted all evidence")
	}
	for _, result := range results {
		if result.Status != "PASS" || result.HarnessStatus != "OK" || result.FixtureManifest.AssetHash != manifest.AssetHash {
			t.Fatal("completed public conformance lost actual passing evidence")
		}
	}
	if err := ValidateResults(cancelled, manifest, content, results); !errors.Is(err, context.Canceled) {
		t.Fatalf("result validation discarded its owning cancellation: %v", err)
	}
	for _, change := range []string{"omitted", "reordered", "duplicate", "foreign manifest", "unknown status", "failed harness", "invalid diagnostic"} {
		t.Run(change, func(t *testing.T) {
			changed := append([]Result{}, results...)
			switch change {
			case "omitted":
				changed = changed[:len(changed)-1]
			case "reordered":
				changed[0], changed[1] = changed[1], changed[0]
			case "duplicate":
				changed[1] = changed[0]
			case "foreign manifest":
				changed[0].FixtureManifest = changed[0].FixtureManifest.Detached()
				changed[0].FixtureManifest.QualityGateRef = "foreign-gate"
			case "unknown status":
				changed[0].Status = "SUCCESS"
			case "failed harness":
				changed[0].HarnessStatus = "ERROR"
			case "invalid diagnostic":
				changed[0].Message = string([]byte{255})
			}
			if err := ValidateResults(t.Context(), manifest, content, changed); err == nil {
				t.Fatal("changed reports acquired complete pinned result identity")
			}
		})
	}
}

func TestCompletedFailingWPTReportsRemainBoundFailureEvidence(t *testing.T) {
	manifest, _, err := DefaultFixture()
	if err != nil {
		t.Fatal(err)
	}
	reported := internalwpt.Result{Path: "actual.html", Name: "actual failed assertion", Status: "FAIL", HarnessStatus: "OK",
		Message: "actual assertion detail", FixtureManifest: &manifest}
	results, err := projectResults([]internalwpt.Result{reported})
	if err == nil || len(results) != 1 || results[0].Status != "FAIL" || results[0].Message != reported.Message || results[0].FixtureManifest.AssetHash != manifest.AssetHash {
		t.Fatalf("completed failure became success or lost its bound evidence: %+v %v", results, err)
	}
}
