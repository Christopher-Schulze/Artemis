package agent

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAgentPackageCannotOwnBrowserComposition(t *testing.T) {
	violations, err := browserCompositionViolations(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("parallel browser composition restored: %s", strings.Join(violations, "; "))
	}
}

func TestBrowserCompositionGuardRejectsParallelOwner(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "chromium.go")
	source := []byte("package agent\nimport _ \"github.com/Christopher-Schulze/Artemis/bridge/actions\"\n")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	violations, err := browserCompositionViolations(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "bridge/actions.Runtime owns Chromium actions") {
		t.Fatalf("guard accepted parallel action ownership: %v", violations)
	}
}

func browserCompositionViolations(directory string) ([]string, error) {
	forbiddenImports := map[string]string{
		"github.com/Christopher-Schulze/Artemis/bridge/actions": "bridge/actions.Runtime owns Chromium actions",
		"github.com/Christopher-Schulze/Artemis/bridge/observe": "bridge/observe.Collector owns Chromium observations",
		"github.com/Christopher-Schulze/Artemis/process":        "process.Browser owns Chromium process lifecycle",
		"github.com/Christopher-Schulze/Artemis/profile":        "profile runtimes own browser sessions",
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	files := token.NewFileSet()
	var violations []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file, parseErr := parser.ParseFile(files, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if parseErr != nil {
			return nil, parseErr
		}
		for _, importSpec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				return nil, unquoteErr
			}
			owner, forbidden := forbiddenImports[importPath]
			if forbidden {
				violations = append(violations, files.Position(importSpec.Pos()).String()+": agent imports "+importPath+"; "+owner)
			}
		}
	}
	return violations, nil
}
