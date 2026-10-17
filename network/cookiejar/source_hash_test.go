package cookiejar

import (
	"bytes"
	"os"
	"runtime"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
)

func TestSourceHashIncludesCurrentCookieOwnerTestsAndLicense(t *testing.T) {
	entries, err := sourceInputs.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		current, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := sourceInputs.ReadFile(entry.Name())
		if err != nil || !bytes.Equal(current, compiled) {
			t.Fatalf("cookie owner source omitted/stale: %s %v", entry.Name(), err)
		}
	}
	want, err := sourcepin.Hash(sourceInputs, runtime.Version())
	if err != nil {
		t.Fatal(err)
	}
	got, err := SourceHash()
	if err != nil || got != want {
		t.Fatalf("cookie source pin: %q %q %v", got, want, err)
	}
	changed, err := sourcepin.Hash(sourceInputs, "different-go-toolchain")
	if err != nil || changed == got {
		t.Fatalf("toolchain mutation did not change pin: %q %v", changed, err)
	}
}
