package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealedCohortRejectsUnknownAndDuplicateManifestFields(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	if _, err := NewHarness(config).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.OutputDir, "manifest.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, prefix string }{{"unknown", `{"unknown":true,`}, {"duplicate", `{"version":"1.0.0",`}} {
		t.Run(test.name, func(t *testing.T) {
			changed := append([]byte(test.prefix), original[1:]...)
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadRawArtifacts(config.OutputDir); err == nil {
				t.Fatal("noncanonical manifest acquired accepted cohort identity")
			}
		})
	}
}
