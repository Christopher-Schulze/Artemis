package corpus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

func TestRunRenderlessExecutesExactCorpusOutcomesAndRejectsForeignInputs(t *testing.T) {
	manifests, data, err := DefaultAssets("renderless")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id     string
		status int
		title  string
	}{{"html-001", 200, "HTML Fixture"}, {"js-001", 200, "JS Fixture"}, {"auth-basic-001", 401, ""}, {"crash-001", 500, ""}} {
		t.Run(test.id, func(t *testing.T) {
			for _, manifest := range manifests {
				if !strings.HasSuffix(manifest.ID, ":"+test.id) {
					continue
				}
				result, err := RunRenderless(t.Context(), manifest, data)
				if err != nil || !result.Passed || result.StatusCode != test.status || result.Title != test.title || result.FixtureManifest.ID != manifest.ID || result.DurationMs < 0 {
					t.Fatalf("actual pinned outcome: %+v %v", result, err)
				}
				return
			}
			t.Fatal("required real corpus case missing")
		})
	}
	manifest := manifests[0]
	if result, err := RunRenderless(nil, manifest, data); err == nil || result.Passed {
		t.Fatal("nil owner acquired passing evidence")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := RunRenderless(cancelled, manifest, data); !errors.Is(err, context.Canceled) || result.Passed || result.StatusCode != 0 {
		t.Fatalf("cancelled owner acquired an observation: %+v %v", result, err)
	}
	changed := append([]byte(nil), data...)
	changed[0] ^= 1
	if result, err := RunRenderless(t.Context(), manifest, changed); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("corrupted source acquired an observation")
	}
	foreign, foreignData, err := DefaultAssets("chromium")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunRenderless(t.Context(), foreign[0], foreignData); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("native manifest silently fell back to renderless")
	}
}

func TestRunRenderlessProvesActualFrameOutcome(t *testing.T) {
	manifests, data, err := DefaultAssets("renderless")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if !strings.HasSuffix(manifest.ID, ":frame-001") {
			continue
		}
		result, err := RunRenderless(t.Context(), manifest, data)
		t.Logf("actual frame outcome: passed=%t eval_error=%q eval=%q run_error=%v", result.Passed, result.EvalError, result.EvalResult, err)
		if result.StatusCode != 200 || result.Title != "Frame Fixture" || result.FixtureManifest.ID != manifest.ID {
			t.Fatalf("actual frame source/observation lost: %+v %v", result, err)
		}
		if err != nil || !result.Passed || result.EvalError != "" || !strings.Contains(result.EvalResult, "Inner heading") {
			t.Fatalf("actual complete frame outcome rejected: %+v %v", result, err)
		}
		scenario, err := fixture.CorpusScenario(manifest, data)
		if err != nil {
			t.Fatal(err)
		}
		// Exercise the internal semantic validator against the unchanged actual
		// HTTP source with an intentionally false requested title. The public
		// admission boundary separately rejects such a changed descriptor.
		scenario.Expect.Title = "an impossible title for the actual source"
		failed, err := runAdmittedRenderless(t.Context(), manifest, data, scenario)
		if err == nil || failed.Title != "Frame Fixture" || failed.StatusCode != 200 {
			t.Fatalf("actual semantic failure or observation was lost: %+v %v", failed, err)
		}
		return
	}
	t.Fatal("required actual frame fixture missing")
}

func TestRunRenderlessNetworkOwnerRejectsAnotherActualOrigin(t *testing.T) {
	var ownCalls, foreignCalls atomic.Int64
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ownCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer owner.Close()
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer foreign.Close()
	runner, err := newCorpusEngine(owner.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	page, err := runner.Fetch(t.Context(), owner.URL, engine.FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if page, err := runner.Fetch(t.Context(), foreign.URL, engine.FetchOpts{}); err == nil {
		if page != nil {
			if err := page.Close(); err != nil {
				t.Error(err)
			}
		}
		t.Fatal("another actual origin bypassed the scoped corpus policy")
	}
	if ownCalls.Load() != 1 || foreignCalls.Load() != 0 {
		t.Fatalf("actual wire ownership: own=%d foreign=%d", ownCalls.Load(), foreignCalls.Load())
	}
}
