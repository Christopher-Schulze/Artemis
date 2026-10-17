package benchmark

import (
	"strings"
	"testing"
)

func TestManifestRecordsExecutedOptionsAndProfiles(t *testing.T) {
	dir := t.TempDir()
	options, err := ParseOptions([]string{"--output=" + dir, "--format=summary", "--iterations=2", "--benchmark-tag=provenance",
		"--cpu-profile=" + dir + "/cpu.pprof", "--mem-profile=" + dir + "/heap.pprof", "--timeout=30s"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHarness(options.Config)
	h.scenarios = DefaultScenarios()[:1]
	sc, err := h.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if sc.Options.OutputFormat != "summary" || sc.Options.Timeout != "30s" || sc.Options.Iterations != 2 || sc.Options.BenchmarkTag != "provenance" || !sc.Options.SkipCompetitor {
		t.Fatalf("option manifest lost fields: %+v", sc.Options)
	}
	if !validSHA256(sc.Provenance.ArtemisSHA256) || sc.Provenance.SourceRevision == "" || sc.Provenance.SourceTreeState == "" || len(sc.Scenarios) != 1 || !validSHA256(sc.Scenarios[0].HTMLSHA256) {
		t.Fatal("executed code or corpus provenance missing")
	}
	if _, err := ReadRawArtifacts(dir); err != nil {
		t.Fatalf("flushed profile cohort: %v", err)
	}
	if !strings.Contains(downloadOrigin("https://example.com/asset?token=private#secret"), "/asset") || strings.Contains(downloadOrigin("https://example.com/asset?token=private#secret"), "private") {
		t.Fatal("download origin leaked credentials")
	}
}
