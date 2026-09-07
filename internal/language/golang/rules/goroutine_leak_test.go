package rules_test

import (
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language/golang/rules"
)

func TestGoroutineLeak_TimeAfterInLoop(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:pollEvents",
		Kind:      graph.NodeFunction,
		Name:      "pollEvents",
		File:      "main.go",
		StartLine: 1,
		EndLine:   10,
		Metadata: map[string]any{
			"Body": `func pollEvents() {
	for {
		select {
		case <-time.After(time.Second):
			println("tick")
		}
	}
}`,
		},
	})

	rule := &rules.GoroutineLeak{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for time.After inside loop, got 0")
	}
	if findings[0].RuleID != "PER-002" {
		t.Errorf("expected RuleID PER-002, got %s", findings[0].RuleID)
	}
}

func TestGoroutineLeak_MissingContextCancel(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:fetchData",
		Kind:      graph.NodeFunction,
		Name:      "fetchData",
		File:      "main.go",
		StartLine: 1,
		EndLine:   10,
		Metadata: map[string]any{
			"Body": `func fetchData(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.example.com", nil)
	_ = req
}`,
		},
	})

	rule := &rules.GoroutineLeak{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for missing context cancel, got 0")
	}
	if findings[0].RuleID != "PER-002" {
		t.Errorf("expected RuleID PER-002, got %s", findings[0].RuleID)
	}
}
