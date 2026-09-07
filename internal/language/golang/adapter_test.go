package golang

import (
	"context"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
)

func TestMethodCallerCarriesReceiverIdentityIntoGraphResolution(t *testing.T) {
	adapter := &Adapter{}
	ast, err := adapter.Parse(context.Background(), []byte(`package sample
type Alpha struct{}
func helper() {}
func (a *Alpha) Run() { helper() }
`))
	if err != nil {
		t.Fatal(err)
	}
	defer ast.Close()
	ast.FilePath = "sample.go"
	b := graph.NewBuilder()
	b.AddFile(ast.FilePath)
	b.AddFunctions(adapter.ExtractFunctions(ast))
	b.AddCalls(adapter.ExtractCalls(ast))
	callEdges := 0
	for _, edge := range b.Build().Edges {
		if edge.Kind == graph.EdgeCalls {
			callEdges++
		}
	}
	if callEdges != 1 {
		t.Fatalf("method caller edges = %d, want 1", callEdges)
	}
}
