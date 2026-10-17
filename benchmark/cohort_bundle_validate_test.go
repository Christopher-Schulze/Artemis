package benchmark

import (
	"bytes"
	"testing"
)

func TestArtifactBundleValidationRejectsChangedCompleteActualOutputs(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	if _, err := NewHarness(config).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err := ReadArtifactBundle(t.Context(), config.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateArtifactBundle(bundle); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"manifest", "receipt", "missing", "extra", "order", "bytes", "digest"} {
		t.Run(change, func(t *testing.T) {
			changed := bundle
			changed.Manifest = bytes.Clone(bundle.Manifest)
			changed.Files = append([]ArtifactFile{}, bundle.Files...)
			switch change {
			case "manifest":
				changed.Manifest[0] = '['
			case "receipt":
				changed.Receipt.Version = "2.0.0"
			case "missing":
				changed.Files = changed.Files[:2]
			case "extra":
				changed.Files = append(changed.Files, changed.Files[0])
			case "order":
				changed.Files[0], changed.Files[1] = changed.Files[1], changed.Files[0]
			case "bytes":
				changed.Files[0].Content = bytes.Clone(changed.Files[0].Content)
				changed.Files[0].Content[0] ^= 1
			case "digest":
				changed.Files[0].Digest.Bytes++
			}
			if err := ValidateArtifactBundle(changed); err == nil {
				t.Fatal("changed complete output bundle was admitted")
			}
		})
	}
}
