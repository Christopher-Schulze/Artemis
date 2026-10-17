package css

import (
	"bytes"
	"os"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
)

func TestCSSSourceHashBindsEveryCompiledImplementationAndTest(t *testing.T) {
	entries, err := conformanceSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		actual, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := conformanceSources.ReadFile(entry.Name())
		if err != nil || !bytes.Equal(actual, embedded) {
			t.Fatalf("compiled CSS source drift: %s %v", entry.Name(), err)
		}
	}
	expected, err := sourcepin.Hash(conformanceSources)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := SourceHash()
	if err != nil || actual != expected {
		t.Fatalf("CSS source/test pin omitted inputs: %q %q %v", actual, expected, err)
	}
}
