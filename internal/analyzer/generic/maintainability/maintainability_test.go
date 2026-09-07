package maintainability_test

import (
	"testing"

	"github.com/poizdev/lookup/internal/analyzer/generic/maintainability"
	"github.com/poizdev/lookup/internal/graph"
)

func TestFunctionLength(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:longFunction",
		Kind:      graph.NodeFunction,
		Name:      "longFunction",
		File:      "main.go",
		StartLine: 1,
		EndLine:   150,
	})

	rule := &maintainability.FunctionLength{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for long function, got 0")
	}
	if findings[0].RuleID != "MNT-001" {
		t.Errorf("expected RuleID MNT-001, got %s", findings[0].RuleID)
	}
}

func TestCyclomaticComplexity(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:complexFunc",
		Kind:      graph.NodeFunction,
		Name:      "complexFunc",
		File:      "main.go",
		StartLine: 1,
		EndLine:   30,
		Metadata: map[string]any{
			"Body": `func complexFunc(x int) {
	if x > 1 && x < 10 {
		if x == 2 || x == 3 {
			for i := 0; i < 5; i++ {
				if i == 2 {
					switch x {
					case 1:
					case 2:
					case 3:
					case 4:
					}
				}
			}
		}
	}
}`,
		},
	})

	rule := &maintainability.CyclomaticComplexity{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for cyclomatic complexity, got 0")
	}
	if findings[0].RuleID != "MNT-002" {
		t.Errorf("expected RuleID MNT-002, got %s", findings[0].RuleID)
	}
}
