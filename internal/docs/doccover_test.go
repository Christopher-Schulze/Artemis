// Package docs guards documentation.md against drift from the real code
// surface: exported product declarations are derived from source and checked
// against the documentation and an explicit ratchet baseline, and required
// sections are parsed as headings. Drift fixtures in surface_drift_test.go
// prove that each guard fails.
package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docPath returns the absolute path to documentation.md by walking up
// from the test working directory (the package dir) until it finds
// go.mod, then joining docs/documentation.md.
func docPath(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "documentation.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod above %s", cwd)
	return ""
}

// TestDocCoveragePublicSurface derives the public surface from source: every
// exported top-level function and type of an in-scope product package must
// be referenced as pkg.Name in documentation.md or listed in
// undocumented_exports.txt. The baseline is a ratchet: each entry must name a
// live, still undocumented declaration, so a new export cannot slip through
// and a documented export must leave the baseline.
func TestDocCoveragePublicSurface(t *testing.T) {
	path := docPath(t)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documentation.md: %v", err)
	}
	baseline, err := readBaseline("undocumented_exports.txt")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(path))
	problems, err := coverageProblems(root, string(content), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Errorf("documentation coverage drift (%d):\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// TestDocCoverageSections verifies that all required documentation
// sections exist in documentation.md.
func TestDocCoverageSections(t *testing.T) {
	content, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read documentation.md: %v", err)
	}
	doc := string(content)

	requiredSections := []string{
		"## Project Overview",
		"## License",
		"## Repository Layout",
		"## Build and Run",
		"## Configuration",
		"## CLI",
		"## Library API",
		"## JavaScript Execution",
		"## Fetch API",
		"## Browser Globals",
		"## Agent Extraction",
		"## Forms and Actions",
		"## Network Stack",
		"## Additional Web Globals",
		"## Hybrid Execution Router",
		"## Browser Provider Switch",
		"## CDP Bridge",
		"## Bridge Actions",
		"## CDP Operations",
		"## Tab Management",
		"## Renderless Engine",
		"## Stealth Layer",
		"## Observation Layer",
		"## Solver / CAPTCHA Pipeline",
		"## Human-like Input",
		"## Security Layer",
		"## Profile / Session Management",
		"## Scraper Subsystem",
		"## Prompts System",
		"## Actions / Auto-Login",
		"## Platform Capabilities",
		"## Steering Server",
		"## Telemetry",
	}

	present := headings(doc)
	var missing []string
	for _, section := range requiredSections {
		if !present[section] {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		t.Errorf("documentation.md is missing %d section heading(s):\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}
