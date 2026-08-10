package artemis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtemisHasNoLegacyProfileAuthority(t *testing.T) {
	forbidden := map[string]struct{}{
		"AutoLoginRegistry":    {},
		"AutoLoginSession":     {},
		"NewAutoLoginRegistry": {},
		"NewProfileRegistry":   {},
		"ProfileRegistry":      {},
		"ProfileSession":       {},
		"seedFromProfile":      {},
	}
	files := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(files, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				failOnLegacyProfileAuthority(t, files, value.Name, forbidden)
			case *ast.GenDecl:
				for _, specification := range value.Specs {
					switch spec := specification.(type) {
					case *ast.TypeSpec:
						failOnLegacyProfileAuthority(t, files, spec.Name, forbidden)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							failOnLegacyProfileAuthority(t, files, name, forbidden)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func failOnLegacyProfileAuthority(t *testing.T, files *token.FileSet, identifier *ast.Ident, forbidden map[string]struct{}) {
	t.Helper()
	if _, exists := forbidden[identifier.Name]; exists {
		t.Fatalf("legacy profile authority %s restored at %s", identifier.Name, files.Position(identifier.Pos()))
	}
}
