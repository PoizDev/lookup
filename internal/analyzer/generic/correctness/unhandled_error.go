package correctness

import (
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// UnhandledError implements COR-001: detection of unhandled errors in source code.
//
// It scans function/method bodies for patterns where an error return value is discarded,
// specifically checking for blank identifier assignments like `_, _ =`, `val, _ :=`,
// or `_ = someFunc()` where error values are explicitly ignored.
type UnhandledError struct{}

var _ analyzer.Rule = (*UnhandledError)(nil)

func (r *UnhandledError) ID() string                  { return "COR-001" }
func (r *UnhandledError) Category() analyzer.Category { return analyzer.CategoryCorrectness }
func (r *UnhandledError) Severity() analyzer.Severity { return analyzer.SeverityMedium }

// blankAssignmentPatterns match blank identifier assignments that ignore error returns.
var blankAssignmentPatterns = []*regexp.Regexp{
	// Matches `_, err := ...` or `val, _ := ...` or `val, _ = ...`
	regexp.MustCompile(`(?:^|[\s\t]+)(?:[a-zA-Z0-9_]+,\s*)?_\s*(?::=|=)\s*([a-zA-Z0-9_\.]+\([^)]*\))`),
	// Matches `_ = someFunc(...)` or `_ = obj.Method(...)`
	regexp.MustCompile(`(?:^|[\s\t]+)_\s*=\s*([a-zA-Z0-9_\.]+\([^)]*\))`),
}

// knownSafeBlankFunctions are calls where ignoring return/error is standard or safe.
var knownSafeBlankFunctions = map[string]bool{
	"recover":             true,
	"time.Now":            true,
	"copy":                true,
	"append":              true,
	"delete":              true,
	"close":               true,
	"fmt.Print":           true,
	"fmt.Println":         true,
	"fmt.Printf":          true,
	"fmt.Fprint":          true,
	"fmt.Fprintln":        true,
	"fmt.Fprintf":         true,
	"fmt.Sprint":          true,
	"fmt.Sprintln":        true,
	"fmt.Sprintf":         true,
	"log.Print":           true,
	"log.Println":         true,
	"log.Printf":          true,
	"atomic.AddInt":       true,
	"hash.Write":          true,
	"hasher.Write":        true,
	"h.Write":             true,
	"io.Copy":             true,
	"response.Body.Close": true,
	"req.Body.Close":      true,
	"res.Body.Close":      true,
}

func (r *UnhandledError) Check(g *graph.Graph) []language.PotentialFinding {
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
		for lineIdx, line := range lines {
			trimmed := strings.TrimSpace(line)

			// Skip comment lines
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
				continue
			}

			for _, pattern := range blankAssignmentPatterns {
				matches := pattern.FindStringSubmatch(trimmed)
				if len(matches) > 1 {
					callExpr := matches[1]
					funcName := extractCalleeName(callExpr)

					if isSafeFunction(funcName) {
						continue
					}

					finding := language.PotentialFinding{
						RuleID:          r.ID(),
						Category:        string(r.Category()),
						Severity:        string(r.Severity()),
						Title:           "Unhandled error (discarded return value)",
						Description:     "A function return value is discarded using the blank identifier `_`. If this function returns an error, ignoring it may lead to silent failures or undefined behavior.",
						Observation:     "An assignment in the supplied line discards a return position with the blank identifier.",
						Hypothesis:      "If the discarded position carries an error, a failure may go unhandled.",
						File:            node.File,
						StartLine:       node.StartLine + lineIdx,
						EndLine:         node.StartLine + lineIdx,
						CodeSnippet:     trimmed,
						AffectedSymbols: []string{node.Name},
					}

					findings = append(findings, finding)
					break
				}
			}
		}
	}

	return findings
}

func extractCalleeName(expr string) string {
	idx := strings.Index(expr, "(")
	if idx > 0 {
		return strings.TrimSpace(expr[:idx])
	}
	return expr
}

func isSafeFunction(funcName string) bool {
	if knownSafeBlankFunctions[funcName] {
		return true
	}
	for safe := range knownSafeBlankFunctions {
		if strings.HasPrefix(funcName, safe) {
			return true
		}
	}
	return false
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
