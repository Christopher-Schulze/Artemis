package corpus

import (
	"testing"

	"github.com/Christopher-Schulze/Artemis/renderless"
)

func TestCSSProjectionKeepsIndependentConformanceReferences(t *testing.T) {
	base := renderless.GenerateCapabilityProfile(renderless.EngineConfig{}, renderless.NewWebAPIRegistry())
	if len(base.Conformance) == 0 || len(base.Conformance[0].Tests) == 0 {
		t.Fatal("current source/test conformance is missing")
	}
	base.ConformanceCoverage = renderless.ConformanceCoverage{
		DeclaredTests: 1,
		UnmappedTests: []renderless.ConformanceTestReference{base.Conformance[0].Tests[0]},
	}
	derived := cssProfileWithEvidence(base, renderless.CSSConformanceEvidence{})
	scope, test := base.Conformance[0].Scope, base.Conformance[0].Tests[0].Test
	derived.Conformance[0].Scope = "caller changed scope"
	derived.Conformance[0].Tests[0].Test = "caller changed test"
	derived.ConformanceCoverage.UnmappedTests[0].Test = "caller changed omission"
	if len(base.BindingCapabilities) == 0 {
		t.Fatal("source binding classifications are missing")
	}
	binding := base.BindingCapabilities[0]
	derived.BindingCapabilities[0].Path = "caller changed descriptor"
	if base.BindingCapabilities[0] != binding {
		t.Fatal("CSS projection changed its source descriptor classification")
	}
	if base.ConformanceCoverage.UnmappedTests[0].Test != test || derived.ConformanceCoverage.DeclaredTests != 1 {
		t.Fatal("CSS projection changed its source coverage or lost coverage counts")
	}
	if base.Conformance[0].Scope != scope || base.Conformance[0].Tests[0].Test != test || derived.ClassificationSourceHash != base.ClassificationSourceHash {
		t.Fatal("CSS projection changed its source profile or lost source identity")
	}
}
