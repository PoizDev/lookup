package semantics_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	golangadapter "github.com/poizdev/lookup/internal/language/golang"
)

var representativeFixturePaths = []string{
	"security/sql_injection/unsafe.go",
	"security/sql_injection/safe.go",
	"gin/routes.go",
	"fiber/routes.go",
	"gorm/queries.go",
	"resources/lifecycle.go",
	"concurrency/operations.go",
}

func BenchmarkGoParseAndSemanticExtract(b *testing.B) {
	adapter := &golangadapter.Adapter{}
	fixtures := loadRepresentativeFixtures(b)
	b.ReportAllocs()
	for iteration := 0; iteration < b.N; iteration++ {
		for path, source := range fixtures {
			ast, err := adapter.Parse(context.Background(), source)
			if err != nil {
				b.Fatal(err)
			}
			ast.FilePath = path
			if _, err := adapter.ExtractSemantic(ast); err != nil {
				b.Fatal(err)
			}
			_ = ast.Close()
		}
	}
}

func BenchmarkGoAnalyzerV2CachedSnapshot(b *testing.B) {
	adapter := &golangadapter.Adapter{}
	builder := graph.NewBuilder()
	for path, source := range loadRepresentativeFixtures(b) {
		ast, err := adapter.Parse(context.Background(), source)
		if err != nil {
			b.Fatal(err)
		}
		ast.FilePath = path
		document, err := adapter.ExtractSemantic(ast)
		if err != nil {
			b.Fatal(err)
		}
		builder.AddFile(ast.FilePath)
		builder.AddFunctions(adapter.ExtractFunctions(ast))
		builder.AddDocument(document)
		_ = ast.Close()
	}
	engine, err := analyzer.NewEngineChecked()
	if err != nil {
		b.Fatal(err)
	}
	if err := engine.AddLanguageRules(adapter); err != nil {
		b.Fatal(err)
	}
	g := builder.Build()
	if findings := engine.Run(g); len(findings) == 0 {
		b.Fatal("representative fixture produced no findings")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		_ = engine.Run(g)
	}
}

func loadRepresentativeFixtures(b *testing.B) map[string][]byte {
	b.Helper()
	fixtures := make(map[string][]byte, len(representativeFixturePaths))
	root := filepath.Join("..", "..", "..", "..", "testdata", "go")
	for _, relative := range representativeFixturePaths {
		path := filepath.Join(root, relative)
		source, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		fixtures[filepath.ToSlash(relative)] = source
	}
	return fixtures
}
