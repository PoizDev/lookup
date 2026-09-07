package maintainability

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// CyclomaticComplexity implements MNT-002: high cyclomatic complexity detection.
//
// Calculates cyclomatic complexity by counting control flow decision points:
// if, else if, for, while, case, &&, ||, select, etc.
// Complexity > 10 is reported as Medium severity, > 20 as High severity.
type CyclomaticComplexity struct{}

var _ analyzer.Rule = (*CyclomaticComplexity)(nil)

func (r *CyclomaticComplexity) ID() string                  { return "MNT-002" }
func (r *CyclomaticComplexity) Category() analyzer.Category { return analyzer.CategoryMaintainability }
func (r *CyclomaticComplexity) Severity() analyzer.Severity { return analyzer.SeverityMedium }
func (r *CyclomaticComplexity) ReviewPolicy() language.ReviewPolicy {
	return language.ReviewPolicyStaticAuthoritative
}

const (
	ComplexityMediumThreshold = 10
	ComplexityHighThreshold   = 20
)

var branchPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bif\b`),
	regexp.MustCompile(`\bfor\b`),
	regexp.MustCompile(`\bwhile\b`),
	regexp.MustCompile(`\bswitch\b`),
	regexp.MustCompile(`\bcase\b`),
	regexp.MustCompile(`\bselect\b`),
	regexp.MustCompile(`\bcatch\b`),
	regexp.MustCompile(`&&`),
	regexp.MustCompile(`\|\|`),
	regexp.MustCompile(`\?`),
}

func (r *CyclomaticComplexity) Check(g *graph.Graph) []language.PotentialFinding {
	var findings []language.PotentialFinding

	g.Mu().RLock()
	defer g.Mu().RUnlock()

	for _, node := range g.Nodes {
		if node.Kind != graph.NodeFunction && node.Kind != graph.NodeMethod {
			continue
		}

		body := getMetadataString(node, "Body")
		if body == "" {
			continue
		}

		complexity := computeComplexity(body)
		if complexity > ComplexityMediumThreshold {
			severity := analyzer.SeverityMedium
			if complexity > ComplexityHighThreshold {
				severity = analyzer.SeverityHigh
			}

			finding := language.PotentialFinding{
				RuleID:          r.ID(),
				Category:        string(r.Category()),
				Severity:        string(severity),
				Title:           fmt.Sprintf("High cyclomatic complexity (%d)", complexity),
				Description:     fmt.Sprintf("Function `%s` has a cyclomatic complexity of %d (threshold: %d). High complexity indicates complex control flow that is hard to understand, maintain, and thoroughly test. Consider refactoring with guard clauses or helper functions.", node.Name, complexity, ComplexityMediumThreshold),
				File:            node.File,
				StartLine:       node.StartLine,
				EndLine:         node.EndLine,
				CodeSnippet:     fmt.Sprintf("func %s(...) [complexity: %d]", node.Name, complexity),
				AffectedSymbols: []string{node.Name},
				Confidence:      1,
			}

			findings = append(findings, finding)
		}
	}

	return findings
}

func computeComplexity(body string) int {
	complexity := 1
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		for _, p := range branchPatterns {
			matches := p.FindAllStringIndex(trimmed, -1)
			complexity += len(matches)
		}
	}

	return complexity
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
