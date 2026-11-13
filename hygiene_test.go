package artemis_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectHygieneFiles verifies that the public-facing project hygiene
// files exist and contain the minimum required
// sections. These files are part of the public release surface and must
// survive the subtree split.
func TestProjectHygieneFiles(t *testing.T) {
	root := "."

	files := []struct {
		path     string
		required []string
	}{
		{"LICENSE", []string{"MIT License", "Christopher Schulze"}},
		{"SECURITY.md", []string{"Supported Versions", "Reporting a Vulnerability", "Threat Model", "Vulnerability Response"}},
		{"CONTRIBUTING.md", []string{"Quick Start", "Local Gates", "Code Style", "Tests", "V8 / v8go Fork", "Security Issues", "License"}},
		{"CHANGELOG.md", []string{"Unreleased", "Added", "Changed", "Removed"}},
		{"README.md", []string{"Artemis"}},
		{filepath.Join("third_party", "v8go", "PROVENANCE.md"), []string{"github.com/tommie/v8go", "v0.39.0", "ARTEMIS_PATCHES.md", "LICENSE"}},
		{filepath.Join("third_party", "v8go", "ARTEMIS_PATCHES.md"), []string{"github.com/tommie/v8go", "SetProcessSnapshot", "LICENSE"}},
		{".github/workflows/ci.yml", []string{"test-linux", "runs-on: ubuntu-latest"}},
		{"docs/release-procedures.md", []string{"Signing", "Rollback", "Revocation", "Vulnerability Response", "Operator Gate"}},
	}

	for _, f := range files {
		t.Run(f.path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Clean(filepath.Join(root, f.path)))
			if err != nil {
				t.Fatalf("read %s: %v", f.path, err)
			}
			content := string(data)
			for _, req := range f.required {
				if !strings.Contains(content, req) {
					t.Errorf("%s missing required content: %q", f.path, req)
				}
			}
		})
	}
	// macOS is verified locally (make full); the public CI runs Linux only.
	for _, workflow := range []string{".github/workflows/ci.yml", ".github/workflows/full.yml"} {
		data, err := os.ReadFile(filepath.Join(root, workflow))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "macos") {
			t.Errorf("%s runs a macOS job; macOS is tested locally", workflow)
		}
	}
}

// TestGitignoreCoversBuildArtifacts ensures the .gitignore prevents
// committed build artifacts (the 51MB artemis binary, *.test binaries)
// from re-entering the tree.
func TestGitignoreCoversBuildArtifacts(t *testing.T) {
	data, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	required := []string{"/artemis", "*.test", "*.prof", "*.log"}
	for _, req := range required {
		if !strings.Contains(content, req) {
			t.Errorf(".gitignore missing required pattern: %q", req)
		}
	}
}

// TestStandaloneDoesNotContainPrivatePlanning checks the actual public
// checkout; internal planning may coexist only outside a standalone root.
func TestStandaloneDoesNotContainPrivatePlanning(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "scripts", "build", "split-artemis.sh")); err == nil {
		return
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, path := range []string{"docs/tasks.md", "docs/tasks"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("standalone checkout contains private planning %s: %v", path, err)
		}
	}
}
