package security_test

import (
	"path/filepath"
	"testing"

	"github.com/poizdev/lookup/internal/analyzer/generic/security"
	"github.com/poizdev/lookup/internal/graph"
)

func TestHardcodedSecret(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "var:config.go:apiKey",
		Kind:      graph.NodeVariable,
		Name:      "apiKey",
		File:      "config.go",
		StartLine: 10,
		EndLine:   10,
		Metadata: map[string]any{
			"Value": "sk-1234567890abcdef1234567890abcdef",
		},
	})

	rule := &security.HardcodedSecret{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for hardcoded secret, got 0")
	}
	if findings[0].RuleID != "GO-SEC-001" {
		t.Errorf("expected RuleID GO-SEC-001, got %s", findings[0].RuleID)
	}
}

func TestHardcodedSecretUsesRepositoryRelativePathForTestClassification(t *testing.T) {
	relative := filepath.FromSlash("src/prefect/_internal/analytics/client.py")
	parents := []string{filepath.Join(string(filepath.Separator), "checkout", "Tests", "prefect"), filepath.Join(string(filepath.Separator), "checkout", "projects", "prefect")}
	for _, root := range parents {
		rule := &security.HardcodedSecret{RepositoryRoot: root}
		g := graph.NewGraph()
		g.AddNode(&graph.Node{ID: "secret", Kind: graph.NodeConstant, Name: "AMPLITUDE_API_KEY", File: filepath.Join(root, relative), Metadata: map[string]any{"Value": `"abcdefghijklmnop"`}})
		findings := rule.Check(g)
		if len(findings) != 1 {
			t.Fatalf("root %q findings = %d, want 1", root, len(findings))
		}
		if findings[0].Title == "Hardcoded secret in test file" || findings[0].Severity == "Low" {
			t.Fatalf("root %q contaminated classification: title=%q severity=%q", root, findings[0].Title, findings[0].Severity)
		}
	}
}

func TestHardcodedSecretRequiresCredentialLikeLiteral(t *testing.T) {
	g := graph.NewGraph()
	for i, item := range []struct{ name, value string }{
		{"defaultJwtSecretLen", "32"}, {"tokenTemplate", `"%{token}"`}, {"secretKind", `"enum"`},
	} {
		g.AddNode(&graph.Node{ID: graph.NodeID(string(rune('a' + i))), Kind: graph.NodeConstant, Name: item.name, File: "config.go", Metadata: map[string]any{"Value": item.value}})
	}
	if got := (&security.HardcodedSecret{}).Check(g); len(got) != 0 {
		t.Fatalf("non-credential findings = %#v", got)
	}
}
