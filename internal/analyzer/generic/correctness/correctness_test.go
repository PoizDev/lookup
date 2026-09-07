package correctness_test

import (
	"testing"

	"github.com/poizdev/lookup/internal/analyzer/generic/correctness"
	"github.com/poizdev/lookup/internal/graph"
)

func TestUnhandledError(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:doSomething",
		Kind:      graph.NodeFunction,
		Name:      "doSomething",
		File:      "main.go",
		StartLine: 1,
		EndLine:   10,
		Metadata: map[string]any{
			"Body": `func doSomething() {
	_ = os.Remove("file.txt")
}`,
		},
	})

	rule := &correctness.UnhandledError{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for unhandled error, got 0")
	}
	if findings[0].RuleID != "COR-001" {
		t.Errorf("expected RuleID COR-001, got %s", findings[0].RuleID)
	}
}

func TestNilDereference(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:processNode",
		Kind:      graph.NodeFunction,
		Name:      "processNode",
		File:      "main.go",
		StartLine: 1,
		EndLine:   10,
		Metadata: map[string]any{
			"Body": `func processNode() {
	node := findChildNode()
	name := node.Name
	_ = name
}`,
		},
	})

	rule := &correctness.NilDereference{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for nil dereference risk, got 0")
	}
	if findings[0].RuleID != "COR-002" {
		t.Errorf("expected RuleID COR-002, got %s", findings[0].RuleID)
	}
}
