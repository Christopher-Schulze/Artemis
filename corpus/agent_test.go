package corpus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

func TestRunAgentActualPinnedHTMLAndHTTPFailures(t *testing.T) {
	manifests, data, err := DefaultAssets("agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id     string
		status int
	}{{"html-001", 200}, {"auth-basic-001", 401}, {"crash-001", 500}, {"spa-001", 200}} {
		t.Run(test.id, func(t *testing.T) {
			for _, manifest := range manifests {
				if !strings.HasSuffix(manifest.ID, ":"+test.id) {
					continue
				}
				result, err := RunAgent(t.Context(), manifest, data)
				if err != nil || !result.Passed || result.StatusCode != test.status || result.FixtureManifest.ID != manifest.ID || result.Text == "" {
					t.Fatalf("actual Agent outcome %s: status=%d title=%q passed=%t err=%v", test.id, result.StatusCode, result.Title, result.Passed, err)
				}
				return
			}
			t.Fatal("required actual source case missing")
		})
	}
}

func TestRunAgentRejectsForeignChangedAndUnownedInputs(t *testing.T) {
	manifests, data, err := DefaultAssets("agent")
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte{}, data...)
	changed[0] ^= 1
	if result, err := RunAgent(t.Context(), manifests[0], changed); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("changed source reached Agent execution")
	}
	foreign, source, err := DefaultAssets("renderless")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunAgent(t.Context(), foreign[0], source); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("foreign runner silently fell back")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := RunAgent(ctx, manifests[0], data); !errors.Is(err, context.Canceled) || result.Passed || result.StatusCode != 0 {
		t.Fatal("cancelled owner acquired coverage")
	}
	if result, err := RunAgent(nil, manifests[0], data); err == nil || result.Passed {
		t.Fatal("missing owner acquired coverage")
	}
}

func TestRunAgentPreservesActualChallengeDenial(t *testing.T) {
	manifests, data, err := DefaultAssets("agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if !strings.HasSuffix(manifest.ID, ":challenge-001") {
			continue
		}
		result, err := RunAgent(t.Context(), manifest, data)
		var failure *artemis.TaskError
		if err == nil || result.Passed || !errors.As(err, &failure) || failure.Code != artemis.TaskErrorExecutionFailed || !strings.Contains(err.Error(), "challenge_detected") {
			t.Fatalf("actual task failure was replaced: passed=%t err=%v", result.Passed, err)
		}
		return
	}
	t.Fatal("required actual challenge source missing")
}

func TestAgentActualAsyncBeforeEligibilityPromotion(t *testing.T) {
	manifests, content, err := DefaultAssets("agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if !strings.HasSuffix(manifest.ID, ":spa-001") {
			continue
		}
		scenario, err := fixture.CorpusScenario(manifest, content)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		result, err := runAdmittedAgent(ctx, manifest, content, scenario)
		if err != nil || result.StatusCode != 200 || !strings.Contains(result.Text, "Hello SPA") || result.FixtureManifest.ID != manifest.ID {
			t.Fatalf("actual admitted Agent SPA: status=%d text=%q err=%v", result.StatusCode, result.Text, err)
		}
		return
	}
	t.Fatal("required actual SPA case missing")
}
