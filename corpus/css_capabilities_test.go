package corpus

import (
	"context"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/renderless"
	"github.com/Christopher-Schulze/Artemis/router"
)

func actualCSSCapabilityResults(t *testing.T) []Result {
	t.Helper()
	var results []Result
	for _, runner := range []string{"renderless", "chromium"} {
		manifests, content, err := DefaultAssets(runner)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range CSSConformanceCaseIDs() {
			found := false
			for _, manifest := range manifests {
				if !strings.HasSuffix(manifest.ID, ":"+id) {
					continue
				}
				found = true
				var result Result
				if runner == "renderless" {
					result, err = RunRenderless(t.Context(), manifest, content)
				} else {
					result, err = RunChromium(t.Context(), manifest, content, process.LaunchConfig{})
				}
				if err != nil || !result.Passed {
					t.Fatalf("actual CSS %s/%s: %s %v", runner, id, result.EvalResult, err)
				}
				results = append(results, result)
				break
			}
			if !found {
				t.Fatalf("required current CSS case missing: %s", id)
			}
		}
	}
	return results
}

func TestCSSCapabilityProfileUsesPairedActualSourcePinnedObservations(t *testing.T) {
	results := actualCSSCapabilityResults(t)
	base := renderless.GenerateCapabilityProfile(renderless.EngineConfig{}, renderless.NewWebAPIRegistry())
	profile, err := GenerateCSSCapabilityProfile(t.Context(), base, results)
	if err != nil || profile.CSSConformance == nil || len(profile.CSSConformance.Observations) != 6 {
		t.Fatalf("complete current CSS proof rejected: %+v %v", profile.CSSConformance, err)
	}
	for _, feature := range []string{"parser", "selectors", "cascade", "computed-style", "inline-style"} {
		if !profile.SupportsCSS(feature) || base.SupportsCSS(feature) {
			t.Fatalf("CSS support was invented or not derived: %s", feature)
		}
	}
	if profile.RequiresEscalation("getComputedStyle", true) || !base.RequiresEscalation("getComputedStyle", true) || !profile.RequiresEscalation("CSSStyleSheet", true) {
		t.Fatal("CSS proof changed the wrong capability boundary")
	}
	for _, observation := range profile.CSSConformance.Observations {
		if len(observation.ResultHash) != 64 || observation.FixtureID == "" || observation.EvidenceRef != "" {
			t.Fatalf("unbound or invented observation metadata: %+v", observation)
		}
	}
	assertCSSCapabilityNegativeControls(t, base, results)
	assertCSSCapabilityRouting(t, base, profile)
}

func assertCSSCapabilityRouting(t *testing.T, base, admitted renderless.RenderlessCapabilityProfile) {
	t.Helper()
	backend, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	routes, err := router.New(router.Config{Executors: map[router.Mode]router.Executor{router.ModeRenderlessJS: router.RenderlessExecutor{Engine: backend, RunScripts: true}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		profile  renderless.RenderlessCapabilityProfile
		api      string
		mode     router.Mode
		category renderless.CapabilityCategory
	}{
		{"unproved", base, "getComputedStyle", router.ModeChromiumCDP, renderless.CategoryUnsupportedEscalate},
		{"paired proof", admitted, "getComputedStyle", router.ModeRenderlessJS, renderless.CategorySupportedReal},
		{"absent constructor", admitted, "CSSStyleSheet", router.ModeChromiumCDP, renderless.CategoryUnsupportedEscalate},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision, err := routes.Decide(router.RouteRequest{URL: "https://example.test/css", Signals: router.Signals{IsHTML: true, ScriptCount: 1}, RequiredWebAPIs: []string{test.api}, Capabilities: test.profile, NeedsRealSemantics: true})
			if err != nil || decision.Mode != test.mode || len(decision.Capabilities) != 1 || decision.Capabilities[0].Category != string(test.category) || decision.Capabilities[0].Escalate != (test.mode == router.ModeChromiumCDP) {
				t.Fatalf("generated CSS proof changed routing/evidence incorrectly: %+v %v", decision, err)
			}
		})
	}
}

func assertCSSCapabilityNegativeControls(t *testing.T, base renderless.RenderlessCapabilityProfile, results []Result) {
	t.Helper()
	for _, test := range []struct {
		name string
		edit func([]Result) []Result
	}{
		{"missing", func(r []Result) []Result { return r[:len(r)-1] }},
		{"duplicate", func(r []Result) []Result { r[1] = r[0]; return r }},
		{"failed", func(r []Result) []Result { r[0].Passed = false; return r }},
		{"false assertion", func(r []Result) []Result { r[0].EvalResult = "false"; return r }},
		{"foreign source", func(r []Result) []Result { r[0].FixtureManifest.AssetHash = strings.Repeat("0", 64); return r }},
		{"foreign runner", func(r []Result) []Result { r[0].FixtureManifest.RunnerRef = "artemis:fixture:agent"; return r }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := test.edit(append([]Result(nil), results...))
			profile, err := GenerateCSSCapabilityProfile(t.Context(), base, changed)
			if err == nil || len(profile.CSSSupport) != 0 || profile.CSSConformance != nil {
				t.Fatalf("invalid proof acquired CSS capability: %+v %v", profile.CSSConformance, err)
			}
		})
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	profile, err := GenerateCSSCapabilityProfile(cancelled, base, results)
	if err == nil || len(profile.CSSSupport) != 0 {
		t.Fatal("cancelled owner acquired positive CSS capability")
	}
}
