package rust

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

const representativeRust = `use std::path::Path as FsPath;
mod helpers;
struct Service { value: String }
enum State { Ready, Done }
trait Runner { fn run(&self, input: &str); }
impl Service {
    fn run(&self, input: &str) -> String {
        let alias = input.to_string();
        helper(alias)
    }
}
fn helper(value: String) -> String { value }
`

func parseRust(t *testing.T, src string) (*Adapter, *graph.RawAST) {
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
	ast.FilePath = "sample.rs"
	return a, ast
}

func TestCapabilitiesMatchImplementedStages(t *testing.T) {
	a := &Adapter{}
	want := language.Capabilities{Parse: true, Structural: true, Semantic: true}
	if got := a.Capabilities(); got != want {
		t.Fatalf("Capabilities() = %+v", got)
	}
	if _, ok := any(a).(language.SemanticAdapter); !ok {
		t.Fatal("missing SemanticAdapter")
	}
}
func TestParseErrorsCancellationAndCleanup(t *testing.T) {
	a := &Adapter{}
	bad, err := a.Parse(context.Background(), []byte("fn broken( {"))
	if err != nil {
		t.Fatal(err)
	}
	if !bad.HasErrors {
		t.Fatal("syntax error not recorded")
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ast, err := a.Parse(ctx, []byte("fn ok() {}"))
	if ast != nil {
		ast.Close()
		t.Fatal("cancelled parse returned AST")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
func TestStructuralAndSemanticExtraction(t *testing.T) {
	a, ast := parseRust(t, representativeRust)
	funcs := a.ExtractFunctions(ast)
	if len(funcs) != 3 || funcs[1].Name != "run" || funcs[1].Receiver != "Service" {
		t.Fatalf("functions = %#v", funcs)
	}
	types := a.ExtractTypes(ast)
	if len(types) != 3 {
		t.Fatalf("types = %#v", types)
	}
	imports := a.ExtractImports(ast)
	if len(imports) != 2 || imports[0].Path != "std::path::Path" || imports[0].Alias != "FsPath" || imports[1].Path != "helpers" {
		t.Fatalf("imports = %#v", imports)
	}
	calls := a.ExtractCalls(ast)
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
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
	for _, key := range []string{"propagation:binding", "call:helper", "return:return", "control:trait"} {
		if !seen[key] {
			t.Errorf("missing %s", key)
		}
	}
}

func TestUnsafeRuleFixtures(t *testing.T) {
	for _, tc := range []struct {
		file string
		want int
	}{{"positive.rs", 1}, {"negative.rs", 0}, {"near_miss.rs", 0}} {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../../testdata/rust/unsafe", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			a, ast := parseRust(t, string(src))
			doc, err := a.ExtractSemantic(ast)
			if err != nil {
				t.Fatal(err)
			}
			g := graph.NewGraph()
			g.AddDocument(doc)
			got := len(a.LanguageRules()[0].Check(g))
			if got != tc.want {
				t.Fatalf("findings=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestReceiverlessCallShadowedByLocalBindingDoesNotBecomeMethodProvenance(t *testing.T) {
	a, ast := parseRust(t, `
struct LocalState;
impl LocalState { fn mmap(&self) {} }
fn mmap() -> i32 { 0 }
fn load() {
    let mmap = || 42;
    let _ = mmap();
}
`)
	b := graph.NewBuilder()
	b.AddFile(ast.FilePath)
	b.AddFunctions(a.ExtractFunctions(ast))
	b.AddCalls(a.ExtractCalls(ast))
	for _, edge := range b.Build().Edges {
		if edge.Kind == graph.EdgeCalls && edge.To == graph.NodeID("sample.rs:LocalState.mmap") {
			t.Fatalf("local mmap invocation resolved to unrelated method: %#v", edge)
		}
	}
}

func TestTLSClassificationCountsDirectInvocationsOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{name: "one nested in chain", body: `builder.danger_accept_invalid_certs(true).danger_accept_invalid_hostnames(true);`, want: 1},
		{name: "two distinct", body: `builder.danger_accept_invalid_certs(true); other.danger_accept_invalid_certs(true);`, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, ast := parseRust(t, "use reqwest::Client; fn client() { "+tc.body+" }")
			doc, err := a.ExtractSemantic(ast)
			if err != nil {
				t.Fatal(err)
			}
			got := 0
			for _, fact := range doc.Facts {
				if fact.Operation == "rust.tls.invalid_certs" {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("invalid-cert facts = %d, want %d", got, tc.want)
			}
		})
	}
}
