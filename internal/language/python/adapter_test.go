package python

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

const representativePython = `import os
from pathlib import Path as P

module_value = "prefix"

class Service:
    async def run(self, value: str = "ok"):
        alias = value
        print(f"value={alias}")
        return alias

def helper(items=[]):
    return len(items)
`

func parsePython(t *testing.T, src string) (*Adapter, *graph.RawAST) {
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
	ast.FilePath = "sample.py"
	return a, ast
}

func TestCapabilitiesMatchImplementedStages(t *testing.T) {
	a := &Adapter{}
	want := language.Capabilities{Parse: true, Structural: true, Semantic: true}
	if got := a.Capabilities(); got != want {
		t.Fatalf("Capabilities() = %+v, want %+v", got, want)
	}
	if _, ok := any(a).(language.SemanticAdapter); !ok {
		t.Fatal("semantic capability without SemanticAdapter")
	}
}

func TestParseRejectsSyntaxErrorsAndHonorsCancellation(t *testing.T) {
	a := &Adapter{}
	bad, err := a.Parse(context.Background(), []byte("def broken(:\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !bad.HasErrors {
		t.Fatal("syntax error was not recorded")
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bad.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ast, err := a.Parse(ctx, []byte("x = 1\n"))
	if ast != nil {
		ast.Close()
		t.Fatal("cancelled parse returned AST")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse() error = %v, want context.Canceled", err)
	}
}

func TestStructuralExtraction(t *testing.T) {
	a, ast := parsePython(t, representativePython)
	funcs := a.ExtractFunctions(ast)
	if len(funcs) != 2 {
		t.Fatalf("functions = %#v", funcs)
	}
	if funcs[0].Name != "run" || funcs[0].Receiver != "Service" || funcs[1].Name != "helper" {
		t.Fatalf("functions = %#v", funcs)
	}
	types := a.ExtractTypes(ast)
	if len(types) != 1 || types[0].Name != "Service" || types[0].Kind != "class" {
		t.Fatalf("types = %#v", types)
	}
	imports := a.ExtractImports(ast)
	wantImports := []graph.Import{{Path: "os", File: "sample.py", Line: 1}, {Path: "pathlib.Path", Alias: "P", File: "sample.py", Line: 2}}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Fatalf("imports = %#v, want %#v", imports, wantImports)
	}
	calls := a.ExtractCalls(ast)
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	symbols := a.ExtractSymbols(ast)
	var foundModule bool
	for _, symbol := range symbols {
		if symbol.Name == "module_value" && symbol.Kind == graph.NodeVariable {
			foundModule = true
		}
	}
	if !foundModule {
		t.Fatalf("module declaration missing from %#v", symbols)
	}
}

func TestSemanticExtractionIsDeterministic(t *testing.T) {
	a, ast := parsePython(t, representativePython)
	first, err := a.ExtractSemantic(ast)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.ExtractSemantic(ast)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("semantic extraction is not deterministic")
	}
	if len(first.Functions) != 2 || len(first.Imports) != 2 {
		t.Fatalf("document = %#v", first)
	}
	seen := map[string]bool{}
	for _, fact := range first.Facts {
		seen[string(fact.Kind)+":"+fact.Operation] = true
	}
	for _, key := range []string{"call:print", "return:return", "propagation:assignment", "control:class", "propagation:string.interpolation"} {
		if !seen[key] {
			t.Errorf("missing fact %q", key)
		}
	}
}

func TestRuleFixtures(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{{"dynamic_execution/positive.py", 1}, {"dynamic_execution/negative.py", 0}, {"dynamic_execution/near_miss.py", 0}, {"mutable_default/positive.py", 1}, {"mutable_default/negative.py", 0}, {"mutable_default/near_miss.py", 0}}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../../testdata/python", tc.path))
			if err != nil {
				t.Fatal(err)
			}
			a, ast := parsePython(t, string(src))
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
				t.Fatalf("findings=%d, want %d", got, tc.want)
			}
		})
	}
}
