package graph

import "testing"

func TestBuilderAddsCallEdgeOnlyForUniqueExactTarget(t *testing.T) {
	b := NewBuilder()
	b.AddFile("caller.go")
	b.AddFunctions([]Function{{Name: "caller", File: "caller.go"}, {Name: "target", File: "caller.go"}})
	b.AddCalls([]Call{{CallerName: "caller", CalleeName: "target", File: "caller.go"}})
	if got := countCallEdges(b.Build()); got != 1 {
		t.Fatalf("call edges = %d, want 1", got)
	}
}

func TestBuilderDoesNotResolveReceiverlessCallAcrossFiles(t *testing.T) {
	b := NewBuilder()
	b.AddFile("caller.rs")
	b.AddFile("target.rs")
	b.AddFunctions([]Function{
		{Name: "load", File: "caller.rs"},
		{Name: "mmap", Receiver: "LocalState", File: "target.rs"},
	})
	b.AddCalls([]Call{{CallerName: "load", CalleeName: "mmap", File: "caller.rs"}})
	if got := countCallEdges(b.Build()); got != 0 {
		t.Fatalf("receiver-less cross-file call edges = %d, want 0", got)
	}
}

func TestBuilderDoesNotAddAmbiguousOrUnresolvedCallEdges(t *testing.T) {
	for _, test := range []struct {
		name      string
		functions []Function
		callee    string
	}{
		{name: "unresolved", functions: []Function{{Name: "caller", File: "caller.go"}}, callee: "missing"},
		{name: "ambiguous", functions: []Function{{Name: "caller", File: "caller.go"}, {Name: "target", File: "a.go"}, {Name: "target", File: "b.go"}}, callee: "target"},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := NewBuilder()
			for _, fn := range test.functions {
				b.AddFile(fn.File)
			}
			b.AddFunctions(test.functions)
			b.AddCalls([]Call{{CallerName: "caller", CalleeName: test.callee, File: "caller.go"}})
			if got := countCallEdges(b.Build()); got != 0 {
				t.Fatalf("call edges = %d, want 0", got)
			}
		})
	}
}

func TestBuilderUsesReceiverInMethodIdentityAndResolution(t *testing.T) {
	b := NewBuilder()
	b.AddFile("methods.go")
	b.AddFunctions([]Function{
		{Name: "caller", File: "methods.go"},
		{Name: "Run", Receiver: "(a *Alpha)", File: "methods.go"},
		{Name: "Run", Receiver: "(b *Beta)", File: "methods.go"},
	})
	b.AddCalls([]Call{{CallerName: "caller", CalleeName: "Run", CalleeReceiver: "Alpha", File: "methods.go"}})
	g := b.Build()
	if got := countCallEdges(g); got != 1 {
		t.Fatalf("call edges = %d, want 1", got)
	}
	if g.Nodes[NodeID("methods.go:Alpha.Run")] == nil || g.Nodes[NodeID("methods.go:Beta.Run")] == nil {
		t.Fatalf("receiver-qualified method nodes missing: %#v", g.Nodes)
	}
}

func TestBuilderDoesNotGuessMethodReceiver(t *testing.T) {
	b := NewBuilder()
	b.AddFile("methods.go")
	b.AddFunctions([]Function{{Name: "caller", File: "methods.go"}, {Name: "Run", Receiver: "(a *Alpha)", File: "methods.go"}, {Name: "Run", Receiver: "(b *Beta)", File: "methods.go"}})
	b.AddCalls([]Call{{CallerName: "caller", CalleeName: "Run", File: "methods.go", IsMethod: true}})
	if got := countCallEdges(b.Build()); got != 0 {
		t.Fatalf("call edges = %d, want 0", got)
	}
}

func TestBuilderResolvesCallsAfterAllFunctionsAreIndexed(t *testing.T) {
	b := NewBuilder()
	b.AddFile("caller.go")
	b.AddFunctions([]Function{{Name: "caller", File: "caller.go"}})
	b.AddCalls([]Call{{CallerName: "caller", CalleeName: "later", File: "caller.go", Line: 3}})
	b.AddFunctions([]Function{{Name: "later", File: "caller.go"}})
	if got := countCallEdges(b.Build()); got != 1 {
		t.Fatalf("forward call edges = %d, want 1", got)
	}
}

func TestBuilderCallResolutionIsStableAcrossCallInsertionOrder(t *testing.T) {
	build := func(calls []Call) []string {
		b := NewBuilder()
		b.AddFile("caller.go")
		b.AddFunctions([]Function{{Name: "caller", File: "caller.go"}, {Name: "alpha", File: "caller.go"}, {Name: "beta", File: "caller.go"}})
		b.AddCalls(calls)
		var edges []string
		for _, edge := range b.Build().Edges {
			if edge.Kind == EdgeCalls {
				edges = append(edges, string(edge.From)+"->"+string(edge.To))
			}
		}
		return edges
	}
	left := build([]Call{{CallerName: "caller", CalleeName: "beta", File: "caller.go", Line: 4}, {CallerName: "caller", CalleeName: "alpha", File: "caller.go", Line: 3}})
	right := build([]Call{{CallerName: "caller", CalleeName: "alpha", File: "caller.go", Line: 3}, {CallerName: "caller", CalleeName: "beta", File: "caller.go", Line: 4}})
	if len(left) != len(right) || left[0] != right[0] || left[1] != right[1] {
		t.Fatalf("call edge order differs: %v vs %v", left, right)
	}
}

func countCallEdges(g *Graph) int {
	count := 0
	for _, edge := range g.Edges {
		if edge.Kind == EdgeCalls {
			count++
		}
	}
	return count
}
