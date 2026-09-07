package rules_test

import (
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language/golang/rules"
)

func TestSQLInjection_DetectsConcat(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:getUser",
		Kind:      graph.NodeFunction,
		Name:      "getUser",
		File:      "main.go",
		StartLine: 10,
		EndLine:   20,
		Metadata: map[string]any{
			"Body": `func getUser(id string) {
	query := "SELECT * FROM users WHERE id = " + id
	db.Query("SELECT * FROM users WHERE id = " + id)
}`,
		},
	})

	rule := &rules.SQLInjection{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for SQL injection concatenation, got 0")
	}

	if findings[0].RuleID != "SEC-002" {
		t.Errorf("expected RuleID SEC-002, got %s", findings[0].RuleID)
	}
}

func TestSQLInjection_IgnoresParameterized(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "func:main.go:getUserSafe",
		Kind:      graph.NodeFunction,
		Name:      "getUserSafe",
		File:      "main.go",
		StartLine: 10,
		EndLine:   20,
		Metadata: map[string]any{
			"Body": `func getUserSafe(id string) {
	db.Query("SELECT * FROM users WHERE id = ?", id)
}`,
		},
	})

	rule := &rules.SQLInjection{}
	findings := rule.Check(g)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for safe parameterized query, got %d", len(findings))
	}
}
