package corpus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/process"
)

func TestRunHybridRejectsChangedSourceForeignRunnerAndMissingOwner(t *testing.T) {
	manifests, data, err := DefaultAssets("hybrid")
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte{}, data...)
	changed[0] ^= 1
	if result, err := RunHybrid(t.Context(), manifests[0], changed, process.LaunchConfig{}); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("corrupt source reached execution")
	}
	foreign, source, err := DefaultAssets("chromium")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunHybrid(t.Context(), foreign[0], source, process.LaunchConfig{}); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("foreign runner fell back")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := RunHybrid(cancelled, manifests[0], data, process.LaunchConfig{}); !errors.Is(err, context.Canceled) || result.Passed || result.StatusCode != 0 {
		t.Fatal("cancelled owner acquired coverage")
	}
	if result, err := RunHybrid(nil, manifests[0], data, process.LaunchConfig{}); err == nil || result.Passed {
		t.Fatal("missing owning context acquired coverage")
	}
	if result, err := RunHybrid(t.Context(), manifests[0], data, process.LaunchConfig{ExtraArgs: []string{"--no-sandbox"}}); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("unscoped launch acquired coverage")
	}
}

func TestRunHybridActualPinnedHTMLAndNativeFrame(t *testing.T) {
	manifests, data, err := DefaultAssets("hybrid")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"html-001", "frame-001-inner", "js-001", "spa-001"} {
		t.Run(id, func(t *testing.T) {
			for _, manifest := range manifests {
				if !strings.HasSuffix(manifest.ID, ":"+id) {
					continue
				}
				result, err := RunHybrid(t.Context(), manifest, data, process.LaunchConfig{})
				if err != nil || !result.Passed || result.StatusCode != 200 || result.FixtureManifest.ID != manifest.ID || result.Text == "" {
					t.Fatalf("actual hybrid source/outcome %s: status=%d title=%q passed=%t err=%v", id, result.StatusCode, result.Title, result.Passed, err)
				}
				return
			}
			t.Fatal("required real source case missing")
		})
	}
}

func TestHybridActualEvaluationAndIdleBeforeEligibilityPromotion(t *testing.T) {
	manifests, data, err := DefaultAssets("hybrid")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, manifest := range manifests {
		scenario, err := fixture.CorpusScenario(manifest, data)
		if err != nil {
			t.Fatal(err)
		}
		if scenario.Expect.Eval == "" && !scenario.AsyncFetch && !scenario.WaitForIdle {
			continue
		}
		count++
		t.Run(scenario.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			// Probe the unchanged admitted case through the real routed page
			// before changing eligibility. Original assertions remain mandatory.
			result, err := runAdmittedHybrid(ctx, manifest, data, scenario, process.LaunchConfig{})
			if err != nil || result.EvalError != "" || !strings.Contains(result.EvalResult, scenario.Expect.EvalContains) {
				t.Fatalf("actual routed evaluation: status=%d eval=%q eval_error=%q err=%v", result.StatusCode, result.EvalResult, result.EvalError, err)
			}
		})
	}
	if count == 0 {
		t.Fatal("evaluation probe did not execute actual cases")
	}
}
