package csharp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

const representativeCSharp = `using System;
using Microsoft.EntityFrameworkCore;
namespace Demo;
interface IRunner { void Run(string value); }
struct Result { public int Code { get; set; } }
class Service {
    public Service() {}
    public string Name { get; set; }
    public async void Run(string value) {
        var alias = value;
        Console.WriteLine(alias);
        await SaveAsync(alias);
    }
    private Task SaveAsync(string value) { return Task.CompletedTask; }
}`

func parseCSharp(t *testing.T, src string) (*Adapter, *graph.RawAST) {
	t.Helper()
	a := &Adapter{}
	ast, err := a.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ast.Close(); err != nil {
			t.Fatal(err)
		}
	})
	ast.FilePath = "Sample.cs"
	return a, ast
}

func TestRuleFixtures(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{{"async_void/positive.cs", 1}, {"async_void/negative.cs", 0}, {"async_void/near_miss.cs", 0}, {"ef_sql/positive.cs", 1}, {"ef_sql/negative.cs", 0}, {"ef_sql/near_miss.cs", 0}}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../../testdata/csharp", tc.path))
			if err != nil {
				t.Fatal(err)
			}
			a, ast := parseCSharp(t, string(src))
			doc, err := a.ExtractSemantic(ast)
			if err != nil {
				t.Fatal(err)
			}
			g := graph.NewGraph()
			g.AddDocument(doc)
			got := 0
			for _, rule := range a.LanguageRules() {
				got += len(rule.Check(g))
			}
			if got != tc.want {
				t.Fatalf("findings=%d, want %d facts=%#v", got, tc.want, doc.Facts)
			}
		})
	}
}
func TestCapabilitiesMatchImplementedStages(t *testing.T) {
	a := &Adapter{}
	want := language.Capabilities{Parse: true, Structural: true, Semantic: true}
	if got := a.Capabilities(); got != want {
		t.Fatalf("caps=%+v", got)
	}
	if _, ok := any(a).(language.SemanticAdapter); !ok {
		t.Fatal("missing semantic adapter")
	}
}
func TestParseErrorsCancellationAndCleanup(t *testing.T) {
	a := &Adapter{}
	bad, err := a.Parse(context.Background(), []byte("class Broken { void M( }"))
	if err != nil {
		t.Fatal(err)
	}
	if !bad.HasErrors {
		t.Fatal("syntax error not recorded")
	}
	bad.Close()
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ast, err := a.Parse(ctx, []byte("class Ok {}"))
	if ast != nil {
		ast.Close()
		t.Fatal("cancelled parse returned AST")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
func TestStructuralAndSemanticExtraction(t *testing.T) {
	a, ast := parseCSharp(t, representativeCSharp)
	funcs := a.ExtractFunctions(ast)
	if len(funcs) != 4 {
		t.Fatalf("functions=%#v", funcs)
	}
	types := a.ExtractTypes(ast)
	if len(types) != 3 {
		t.Fatalf("types=%#v", types)
	}
	imports := a.ExtractImports(ast)
	if len(imports) != 2 || imports[1].Path != "Microsoft.EntityFrameworkCore" {
		t.Fatalf("imports=%#v", imports)
	}
	calls := a.ExtractCalls(ast)
	if len(calls) != 2 {
		t.Fatalf("calls=%#v", calls)
	}
	first, err := a.ExtractSemantic(ast)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.ExtractSemantic(ast)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("non-deterministic semantics")
	}
	seen := map[string]bool{}
	for _, f := range first.Facts {
		seen[string(f.Kind)+":"+f.Operation] = true
	}
	for _, key := range []string{"guard:csharp.async_void", "propagation:assignment", "call:SaveAsync", "return:return"} {
		if !seen[key] {
			t.Errorf("missing %s", key)
		}
	}
}
