package maintainability

import (
	"fmt"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// FunctionLength implements MNT-001: excessive function length detection.
//
// Functions with more than 100 lines are flagged with Medium severity,
// and functions with more than 200 lines are flagged with High severity.
type FunctionLength struct{}

var _ analyzer.Rule = (*FunctionLength)(nil)

func (r *FunctionLength) ID() string                  { return "MNT-001" }
func (r *FunctionLength) Category() analyzer.Category { return analyzer.CategoryMaintainability }
func (r *FunctionLength) Severity() analyzer.Severity { return analyzer.SeverityMedium }
func (r *FunctionLength) ReviewPolicy() language.ReviewPolicy {
	return language.ReviewPolicyStaticAuthoritative
}

const (
	MediumThreshold = 100
	HighThreshold   = 200
)

func (r *FunctionLength) Check(g *graph.Graph) []language.PotentialFinding {
	var findings []language.PotentialFinding

	g.Mu().RLock()
	defer g.Mu().RUnlock()

	for _, node := range g.Nodes {
		if node.Kind != graph.NodeFunction && node.Kind != graph.NodeMethod {
			continue
		}

		if node.StartLine <= 0 || node.EndLine <= 0 {
			continue
		}

		lineCount := node.EndLine - node.StartLine + 1
		if lineCount > MediumThreshold {
			severity := analyzer.SeverityMedium
			if lineCount > HighThreshold {
				severity = analyzer.SeverityHigh
			}

			finding := language.PotentialFinding{
				RuleID:          r.ID(),
				Category:        string(r.Category()),
				Severity:        string(severity),
				Title:           fmt.Sprintf("Function is excessively long (%d lines)", lineCount),
				Description:     fmt.Sprintf("Function `%s` spans %d lines (threshold: %d). Long functions are harder to read, maintain, and test. Consider breaking it down into smaller, cohesive helper functions.", node.Name, lineCount, MediumThreshold),
				File:            node.File,
				StartLine:       node.StartLine,
				EndLine:         node.EndLine,
				CodeSnippet:     fmt.Sprintf("func %s(...) [lines %d-%d]", node.Name, node.StartLine, node.EndLine),
				AffectedSymbols: []string{node.Name},
				Confidence:      1,
			}

			findings = append(findings, finding)
		}
	}

	return findings
}
