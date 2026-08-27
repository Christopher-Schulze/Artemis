package artemis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestReleaseIdentityAndClaimsDoNotDrift(t *testing.T) {
	if Version != "0.1.0-alpha.1" {
		t.Fatalf("Version = %q", Version)
	}
	if !strings.Contains(engine.DefaultUserAgent, Version) {
		t.Fatalf("DefaultUserAgent %q does not contain version %q", engine.DefaultUserAgent, Version)
	}

	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	license := read("LICENSE")
	if !strings.HasPrefix(license, "MIT License\n") {
		t.Fatal("LICENSE is not MIT")
	}
	module := read("go.mod")
	if !strings.HasPrefix(module, "module github.com/Christopher-Schulze/Artemis\n") {
		t.Fatal("go.mod module identity drifted")
	}
	readme := read("README.md")
	documentation := read("docs/documentation.md")
	for _, required := range []string{Version, "artemis capabilities", "Chromium/CDP", "unavailable"} {
		if !strings.Contains(readme, required) {
			t.Fatalf("README missing release truth %q", required)
		}
		if !strings.Contains(documentation, required) {
			t.Fatalf("documentation missing release truth %q", required)
		}
	}
	for _, forbidden := range []string{"Hybrid engine, automatic routing", "2.7x faster", "Serious stealth"} {
		if strings.Contains(readme, forbidden) {
			t.Fatalf("README contains unsupported claim %q", forbidden)
		}
	}
}

func TestSupportedCapabilitiesNameBehaviorEvidence(t *testing.T) {
	for _, capability := range Capabilities() {
		if capability.State != SupportSupported {
			continue
		}
		if capability.Since != Version || capability.BehaviorTest == "" || capability.Owner == "" || capability.Entrypoint == "" {
			t.Fatalf("supported capability lacks release evidence: %+v", capability)
		}
	}
}
