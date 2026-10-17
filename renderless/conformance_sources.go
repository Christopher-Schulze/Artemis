package renderless

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"runtime"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

//go:embed *.go
var capabilitySources embed.FS

// ConformanceTestReference identifies an executable test declaration in current
// compiled source. It is not a test-run receipt or producer authentication.
type ConformanceTestReference struct {
	Suite      string `json:"suite"`
	Test       string `json:"test"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	SourceHash string `json:"source_hash"`
}

func loadConformanceSourceReferences() (string, map[string]ConformanceTestReference, error) {
	references := make(map[string]ConformanceTestReference)
	identities := []string{runtime.Version(), runtime.GOOS, runtime.GOARCH}
	jsHash, err := js.SourceHash()
	if err != nil {
		return "", nil, err
	}
	identities = append(identities, jsHash)
	for _, source := range []struct {
		suite string
		files embed.FS
	}{{"js", js.BindingSources()}, {"engine", engine.BindingSources()}, {"agent", agent.BindingSources()}, {"network", network.BindingSources()}, {"css", css.BindingSources()}, {"webapi", webapi.BindingSources()}, {"renderless", capabilitySources}} {
		digest, err := sourcepin.Hash(source.files, source.suite)
		if err != nil {
			return "", nil, err
		}
		identities = append(identities, source.suite, digest)
		if err := collectConformanceTests(source.files, source.suite, references); err != nil {
			return "", nil, err
		}
	}
	digest, err := sourcepin.Hash(capabilitySources, identities...)
	return digest, references, err
}

func collectConformanceTests(files fs.FS, suite string, references map[string]ConformanceTestReference) error {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			return err
		}
		if err := collectTestDeclarations(data, suite, entry.Name(), references); err != nil {
			return err
		}
	}
	return nil
}

func collectTestDeclarations(data []byte, suite, name string, references map[string]ConformanceTestReference) error {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, name, data, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	testingName := testingImportName(file)
	sourceHash := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") || function.Name.Name == "TestMain" {
			continue
		}
		if !isExecutableTest(function, testingName) {
			return fmt.Errorf("conformance: invalid test declaration %s/%s:%s", suite, name, function.Name.Name)
		}
		key := suite + "." + function.Name.Name
		if _, duplicate := references[key]; duplicate {
			return fmt.Errorf("conformance: ambiguous test %s", key)
		}
		references[key] = ConformanceTestReference{Suite: suite, Test: function.Name.Name, File: name, Line: positions.Position(function.Pos()).Line, SourceHash: sourceHash}
	}
	return nil
}

func testingImportName(file *ast.File) string {
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || path != "testing" {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return "testing"
	}
	return ""
}

func isExecutableTest(function *ast.FuncDecl, testingName string) bool {
	if testingName == "" || function.Body == nil || len(function.Body.List) == 0 || function.Type.Params == nil || len(function.Type.Params.List) != 1 || function.Type.Results != nil || function.Type.TypeParams != nil {
		return false
	}
	parameter := function.Type.Params.List[0]
	if len(parameter.Names) > 1 {
		return false
	}
	pointer, ok := parameter.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "T" {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == testingName
}
