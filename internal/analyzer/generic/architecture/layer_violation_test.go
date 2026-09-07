package architecture_test

import (
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/analyzer/generic/architecture"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

func TestLayerViolation(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{
		ID:        "import:internal/handler/user.go:github.com/poizdev/lookup/internal/db",
		Kind:      graph.NodeImport,
		Name:      "github.com/poizdev/lookup/internal/db",
		File:      "/workspace/project/internal/handler/user.go",
		StartLine: 5,
		EndLine:   5,
		Metadata: map[string]any{
			"Path": "github.com/poizdev/lookup/internal/db",
		},
	})

	rule := &architecture.LayerViolation{}
	findings := rule.Check(g)

	if len(findings) == 0 {
		t.Fatalf("expected finding for layer violation, got 0")
	}
	if findings[0].RuleID != "ARC-001" {
		t.Errorf("expected RuleID ARC-001, got %s", findings[0].RuleID)
	}
}

func TestLayerViolationDoesNotInferPersistenceFromAmbiguousRepositoryName(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{ID: "domain", Kind: graph.NodeImport, Name: "code.example/services/repository", File: "/src/routers/api/repo.go", Metadata: map[string]any{"Path": "code.example/services/repository"}})
	if got := (&architecture.LayerViolation{}).Check(g); len(got) != 0 {
		t.Fatalf("ambiguous repository findings = %#v", got)
	}
}

func TestLayerViolationExposesBuiltInHeuristicBasisWithoutClaimingRepositoryPolicy(t *testing.T) {
	g := architectureGraph("/src/routers/api/actions/artifacts.go", "gitea.dev/models/db")
	findings := (&architecture.LayerViolation{}).Check(g)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	basis := stepByKind(findings[0].EvidenceSteps, "architecture_basis")
	missing := stepByKind(findings[0].EvidenceSteps, "architecture_policy_missing")
	if basis == nil || !strings.Contains(basis.Expression, "source_keyword=/api/") || !strings.Contains(basis.Expression, "import_keyword=/db") || !strings.Contains(basis.Expression, "origin=built_in_heuristic") {
		t.Fatalf("architecture basis = %#v", basis)
	}
	if missing == nil || !strings.Contains(strings.ToLower(missing.Message), "repository architecture policy") {
		t.Fatalf("missing policy premise = %#v", missing)
	}
}

func architectureGraph(file, importPath string) *graph.Graph {
	g := graph.NewGraph()
	g.AddNode(&graph.Node{ID: graph.NodeID("import:" + file + ":" + importPath), Kind: graph.NodeImport, Name: importPath, File: file, StartLine: 3, EndLine: 3, Metadata: map[string]any{"Path": importPath}})
	return g
}

func stepByKind(steps []language.EvidenceStep, kind string) *language.EvidenceStep {
	for index := range steps {
		if steps[index].Kind == kind {
			return &steps[index]
		}
	}
	return nil
}
