package renderless

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestConformanceSourcesAndCasesAreCurrent(t *testing.T) {
	compiled, err := currentConformance()
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.sourceHash) != 64 || len(compiled.cases) == 0 {
		t.Fatal("classification lacks source/case identity")
	}
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if profile.ConformanceError != "" || profile.ClassificationSourceHash != compiled.sourceHash || len(profile.Conformance) == 0 {
		t.Fatalf("profile lost compiled test linkage: %+v error=%v", profile, registry.conformanceError)
	}
	cases := make(map[string]WebAPIConformanceEvidence)
	for _, evidence := range profile.Conformance {
		if evidence.CaseID == "" || evidence.Scope == "" || len(evidence.Tests) == 0 {
			t.Fatalf("empty conformance claim: %+v", evidence)
		}
		for _, reference := range evidence.Tests {
			if reference.Test == "" || reference.File == "" || reference.Line <= 0 || len(reference.SourceHash) != 64 {
				t.Fatalf("test reference lacks actual declaration/source: %+v", reference)
			}
		}
		cases[evidence.CaseID] = evidence
	}
	if profile.CapabilityMatch("element") != CategorySupportedReal || profile.CapabilityMatch("Element") != CategorySupportedReal {
		t.Fatal("source-declared aliases in the same conformance case conflicted")
	}
	for _, api := range registry.All() {
		if _, exists := cases[api.ConformanceCase]; !exists {
			t.Fatalf("registered API lacks a mapped test case: %+v", api)
		}
	}
	profile.Conformance[0].Tests[0].Test = "forged"
	if next := GenerateCapabilityProfile(EngineConfig{}, registry); reflect.DeepEqual(profile.Conformance, next.Conformance) {
		t.Fatal("caller mutation changed cached conformance references")
	}
}

func TestConformanceDefinitionsRejectMissingTestsAndConflictingOwners(t *testing.T) {
	digest, references, err := loadConformanceSourceReferences()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing-test", "duplicate-case", "conflicting-api", "invalid-category", "missing-scope"} {
		t.Run(kind, func(t *testing.T) {
			definitions := conformanceDefinitions()
			switch kind {
			case "missing-test":
				definitions[1].tests = "js.TestDoesNotExist"
			case "duplicate-case":
				definitions = append(definitions, definitions[1])
			case "conflicting-api":
				definitions[2].names += " FETCH"
			case "invalid-category":
				definitions[1].category = "invalid"
			case "missing-scope":
				definitions[1].scope = ""
			}
			if _, err := compileConformanceDefinitions(digest, references, definitions); err == nil {
				t.Fatalf("invalid classification source accepted: %s", kind)
			}
		})
	}
}

func TestConformanceReferencesUseActualTestDeclarations(t *testing.T) {
	const source = "package fixture\nimport tt \"testing\"\nfunc TestActual(t *tt.T) { t.Fatal(\"actual assertion\") }\n"
	references := make(map[string]ConformanceTestReference)
	if err := collectTestDeclarations([]byte(source), "fixture", "actual_test.go", references); err != nil {
		t.Fatal(err)
	}
	reference, exists := references["fixture.TestActual"]
	if !exists || reference.Line != 3 || reference.File != "actual_test.go" || reference.SourceHash != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
		t.Fatalf("test reference fabricated declaration/source details: %+v", reference)
	}
	for _, invalid := range []string{
		strings.Replace(source, "*tt.T", "*string", 1),
		strings.Replace(source, "t.Fatal(\"actual assertion\")", "", 1),
		strings.Replace(source, "func TestActual", "invalid TestActual", 1),
	} {
		if err := collectTestDeclarations([]byte(invalid), "fixture", "actual_test.go", make(map[string]ConformanceTestReference)); err == nil {
			t.Fatalf("invalid test declaration became evidence: %s", invalid)
		}
	}
	if err := collectTestDeclarations([]byte(source), "fixture", "other_test.go", references); err == nil {
		t.Fatal("ambiguous test name became evidence")
	}
}

func TestConformanceAdmissionCannotBeReclassifiedByCaller(t *testing.T) {
	registry := NewWebAPIRegistry()
	registry.Register(WebAPIGlobal{Name: "Worker", Implemented: true, Category: CategorySupportedReal, ConformanceCase: "dom"})
	definition, exists := registry.Get("Worker")
	if !exists || definition.Category != CategoryUnsupportedEscalate || definition.ConformanceCase != "binding-admission" {
		t.Fatalf("caller forged a semantic classification: %+v", definition)
	}
	registry.conformance = nil
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if profile.IsCapable() || profile.FetchSupport || profile.CookieJar || profile.ConformanceError == "" {
		t.Fatalf("missing semantic sources produced supported engine: %+v", profile)
	}
}

func TestSyntheticConformanceRequiresEscalationForEveryRealSemanticRequest(t *testing.T) {
	profile := GenerateCapabilityProfile(EngineConfig{}, NewWebAPIRegistry())
	for _, api := range []string{"canvas", "Element.animate", "IntersectionObserver", "ResizeObserver", "Worker", "SharedWorker", "visualViewport", "screen", "matchMedia"} {
		if profile.CapabilityMatch(api) != CategoryStubCompatible || !profile.RequiresEscalation(api, true) || profile.RequiresEscalation(api, false) {
			t.Fatalf("synthetic API acquired real semantics or lost compatibility: %s", api)
		}
	}
}
