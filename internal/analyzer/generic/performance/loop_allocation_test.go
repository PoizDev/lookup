package performance_test

import (
	"testing"

	"github.com/poizdev/lookup/internal/analyzer/generic/performance"
	"github.com/poizdev/lookup/internal/graph"
)

func TestLoopAllocation(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:processItems",
		Kind:      graph.NodeFunction,
		Name:      "processItems",
		File:      "main.go",
		StartLine: 1,
		EndLine:   10,
		Metadata: map[string]any{
			"Body": `func processItems(items []string) {
	for _, item := range items {
		re := regexp.MustCompile(item)
		_ = re
	}
}`,
		},
	})

	rule := &performance.LoopAllocation{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for loop allocation, got 0")
	}
	if findings[0].RuleID != "PER-001" {
		t.Errorf("expected RuleID PER-001, got %s", findings[0].RuleID)
	}
}
