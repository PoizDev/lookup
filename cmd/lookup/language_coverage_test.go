package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/poizdev/lookup/internal/discovery"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/language/golang"
	"github.com/poizdev/lookup/internal/semantic"
)

type lifecycleAdapter struct {
	mode       string
	closeCalls int
}

func (*lifecycleAdapter) Language() language.Language { return language.Go }
func (*lifecycleAdapter) Capabilities() language.Capabilities {
	return language.Capabilities{Parse: true, Structural: true, Semantic: true}
}
func (a *lifecycleAdapter) Parse(ctx context.Context, src []byte) (*graph.RawAST, error) {
	ast := graph.NewRawAST("Go", nil, src, func() error { a.closeCalls++; return nil })
	if a.mode == "syntax" {
		ast.HasErrors = true
	}
	if a.mode == "cancel" {
		return ast, ctx.Err()
	}
	return ast, nil
}
func (*lifecycleAdapter) ExtractSymbols(*graph.RawAST) []graph.Symbol     { return nil }
func (*lifecycleAdapter) ExtractFunctions(*graph.RawAST) []graph.Function { return nil }
func (*lifecycleAdapter) ExtractTypes(*graph.RawAST) []graph.TypeDef      { return nil }
func (*lifecycleAdapter) ExtractImports(*graph.RawAST) []graph.Import     { return nil }
func (*lifecycleAdapter) ExtractCalls(*graph.RawAST) []graph.Call         { return nil }
func (a *lifecycleAdapter) ExtractSemantic(ast *graph.RawAST) (*semantic.Document, error) {
	if a.mode == "semantic" {
		return nil, errors.New("semantic failed")
	}
	return &semantic.Document{Path: ast.FilePath}, nil
}
func (*lifecycleAdapter) LanguageRules() []language.AnalysisRule { return nil }

func TestLanguagePipelineClosesParseResultBeforeAdvancingOnEveryPath(t *testing.T) {
	for _, mode := range []string{"success", "syntax", "semantic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := writeCoverageFixture(t, dir, "input.go", "package sample")
			adapter := &lifecycleAdapter{mode: mode}
			registry := language.NewRegistry()
			if err := registry.Register(adapter); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			increments := 0
			processLanguageFiles(ctx, []discovery.DiscoveredFile{{Path: path, Language: "Go"}}, registry, graph.NewBuilder(), nil, func() {
				increments++
				if adapter.closeCalls != 1 {
					t.Fatalf("close calls at increment = %d, want 1", adapter.closeCalls)
				}
			})
			if increments != 1 || adapter.closeCalls != 1 {
				t.Fatalf("increments/close calls = %d/%d", increments, adapter.closeCalls)
			}
		})
	}
}

func TestLanguageCoveragePipelineUsesRealExecutionOutcomes(t *testing.T) {
	dir := t.TempDir()
	validGo := writeCoverageFixture(t, dir, "valid.go", "package sample\nfunc Valid() {}\n")
	invalidGo := writeCoverageFixture(t, dir, "invalid.go", "package sample\nfunc Broken( {\n")
	python := writeCoverageFixture(t, dir, "tool.py", "print('ok')\n")
	yaml := writeCoverageFixture(t, dir, "ci.yml", "name: ci\n")

	registry := language.NewRegistry()
	if err := registry.Register(&golang.Adapter{}); err != nil {
		t.Fatal(err)
	}
	files := []discovery.DiscoveredFile{
		{Path: validGo, Language: "Go"},
		{Path: invalidGo, Language: "Go"},
		{Path: python, Language: "Python"},
		{Path: yaml, Language: "YAML"},
	}

	builder := graph.NewBuilder()
	result := processLanguageFiles(context.Background(), files, registry, builder, nil, nil)
	if result.ParsedCount != 1 {
		t.Fatalf("ParsedCount = %d, want 1", result.ParsedCount)
	}
	if err := result.CompleteSemantic(); err != nil {
		t.Fatal(err)
	}
	documents := builder.Build().Documents()
	if len(documents) != 1 || documents[0].Path != validGo {
		t.Fatalf("semantic documents = %#v, want parsed Go file", documents)
	}
	coverage := result.Collector.Coverage()
	if len(coverage) != 3 {
		t.Fatalf("coverage rows = %#v", coverage)
	}
	goCoverage := coverage[0]
	if goCoverage.Language != "Go" || goCoverage.Files != 2 || goCoverage.Parse.Attempted != 2 || goCoverage.Parse.Succeeded != 1 || goCoverage.Structural.Attempted != 1 || goCoverage.Structural.Succeeded != 1 || goCoverage.Semantic.Attempted != 1 || goCoverage.Semantic.Succeeded != 1 {
		t.Fatalf("Go coverage = %#v", goCoverage)
	}
	for _, unsupported := range coverage[1:] {
		if unsupported.Files != 1 || unsupported.Parse.Supported != 0 || unsupported.Parse.Attempted != 0 || unsupported.Parse.Succeeded != 0 {
			t.Fatalf("unsupported coverage counted as failure: %#v", unsupported)
		}
	}
}

func TestDefaultLanguageRegistryIsDeterministicAndComplete(t *testing.T) {
	registry, err := newLanguageRegistry()
	if err != nil {
		t.Fatal(err)
	}
	want := []language.Language{language.CSharp, language.Go, language.Python, language.Rust}
	if got := registry.Languages(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
	for _, adapter := range registry.Adapters() {
		if got := adapter.Capabilities(); got != (language.Capabilities{Parse: true, Structural: true, Semantic: true}) {
			t.Fatalf("%s capabilities = %+v", adapter.Language(), got)
		}
	}
}

func writeCoverageFixture(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
