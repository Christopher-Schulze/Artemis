package v8go

import (
	"os"
	"strings"
	"testing"
)

// TestProvenanceDocument verifies that the PROVENANCE.md file exists and
// contains the minimum required fields for redistribution and reproduction.
func TestProvenanceDocument(t *testing.T) {
	data, err := os.ReadFile("PROVENANCE.md")
	if err != nil {
		t.Fatalf("read PROVENANCE.md: %v", err)
	}
	content := string(data)

	required := []string{
		"rogchap.com/v8go",
		"v0.9.0",
		"https://github.com/rogchap/v8go",
		"ARTEMIS_PATCHES.md",
		"deps/darwin_arm64/libv8.a",
		"deps/linux_x86_64/libv8.a",
		"LICENSE",
	}
	for _, s := range required {
		if !strings.Contains(content, s) {
			t.Errorf("PROVENANCE.md missing required field: %q", s)
		}
	}
}

// TestLicenseExists ensures the upstream license file is preserved.
func TestLicenseExists(t *testing.T) {
	if _, err := os.Stat("LICENSE"); err != nil {
		t.Fatalf("LICENSE file not found: %v", err)
	}
}
