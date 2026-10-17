package renderless

import (
	"slices"
	"testing"
)

func TestConformanceCoverageAccountsForEveryRequiredSourceFamily(t *testing.T) {
	_, references, err := loadConformanceSourceReferences()
	if err != nil {
		t.Fatal(err)
	}
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	coverage := profile.ConformanceCoverage
	if coverage.DeclaredTests != len(references) || coverage.MappedTests+len(coverage.UnmappedTests) != len(references) {
		t.Fatalf("test coverage counts omitted source: %+v", coverage)
	}
	mapped := make(map[string]bool)
	for _, evidence := range registry.conformance.cases {
		for _, reference := range evidence.Tests {
			mapped[reference.Suite+"."+reference.Test] = true
		}
	}
	unmapped := make(map[string]bool)
	keys := make([]string, 0, len(coverage.UnmappedTests))
	for _, reference := range coverage.UnmappedTests {
		key := reference.Suite + "." + reference.Test
		if unmapped[key] || mapped[key] || references[key] != reference {
			t.Fatalf("unmapped declaration is duplicated, invented or already mapped: %s", key)
		}
		unmapped[key] = true
		keys = append(keys, key)
	}
	if !slices.IsSorted(keys) || len(mapped) != coverage.MappedTests {
		t.Fatal("coverage is nondeterministic or inflated by duplicate mappings")
	}
	owners := make(map[string]int)
	for key, reference := range references {
		owners[reference.Suite]++
		if !mapped[key] && !unmapped[key] {
			t.Fatalf("declared test vanished from classification coverage: %s", key)
		}
	}
	for _, suite := range []string{"js", "engine", "agent", "network", "css", "webapi", "renderless"} {
		if owners[suite] == 0 {
			t.Fatalf("required source family absent: %s", suite)
		}
	}
	t.Logf("declared=%d mapped=%d unmapped=%d suites=%v", coverage.DeclaredTests, coverage.MappedTests, len(coverage.UnmappedTests), owners)
}

func TestConformanceCoverageCountsSharedReferencesOnceAndDetachesProfile(t *testing.T) {
	_, references, err := loadConformanceSourceReferences()
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(references))
	for key := range references {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	first, second := references[keys[0]], references[keys[1]]
	selected := map[string]ConformanceTestReference{keys[0]: first, keys[1]: second}
	cases := map[string]WebAPIConformanceEvidence{"one": {Tests: []ConformanceTestReference{first}}, "two": {Tests: []ConformanceTestReference{first}}}
	coverage := conformanceCoverage(selected, cases)
	if coverage.DeclaredTests != 2 || coverage.MappedTests != 1 || len(coverage.UnmappedTests) != 1 || coverage.UnmappedTests[0] != second {
		t.Fatalf("shared evidence inflated coverage or lost omission: %+v", coverage)
	}
	registry := NewWebAPIRegistry()
	compiled := *registry.conformance
	compiled.coverage = coverage
	registry.conformance = &compiled
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	profile.ConformanceCoverage.UnmappedTests[0].Test = "caller mutation"
	if next := GenerateCapabilityProfile(EngineConfig{}, registry); next.ConformanceCoverage.UnmappedTests[0] != second {
		t.Fatal("caller changed compiled coverage source")
	}
}
