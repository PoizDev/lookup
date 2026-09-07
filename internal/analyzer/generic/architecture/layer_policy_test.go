package architecture

import (
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
)

func TestExplicitLayerPolicyOriginRemainsDistinct(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{ID: "explicit", Kind: graph.NodeImport, Name: "example.dev/persistence", File: "/src/ui/action.go", StartLine: 3, EndLine: 3, Metadata: map[string]any{"Path": "example.dev/persistence"}})
	rule := &LayerViolation{policy: &layerPolicy{PresentationKeywords: []string{"/ui/"}, DataKeywords: []string{"/persistence"}, Origin: policyOriginExplicit, Basis: "repository architecture manifest"}}
	findings := rule.Check(g)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	for _, step := range findings[0].EvidenceSteps {
		if step.Kind == "architecture_policy_missing" {
			t.Fatalf("explicit policy marked missing: %#v", step)
		}
		if step.Kind == "architecture_basis" && (!strings.Contains(step.Expression, "origin=explicit_repository_policy") || !strings.Contains(step.Expression, "repository architecture manifest")) {
			t.Fatalf("explicit basis = %#v", step)
		}
	}
}

func TestUnknownLayerPolicyOriginFailsClosed(t *testing.T) {
	policy := layerPolicy{Origin: policyOrigin("unknown"), Basis: "untrusted"}
	if policy.establishesBoundary() {
		t.Fatal("unknown policy origin established an architecture boundary")
	}
}
