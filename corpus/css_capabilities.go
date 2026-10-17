package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/renderless"
)

// CSSConformanceCaseIDs selects the existing source-pinned semantic cases.
func CSSConformanceCaseIDs() []string { return []string{"css-002", "css-003", "css-004"} }

// GenerateCSSCapabilityProfile validates paired native/renderless observations
// against current compiled source before projecting the admitted CSS subset.
// Result structs do not authenticate producers; the embedding authority must
// admit retained receipts before supplying them. No execution occurs here.
func GenerateCSSCapabilityProfile(ctx context.Context, base renderless.RenderlessCapabilityProfile, results []Result) (renderless.RenderlessCapabilityProfile, error) {
	if ctx == nil || len(results) != 2*len(CSSConformanceCaseIDs()) {
		return renderless.RenderlessCapabilityProfile{}, fmt.Errorf("CSS capability: complete paired observations and context required")
	}
	if err := ctx.Err(); err != nil {
		return renderless.RenderlessCapabilityProfile{}, err
	}
	_, content, err := DefaultAssets("renderless")
	if err != nil {
		return renderless.RenderlessCapabilityProfile{}, err
	}
	metadata, err := cssCapabilityObservations(ctx, content, results)
	if err != nil {
		return renderless.RenderlessCapabilityProfile{}, err
	}
	if err := ctx.Err(); err != nil {
		return renderless.RenderlessCapabilityProfile{}, err
	}
	return cssProfileWithEvidence(base, metadata), nil
}

func cssProfileWithEvidence(base renderless.RenderlessCapabilityProfile, metadata renderless.CSSConformanceEvidence) renderless.RenderlessCapabilityProfile {
	profile := base
	profile.WebAPIs = withoutCSSAPIs(base.WebAPIs)
	profile.SupportedReal = withoutCSSAPIs(base.SupportedReal)
	profile.StubCompatible = withoutCSSAPIs(base.StubCompatible)
	profile.UnsupportedEscalate = withoutCSSAPIs(base.UnsupportedEscalate)
	profile.ScriptTypes = append([]string{}, base.ScriptTypes...)
	profile.Conformance = append([]renderless.WebAPIConformanceEvidence{}, base.Conformance...)
	profile.ConformanceCoverage.UnmappedTests = slices.Clone(base.ConformanceCoverage.UnmappedTests)
	profile.BindingCapabilities = slices.Clone(base.BindingCapabilities)
	for index := range profile.Conformance {
		profile.Conformance[index].Tests = append([]renderless.ConformanceTestReference{}, base.Conformance[index].Tests...)
	}
	profile.CSSSupport = []string{"parser", "selectors", "cascade", "computed-style", "inline-style"}
	profile.CSSConformance = &metadata
	if !slices.Contains(profile.WebAPIs, "getComputedStyle") {
		profile.WebAPIs = append(profile.WebAPIs, "getComputedStyle")
	}
	profile.SupportedReal = append(profile.SupportedReal, "getComputedStyle")
	profile.UnsupportedEscalate = append(profile.UnsupportedEscalate, "CSSStyleSheet", "CSSStyleDeclaration")
	return profile
}

func withoutCSSAPIs(names []string) []string {
	return slices.DeleteFunc(append([]string{}, names...), func(name string) bool {
		return strings.EqualFold(name, "CSSStyleSheet") || strings.EqualFold(name, "CSSStyleDeclaration") || strings.EqualFold(name, "getComputedStyle")
	})
}

func cssCapabilityObservations(ctx context.Context, content []byte, results []Result) (renderless.CSSConformanceEvidence, error) {
	metadata := renderless.CSSConformanceEvidence{}
	var err error
	metadata.CSSHash, err = css.SourceHash()
	if err != nil {
		return metadata, err
	}
	metadata.BindingHash, err = js.SourceHash()
	if err != nil {
		return metadata, err
	}
	seen := make(map[string]bool)
	for _, result := range results {
		manifest := result.FixtureManifest
		scenario, err := fixture.CorpusScenario(manifest, content)
		if err != nil {
			return metadata, err
		}
		runner := strings.TrimPrefix(manifest.RunnerRef, "artemis:fixture:")
		key := runner + "/" + scenario.ID
		if (runner != "renderless" && runner != "chromium") || !slices.Contains(CSSConformanceCaseIDs(), scenario.ID) || seen[key] || !result.Passed {
			return metadata, fmt.Errorf("CSS capability: exact passing native/renderless case pairs required")
		}
		if err := ValidateResult(ctx, manifest, content, result); err != nil {
			return metadata, err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return metadata, err
		}
		seen[key] = true
		metadata.SourceHash = manifest.AssetHash
		metadata.Observations = append(metadata.Observations, renderless.CSSConformanceObservation{CaseID: scenario.ID, Runner: runner, FixtureID: manifest.ID, ResultHash: fmt.Sprintf("%x", sha256.Sum256(encoded))})
	}
	sort.Slice(metadata.Observations, func(i, j int) bool { return cssObservationLess(metadata.Observations[i], metadata.Observations[j]) })
	return metadata, ctx.Err()
}

func cssObservationLess(left, right renderless.CSSConformanceObservation) bool {
	if left.CaseID == right.CaseID {
		return left.Runner < right.Runner
	}
	return left.CaseID < right.CaseID
}
