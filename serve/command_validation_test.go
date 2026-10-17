package serve

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestCommandValidationMatchesActualProtocolAndDispatch(t *testing.T) {
	contract, err := parser.ParseFile(token.NewFileSet(), "contract.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	commands := make(map[string]Command)
	ast.Inspect(contract, func(node ast.Node) bool {
		value, ok := node.(*ast.ValueSpec)
		if !ok || len(value.Names) != 1 || len(value.Values) != 1 || !strings.HasPrefix(value.Names[0].Name, "Cmd") {
			return true
		}
		literal, ok := value.Values[0].(*ast.BasicLit)
		if !ok {
			t.Fatal("command constant is not a literal")
		}
		raw, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		commands[value.Names[0].Name] = Command(raw)
		return true
	})
	server, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	dispatched := make(map[string]bool)
	for _, declaration := range server.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "dispatch" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, value := range clause.List {
				call, ok := value.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					t.Fatal("unexpected command dispatch case")
				}
				name, ok := call.Args[0].(*ast.Ident)
				if !ok {
					t.Fatal("dispatch does not use the canonical command constant")
				}
				if _, exists := commands[name.Name]; !exists {
					t.Fatalf("undeclared dispatched command %s", name.Name)
				}
				dispatched[name.Name] = true
			}
			return true
		})
	}
	if len(commands) == 0 || len(dispatched) != len(commands) {
		t.Fatalf("declared/dispatched commands differ: %v %v", commands, dispatched)
	}
	for name, command := range commands {
		if !dispatched[name] {
			t.Fatalf("declared command %s has no real dispatch", name)
		}
		if err := command.Validate(); err != nil {
			t.Fatalf("real command %s rejected: %v", name, err)
		}
	}
	for _, invalid := range []Command{"", "not-real", "session.new ", "SESSION.NEW"} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("unknown command %q admitted", invalid)
		}
	}
}
