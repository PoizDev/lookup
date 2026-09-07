package rules_test

import (
	"context"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/csharp"
	"github.com/poizdev/lookup/internal/language/golang"
	"github.com/poizdev/lookup/internal/language/golang/rules"
	"github.com/poizdev/lookup/internal/language/python"
	"github.com/poizdev/lookup/internal/language/rust"
)

func TestGoComplexityExcludesOtherLanguageControlFacts(t *testing.T) {
	// Eleven data classes reproduce the template's module-level class facts;
	// unsafe blocks and C# control nodes are not Go decision nodes either.
	cases := []struct {
		a         language.LanguageAdapter
		path, src string
	}{
		{&python.Adapter{}, "models.py", strings.Repeat("class Model: pass\n", 11)},
		{&rust.Adapter{}, "lib.rs", "fn run() {" + strings.Repeat("unsafe {}\n", 11) + "}"},
		{&csharp.Adapter{}, "Service.cs", "class C { void Run() {" + strings.Repeat("if (true) {}\n", 11) + "} }"},
		{&golang.Adapter{}, "main.go", "package demo\nfunc run() {" + strings.Repeat("if true {}\n", 11) + "}"},
	}
	g := graph.NewGraph()
	for _, tc := range cases {
		ast, err := tc.a.Parse(context.Background(), []byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		ast.FilePath = tc.path
		if ast.HasErrors {
			t.Fatalf("invalid %s fixture", tc.path)
		}
		doc, err := tc.a.(language.SemanticAdapter).ExtractSemantic(ast)
		if err != nil {
			t.Fatal(err)
		}
		g.AddDocument(doc)
		if err := ast.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, rule := range rules.All() {
		if rule.ID() != "GO-MNT-002" {
			continue
		}
		got := rule.Check(g)
		if len(got) != 1 || got[0].File != "main.go" {
			t.Fatalf("Go complexity must only measure Go facts: %#v", got)
		}
	}
}
