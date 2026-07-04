package benchmark

import "testing"

// BenchmarkArtemisFetchNav measures the full fetch→parse→extract
// pipeline for a navigation scenario.
func BenchmarkArtemisFetchNav(b *testing.B) {
	s := *ScenarioByID("nav-001")
	r := NewArtemisRunner([]Scenario{s})
	defer r.Close()
	r.RunScenarioBench(b, s)
}

// BenchmarkArtemisFetchScrape measures the full pipeline for a
// content-rich scrape scenario.
func BenchmarkArtemisFetchScrape(b *testing.B) {
	s := *ScenarioByID("scr-001")
	r := NewArtemisRunner([]Scenario{s})
	defer r.Close()
	r.RunScenarioBench(b, s)
}

// BenchmarkArtemisFetchScriptHeavy measures the full pipeline including
// V8 script execution for a script-heavy page.
func BenchmarkArtemisFetchScriptHeavy(b *testing.B) {
	s := *ScenarioByID("scr-003")
	r := NewArtemisRunner([]Scenario{s})
	defer r.Close()
	r.RunScenarioBench(b, s)
}

// BenchmarkArtemisFetchHeavyJS measures the full pipeline with 20
// inline scripts, exercising V8 context pool reuse.
func BenchmarkArtemisFetchHeavyJS(b *testing.B) {
	s := *ScenarioByID("scr-004")
	r := NewArtemisRunner([]Scenario{s})
	defer r.Close()
	r.RunScenarioBench(b, s)
}

// BenchmarkParseOnlyNav measures parse + extract without network I/O
// for a navigation scenario.
func BenchmarkParseOnlyNav(b *testing.B) {
	s := *ScenarioByID("nav-001")
	ParseOnlyBench(b, s)
}

// BenchmarkParseOnlyScrape measures parse + extract without network I/O
// for a content-rich scrape scenario.
func BenchmarkParseOnlyScrape(b *testing.B) {
	s := *ScenarioByID("scr-001")
	ParseOnlyBench(b, s)
}

// BenchmarkParseOnlyMarkdown measures parse + markdown conversion
// for the markdown test page.
func BenchmarkParseOnlyMarkdown(b *testing.B) {
	s := *ScenarioByID("md-001")
	ParseOnlyBench(b, s)
}

// BenchmarkParseOnlyComplexMarkdown measures parse + markdown conversion
// for the complex nested markdown page.
func BenchmarkParseOnlyComplexMarkdown(b *testing.B) {
	s := *ScenarioByID("md-002")
	ParseOnlyBench(b, s)
}

// BenchmarkDOMQuery measures DOM query performance (getElementById,
// getElementsByTagName, getElementsByClassName) on a DOM-heavy page.
func BenchmarkDOMQuery(b *testing.B) {
	s := *ScenarioByID("dom-001")
	DOMQueryBench(b, s)
}

// BenchmarkDOMQueryForms measures DOM query performance on a
// form-heavy page.
func BenchmarkDOMQueryForms(b *testing.B) {
	s := *ScenarioByID("dom-002")
	DOMQueryBench(b, s)
}
