package css

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	csssyntax "github.com/tdewolff/parse/v2/css"
)

func cssTestVariables(t *testing.T, source string) cssVariables {
	t.Helper()
	block := ParseDeclarations(source)
	if err := block.Err(); err != nil {
		t.Fatal(err)
	}
	result := make(cssVariables)
	for _, declaration := range block.Declarations {
		result[declaration.Property] = declaration.tokens
	}
	return result
}

func TestCSSVariablesCyclesFallbacksCaseAndEmptyValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   map[string]string
	}{
		{"fallback cycle", `--a: var(--missing, var(--b)); --b: var(--a); --c: var(--a, green)`, map[string]string{"--c": "green"}},
		{"unused fallback cycle", `--base: red; --a: var(--base, var(--b)); --b: var(--a); --c: var(--a, blue)`, map[string]string{"--base": "red", "--c": "blue"}},
		{"empty is valid", `--empty:; --x: var(--empty, red); --y: var(--missing,);`, map[string]string{"--empty": "", "--x": "", "--y": ""}},
		{"case and escaped name", `--Mixed: red; --mixed: blue; --x: var(--M\69 xed); --y: var(--mixed)`, map[string]string{"--Mixed": "red", "--mixed": "blue", "--x": "red", "--y": "blue"}},
		{"nested fallback", `--x: var(--missing, rgb(var(--r, 1), 2, 3))`, map[string]string{"--x": "rgb(1, 2, 3)"}},
		{"quoted text", `--x: "var(--missing)"; --y: url("var(--missing)")`, map[string]string{"--x": `"var(--missing)"`, "--y": `url("var(--missing)")`}},
		{"self cycle", `--a: var(--a, red); --b: var(--a, blue)`, map[string]string{"--b": "blue"}},
		{"generated initial", `--a: var(--missing, initial); --b: var(--a, blue)`, map[string]string{"--b": "blue"}},
		{"comments", `--y: /* baz */; --x: /* foo */ var(--y) /* bar */`, map[string]string{"--y": "/* baz */", "--x": "/* foo */ /* baz */ /* bar */"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := resolveCSSVariables(nil, cssTestVariables(t, test.source))
			if err != nil || len(actual) != len(test.want) {
				t.Fatalf("variable computation failed: %v %v", actual, err)
			}
			for name, want := range test.want {
				value, exists := actual[name]
				if !exists || cssTokenText(value) != want {
					t.Fatalf("%s = %q, present %v; want %q", name, cssTokenText(value), exists, want)
				}
			}
		})
	}
}

func TestCSSVariablesInheritComputedParentScopeAndKeywords(t *testing.T) {
	parent, err := resolveCSSVariables(nil, cssTestVariables(t, `--base: red; --alias: var(--base); --empty:; --remove: yes; --explicit: yes`))
	if err != nil {
		t.Fatal(err)
	}
	child, err := resolveCSSVariables(parent, cssTestVariables(t, `--base: blue; --remove: initial; --explicit: inherit; --new: unset; --use: var(--alias)`))
	if err != nil || cssTokenText(child["--alias"]) != "red" || cssTokenText(child["--use"]) != "red" || cssTokenText(child["--base"]) != "blue" || cssTokenText(child["--explicit"]) != "yes" {
		t.Fatalf("parent variables rebound or keyword inheritance failed: %v %v", child, err)
	}
	if _, exists := child["--empty"]; !exists {
		t.Fatal("inherited empty value lost")
	}
	for _, name := range []string{"--remove", "--new"} {
		if _, exists := child[name]; exists {
			t.Fatalf("guaranteed-invalid %s became present", name)
		}
	}
	if cssTokenText(parent["--base"]) != "red" || cssTokenText(parent["--remove"]) != "yes" {
		t.Fatal("child mutated computed parent scope")
	}
}

func TestCSSVariableSubstitutionPreservesTokenIdentity(t *testing.T) {
	actual, err := resolveCSSVariables(nil, cssTestVariables(t, `--n: 10; --length: var(--n)px`))
	if err != nil {
		t.Fatal(err)
	}
	value := actual["--length"]
	if len(value) != 2 || value[0].kind != csssyntax.NumberToken || value[1].kind != csssyntax.IdentToken {
		t.Fatalf("substitution retokenized separate number/identifier as a dimension: %v", value)
	}
}

func TestCSSVariablesBoundDependenciesAndExponentialExpansion(t *testing.T) {
	var expansion, chain strings.Builder
	expansion.WriteString("--v0: 1234567890;")
	for index := 1; index < 20; index++ {
		fmt.Fprintf(&expansion, "--v%d: var(--v%d)var(--v%d);", index, index-1, index-1)
	}
	for index := 0; index < maxCSSVariableDepth+1; index++ {
		fmt.Fprintf(&chain, "--v%03d: var(--v%03d);", index, index+1)
	}
	for _, source := range []string{expansion.String(), chain.String()} {
		_, err := resolveCSSVariables(nil, cssTestVariables(t, source))
		var diagnostic *Diagnostic
		if !errors.As(err, &diagnostic) || diagnostic.Code != DiagnosticBudget {
			t.Fatalf("unbounded variables were admitted: %v", err)
		}
	}
	declared := make(cssVariables)
	for index := 0; index <= maxCSSVariables; index++ {
		declared[fmt.Sprintf("--v%d", index)] = nil
	}
	if _, err := resolveCSSVariables(nil, declared); err == nil {
		t.Fatal("variable count budget ignored")
	}
}

func TestCSSVariablesGeneratedRollbackReachesCascadeOwner(t *testing.T) {
	for _, keyword := range []string{"revert", "revert-layer"} {
		_, err := resolveCSSVariables(nil, cssTestVariables(t, "--a: var(--missing, "+keyword+"); --b: var(--a, red)"))
		var rollback *cssVariableRollback
		if !errors.As(err, &rollback) || rollback.name != "--a" || rollback.keyword != keyword {
			t.Fatalf("origin rollback became a literal or dependent fallback: %v", err)
		}
	}
}

func TestCSSVariablesExactCountAndDepthAndAggregateBudget(t *testing.T) {
	declared := make(cssVariables)
	for index := 0; index < maxCSSVariables; index++ {
		declared[fmt.Sprintf("--v%d", index)] = nil
	}
	parent, err := resolveCSSVariables(nil, declared)
	if err != nil || len(parent) != maxCSSVariables {
		t.Fatalf("exact variable count rejected: %v", err)
	}
	if child, err := resolveCSSVariables(parent, declared); err != nil || len(child) != maxCSSVariables {
		t.Fatalf("overrides counted twice against variable budget: %v", err)
	}
	var chain strings.Builder
	for index := 0; index < maxCSSVariableDepth-1; index++ {
		fmt.Fprintf(&chain, "--v%03d: var(--v%03d);", index, index+1)
	}
	fmt.Fprintf(&chain, "--v%03d: red", maxCSSVariableDepth-1)
	if actual, err := resolveCSSVariables(nil, cssTestVariables(t, chain.String())); err != nil || cssTokenText(actual["--v000"]) != "red" {
		t.Fatalf("exact dependency depth rejected: %v", err)
	}
	var source strings.Builder
	fmt.Fprintf(&source, "--base: %s;", strings.Repeat("a", 32768))
	for index := 0; index < 32; index++ {
		fmt.Fprintf(&source, "--v%d: var(--base);", index)
	}
	_, err = resolveCSSVariables(nil, cssTestVariables(t, source.String()))
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != DiagnosticBudget {
		t.Fatalf("aggregate expansion budget ignored: %v", err)
	}
}
