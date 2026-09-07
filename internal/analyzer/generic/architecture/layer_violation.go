package architecture

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// LayerViolation implements ARC-001: architectural layer boundary violation detection.
//
// Identifies architectural violations where upper layers (e.g. presentation/handlers/controllers)
// directly access or import low-level persistence layers (e.g. db/repository/store) bypassing
// the domain / service business logic layer.
type policyOrigin string

const (
	policyOriginBuiltIn    policyOrigin = "built_in_heuristic"
	policyOriginConfigured policyOrigin = "user_configured_policy"
	policyOriginExplicit   policyOrigin = "explicit_repository_policy"
)

type layerPolicy struct {
	PresentationKeywords []string
	DataKeywords         []string
	Origin               policyOrigin
	Basis                string
}

type LayerViolation struct {
	policy *layerPolicy
}

var _ analyzer.Rule = (*LayerViolation)(nil)

func (r *LayerViolation) ID() string                  { return "ARC-001" }
func (r *LayerViolation) Category() analyzer.Category { return analyzer.CategoryArchitecture }
func (r *LayerViolation) Severity() analyzer.Severity { return analyzer.SeverityMedium }

var upperLayerKeywords = []string{
	"/handler", "/handlers", "/controller", "/controllers",
	"/transport", "/api/", "/http/", "/grpc/", "/rest/",
}

var dataLayerKeywords = []string{
	"/db", "/database", "/dao", "/persistence",
}

func (r *LayerViolation) Check(g *graph.Graph) []language.PotentialFinding {
	var findings []language.PotentialFinding
	policy := r.effectivePolicy()

	g.Mu().RLock()
	defer g.Mu().RUnlock()

	for _, node := range g.Nodes {
		if node.Kind != graph.NodeImport {
			continue
		}

		importPath := getMetadataString(node, "Path")
		if importPath == "" {
			importPath = node.Name
		}

		fileNormalized := filepath.ToSlash(node.File)
		var sourceKeyword string
		for _, kw := range policy.PresentationKeywords {
			if strings.Contains(fileNormalized, kw) {
				sourceKeyword = kw
				break
			}
		}

		if sourceKeyword == "" {
			continue
		}

		importNormalized := "/" + filepath.ToSlash(importPath)
		var importKeyword string
		for _, kw := range policy.DataKeywords {
			if strings.Contains(importNormalized, kw) {
				importKeyword = kw
				break
			}
		}

		if importKeyword != "" {
			basis := fmt.Sprintf("source_path=%s source_keyword=%s import_path=%s import_keyword=%s origin=%s", fileNormalized, sourceKeyword, importPath, importKeyword, policy.Origin)
			if policy.Basis != "" {
				basis += " basis=" + policy.Basis
			}
			steps := []language.EvidenceStep{
				{Kind: "architecture_import", File: node.File, Line: node.StartLine, Expression: fmt.Sprintf("import %q", importPath), Message: "The source file directly imports this path."},
				{Kind: "architecture_basis", File: node.File, Line: node.StartLine, Expression: basis, Message: "Exact deterministic path classifications used by ARC-001."},
			}
			if !policy.establishesBoundary() {
				steps = append(steps, language.EvidenceStep{Kind: "architecture_policy_missing", Message: "repository architecture policy unavailable; the layer mapping is only a built-in heuristic"})
			}
			finding := language.PotentialFinding{
				RuleID:   r.ID(),
				Category: string(r.Category()),
				Severity: string(r.Severity()),
				Title:    "Architectural layer violation (handler directly imports database/repository)",
				Description: fmt.Sprintf(
					"File `%s` in the presentation/transport layer directly imports data layer `%s`. Presentation layers should depend on service/use-case interfaces rather than low-level database repositories.",
					filepath.Base(node.File), importPath,
				),
				Observation:      fmt.Sprintf("A source path matches presentation-layer keyword %q and directly imports a path matching data-layer keyword %q (%s).", sourceKeyword, importKeyword, importPath),
				Hypothesis:       "The direct import may bypass an intended service or use-case boundary if the path conventions reflect the repository's architecture.",
				File:             node.File,
				StartLine:        node.StartLine,
				EndLine:          node.EndLine,
				CodeSnippet:      fmt.Sprintf("import \"%s\"", importPath),
				AffectedSymbols:  []string{importPath},
				Confidence:       0.70,
				EvidenceStrength: "moderate",
				EvidenceSteps:    steps,
			}
			findings = append(findings, finding)
		}
	}

	return findings
}

func (r *LayerViolation) effectivePolicy() layerPolicy {
	if r != nil && r.policy != nil {
		policy := *r.policy
		policy.PresentationKeywords = append([]string(nil), r.policy.PresentationKeywords...)
		policy.DataKeywords = append([]string(nil), r.policy.DataKeywords...)
		return policy
	}
	return layerPolicy{PresentationKeywords: append([]string(nil), upperLayerKeywords...), DataKeywords: append([]string(nil), dataLayerKeywords...), Origin: policyOriginBuiltIn, Basis: "Lookup default path-keyword mapping"}
}

func (policy layerPolicy) establishesBoundary() bool {
	if strings.TrimSpace(policy.Basis) == "" {
		return false
	}
	return policy.Origin == policyOriginConfigured || policy.Origin == policyOriginExplicit
}

func getMetadataString(node *graph.Node, key string) string {
	if node.Metadata == nil {
		return ""
	}
	val, ok := node.Metadata[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}
