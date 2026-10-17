package docs

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// exportDecl is one exported top-level function or type of a product
// package; Key is "<package dir>:<Name>" because package names repeat.
type exportDecl struct {
	Key     string
	Package string
	Name    string
}

// skippedDirs are not product API: tools, generated vendor code and test
// data. Packages named main are skipped as well.
var skippedDirs = map[string]bool{"internal": true, "third_party": true, "cmd": true, "testdata": true}

func exportedDecls(root string) ([]exportDecl, error) {
	var decls []exportDecl
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (skippedDirs[entry.Name()] || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if file.Name.Name == "main" {
			return nil
		}
		dir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		add := func(name *ast.Ident) {
			if name.IsExported() {
				decls = append(decls, exportDecl{Key: filepath.ToSlash(dir) + ":" + name.Name, Package: file.Name.Name, Name: name.Name})
			}
		}
		for _, decl := range file.Decls {
			switch typed := decl.(type) {
			case *ast.FuncDecl:
				if typed.Recv == nil {
					add(typed.Name)
				}
			case *ast.GenDecl:
				if typed.Tok == token.TYPE {
					for _, spec := range typed.Specs {
						add(spec.(*ast.TypeSpec).Name)
					}
				}
			}
		}
		return nil
	})
	sort.Slice(decls, func(i, j int) bool { return decls[i].Key < decls[j].Key })
	return decls, err
}

// documented reports whether doc references pkg.Name as a whole identifier.
func documented(doc, pkg, name string) bool {
	needle := pkg + "." + name
	for offset := 0; ; {
		index := strings.Index(doc[offset:], needle)
		if index < 0 {
			return false
		}
		start, end := offset+index, offset+index+len(needle)
		before := start == 0 || !isIdentByte(doc[start-1])
		after := end == len(doc) || !isIdentByte(doc[end])
		if before && after {
			return true
		}
		offset = end
	}
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func coverageProblems(root, doc string, baseline map[string]bool) ([]string, error) {
	decls, err := exportedDecls(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	live := make(map[string]bool, len(decls))
	for _, decl := range decls {
		if live[decl.Key] {
			continue
		}
		live[decl.Key] = true
		covered := documented(doc, decl.Package, decl.Name)
		switch {
		case covered && baseline[decl.Key]:
			problems = append(problems, "documented export still in baseline: "+decl.Key)
		case !covered && !baseline[decl.Key]:
			problems = append(problems, fmt.Sprintf("undocumented export: %s (document %s.%s)", decl.Key, decl.Package, decl.Name))
		}
	}
	for key := range baseline {
		if !live[key] {
			problems = append(problems, "baseline entry has no live declaration: "+key)
		}
	}
	sort.Strings(problems)
	return problems, nil
}

func readBaseline(path string) (map[string]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	baseline := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if baseline[line] {
			return nil, errors.New("duplicate baseline entry: " + line)
		}
		baseline[line] = true
	}
	return baseline, scanner.Err()
}

// headings returns the "## " section headings outside code fences.
func headings(doc string) map[string]bool {
	result := map[string]bool{}
	fenced := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			result[strings.TrimRight(line, " \t\r")] = true
		}
	}
	return result
}
