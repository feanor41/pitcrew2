package roadmap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestGitHubPreparationHasNoNetworkDependency(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "github.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		imports, ok := declaration.(*ast.GenDecl)
		if !ok || imports.Tok != token.IMPORT {
			continue
		}
		for _, specification := range imports.Specs {
			path, err := strconv.Unquote(specification.(*ast.ImportSpec).Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "net" || path == "net/http" {
				t.Fatalf("pure preparation imports network package %q", path)
			}
		}
	}
}
