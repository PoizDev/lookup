package csharp

import (
	"github.com/poizdev/lookup/internal/semantic"
	"testing"
)

// Modern .NET entry points and field/property initializers occur outside methods.
func TestAssignmentsOutsideMethodsExcludedFromSemanticFacts(t *testing.T) {
	for name, src := range map[string]string{
		"top_level": `using Microsoft.AspNetCore.Builder; var builder = WebApplication.CreateBuilder(args); var app = builder.Build(); app.Run();`,
		"field":     `class Service { private string name = "demo"; public string Name { get; set; } = "sample"; }`,
	} {
		t.Run(name, func(t *testing.T) {
			a, ast := parseCSharp(t, src)
			if ast.HasErrors {
				t.Fatal("invalid fixture")
			}
			doc, err := a.ExtractSemantic(ast)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, fact := range doc.Facts {
				if fact.Kind == semantic.FactPropagation && fact.Operation == "assignment" {
					count++
				}
			}
			if count == 0 {
				t.Fatal("outside-method assignment facts lost")
			}
		})
	}
}

func TestAsyncVoidUsesMethodSignature(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want int
	}{
		"lambda":      {`class C { void Run() { Dispatch(async () => { await Work(); }); } }`, 0},
		"nested_void": {`class C { async Task Run() { void Helper() {} await Work(); } }`, 0},
		"comment":     {`class C { void Run() { /* async */ } }`, 0},
		"async_void":  {`class C { async void Run() { await Work(); } }`, 1},
	} {
		t.Run(name, func(t *testing.T) {
			a, ast := parseCSharp(t, tc.src)
			if ast.HasErrors {
				t.Fatal("invalid fixture")
			}
			doc, err := a.ExtractSemantic(ast)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, f := range doc.Facts {
				if f.Operation == "csharp.async_void" {
					count++
				}
			}
			if count != tc.want {
				t.Fatalf("async-void facts=%d, want %d; tree=%s", count, tc.want, ast.Root)
			}
		})
	}
}
