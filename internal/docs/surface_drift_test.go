package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageDetectsDeclarationDocumentationAndBaselineDrift(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "widget/widget.go", "package widget\n\nfunc New() {}\n\ntype Widget struct{}\n\ntype hidden struct{}\n\nfunc (Widget) Method() {}\n")
	writeFixture(t, root, "internal/skip/skip.go", "package skip\n\nfunc Exported() {}\n")
	doc := "Use `widget.New` to build a `widget.Widget`."
	if problems, err := coverageProblems(root, doc, map[string]bool{}); err != nil || len(problems) != 0 {
		t.Fatalf("documented surface reported drift: %v %v", problems, err)
	}
	writeFixture(t, root, "widget/extra.go", "package widget\n\nfunc Added() {}\n")
	problems, err := coverageProblems(root, doc, map[string]bool{})
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "undocumented export: widget:Added") {
		t.Fatalf("new export without documentation passed: %v %v", problems, err)
	}
	if problems, _ := coverageProblems(root, strings.Replace(doc, "widget.New", "widget.NewThing", 1), map[string]bool{"widget:Added": true}); len(problems) != 1 || !strings.Contains(problems[0], "widget:New") {
		t.Fatalf("removed documentation passed (prefix match must not count): %v", problems)
	}
	if problems, _ := coverageProblems(root, doc+" widget.Added", map[string]bool{"widget:Added": true}); len(problems) != 1 || !strings.Contains(problems[0], "documented export still in baseline") {
		t.Fatalf("stale baseline entry passed: %v", problems)
	}
	if problems, _ := coverageProblems(root, doc+" widget.Added", map[string]bool{"widget:Gone": true}); len(problems) != 1 || !strings.Contains(problems[0], "no live declaration") {
		t.Fatalf("dead baseline entry passed: %v", problems)
	}
}

func TestHeadingsAreParsedStructurally(t *testing.T) {
	doc := "# Title\n\nSee ## Telemetry in prose.\n### Telemetry\n```\n## Fenced\n```\n## Real Section\n"
	got := headings(doc)
	if !got["## Real Section"] || got["## Telemetry"] || got["## Fenced"] || len(got) != 1 {
		t.Fatalf("headings = %v", got)
	}
}
