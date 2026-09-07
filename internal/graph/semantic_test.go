package graph

import (
	"errors"
	"testing"

	"github.com/poizdev/lookup/internal/semantic"
)

func TestGraphStoresSemanticDocumentsInPathOrder(t *testing.T) {
	builder := NewBuilder()
	builder.AddDocument(&semantic.Document{Path: "z.go", Language: "Go"})
	builder.AddDocument(&semantic.Document{Path: "a.go", Language: "Go"})

	documents := builder.Build().Documents()
	if len(documents) != 2 || documents[0].Path != "a.go" || documents[1].Path != "z.go" {
		t.Fatalf("documents = %#v", documents)
	}
}

func TestRawASTCloseIsIdempotent(t *testing.T) {
	calls := 0
	wantErr := errors.New("close failed")
	ast := NewRawAST("Go", nil, nil, func() error {
		calls++
		return wantErr
	})
	if err := ast.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := ast.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("second Close() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", calls)
	}
}

func TestGraphCachesSemanticIndexAndDataflowTraces(t *testing.T) {
	g := NewGraph()
	g.AddDocument(&semantic.Document{Path: "handler.go", Facts: []semantic.Fact{
		{ID: "source", Kind: semantic.FactSource, Operation: "http.query", Function: "handler", Outputs: []string{"id"}},
		{ID: "sink", Kind: semantic.FactSink, Operation: "sql.raw", Function: "handler", Inputs: []string{"id"}},
	}})
	if first, second := g.SemanticIndex(), g.SemanticIndex(); first != second {
		t.Fatal("SemanticIndex rebuilt for an immutable graph")
	}
	first := g.DataflowTraces()
	second := g.DataflowTraces()
	if len(first) != 1 || len(second) != 1 || first[0].Source.ID != second[0].Source.ID {
		t.Fatalf("cached traces = %#v and %#v", first, second)
	}
}
