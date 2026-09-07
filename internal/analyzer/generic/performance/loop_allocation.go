package performance

import (
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// LoopAllocation implements PER-001: detection of expensive operations inside loops.
//
// Identifies costly allocations and calls inside loop bodies such as:
//   - Regex compilation (regexp.Compile, regexp.MustCompile)
//   - JSON marshaling/unmarshaling (json.Marshal, json.Unmarshal)
//   - Database queries inside loops (N+1 query pattern)
//   - Repeated buffer/slice/map allocations in loops (make([]..., make(map[...))
type LoopAllocation struct{}

var _ analyzer.Rule = (*LoopAllocation)(nil)

func (r *LoopAllocation) ID() string                  { return "PER-001" }
func (r *LoopAllocation) Category() analyzer.Category { return analyzer.CategoryPerformance }
func (r *LoopAllocation) Severity() analyzer.Severity { return analyzer.SeverityMedium }

type expensivePattern struct {
	regex       *regexp.Regexp
	description string
}

var expensivePatterns = []expensivePattern{
	{
		regex:       regexp.MustCompile(`\bregexp\.(?:Compile|MustCompile)\s*\(`),
		description: "Regex compilation inside loop. Compile the regex once outside the loop to avoid severe CPU overhead.",
	},
	{
		regex:       regexp.MustCompile(`\bjson\.(?:Marshal|Unmarshal)\s*\(`),
		description: "JSON serialization inside loop. Consider batching or streaming serialization outside the hot loop.",
	},
	{
		regex:       regexp.MustCompile(`\b(?:db|tx|conn)\.(?:Query|QueryRow|Exec|QueryContext|ExecContext)\s*\(`),
		description: "Database query inside loop (N+1 query pattern). Use batch queries or JOINs instead of querying in each iteration.",
	},
	{
		regex:       regexp.MustCompile(`\bmake\s*\(\s*(?:\[\]|map\[)`),
		description: "Slice or map allocation inside loop. Consider allocating before the loop or reusing slices/buffers with reset.",
	},
}

func (r *LoopAllocation) Check(g *graph.Graph) []language.PotentialFinding {
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

		lines := strings.Split(body, "\n")
		loopDepth := 0
		inLoop := false

		for lineIdx, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
				continue
			}

			// Check for loop start
			if strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "for{") || strings.HasPrefix(trimmed, "while ") {
				inLoop = true
			}

			// Track braces
			openCount := strings.Count(trimmed, "{")
			closeCount := strings.Count(trimmed, "}")

			if inLoop {
				loopDepth += openCount - closeCount
				if loopDepth <= 0 {
					inLoop = false
					loopDepth = 0
				}
			}

			if inLoop && loopDepth > 0 {
				for _, p := range expensivePatterns {
					if p.regex.MatchString(trimmed) {
						finding := language.PotentialFinding{
							RuleID:          r.ID(),
							Category:        string(r.Category()),
							Severity:        string(r.Severity()),
							Title:           "Expensive operation inside loop",
							Description:     p.description,
							Observation:     "A configured allocation or expensive-call pattern occurs syntactically inside a loop body.",
							Hypothesis:      "Depending on iteration count and runtime cost, repeating this operation may materially increase resource use.",
							File:            node.File,
							StartLine:       node.StartLine + lineIdx,
							EndLine:         node.StartLine + lineIdx,
							CodeSnippet:     trimmed,
							AffectedSymbols: []string{node.Name},
						}
						findings = append(findings, finding)
					}
				}
			}
		}
	}

	return findings
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
