package correctness

import (
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// NilDereference implements COR-002: detection of potential nil pointer dereferences.
//
// It analyzes function/method bodies for patterns where a variable is assigned from a
// function call, type assertion, or pointer retrieval and subsequently accessed (field access
// or method invocation) without an intervening nil check guard.
type NilDereference struct{}

var _ analyzer.Rule = (*NilDereference)(nil)

func (r *NilDereference) ID() string                  { return "COR-002" }
func (r *NilDereference) Category() analyzer.Category { return analyzer.CategoryCorrectness }
func (r *NilDereference) Severity() analyzer.Severity { return analyzer.SeverityHigh }

// varAssignPattern matches single-variable assignments from function/method calls or type assertions.
// e.g. `ptr := getPtr()` or `node := tree.RootNode()` or `val := x.(*Type)`
var varAssignPattern = regexp.MustCompile(`^([a-zA-Z0-9_]+)\s*:=\s*([a-zA-Z0-9_\.]+\([^)]*\)|\w+\.\(\*\w+\))`)

// knownNonNilConstructors are factory functions that are historically/semantically known never to return nil.
var knownNonNilConstructors = map[string]bool{
	"sha256.New":               true,
	"md5.New":                  true,
	"sha1.New":                 true,
	"sha512.New":               true,
	"zap.NewProductionConfig":  true,
	"zap.NewDevelopmentConfig": true,
	"bufio.NewScanner":         true,
	"bufio.NewReader":          true,
	"bufio.NewWriter":          true,
	"bytes.NewBuffer":          true,
	"bytes.NewReader":          true,
	"strings.NewReader":        true,
	"progress.New":             true,
	"language.NewRegistry":     true,
	"sitter.NewParser":         true,
	"make":                     true,
}

func (r *NilDereference) Check(g *graph.Graph) []language.PotentialFinding {
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
		// Look for assignments followed by immediate unsafe accesses
		for i := 0; i < len(lines)-1; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
				continue
			}

			matches := varAssignPattern.FindStringSubmatch(trimmed)
			if len(matches) > 1 {
				varName := matches[1]
				assignRight := matches[2]

				// If variable name is err or blank, skip
				if varName == "_" || varName == "err" || strings.HasSuffix(varName, "Err") {
					continue
				}

				// Check if the assignment is a known non-nil constructor
				funcName := extractCalleeName(assignRight)
				if knownNonNilConstructors[funcName] {
					continue
				}

				// Check the next few lines (up to 4 lines)
				maxLookahead := 4
				if i+maxLookahead >= len(lines) {
					maxLookahead = len(lines) - 1 - i
				}

				varDot := varName + "."
				starVar := "*" + varName
				nilCheck1 := varName + " != nil"
				nilCheck2 := varName + " == nil"
				nilCheck3 := varName + "!=nil"
				nilCheck4 := varName + "==nil"

				for j := 1; j <= maxLookahead; j++ {
					nextTrimmed := strings.TrimSpace(lines[i+j])
					if strings.HasPrefix(nextTrimmed, "//") {
						continue
					}

					// If a nil check is found first, it is safe
					if strings.Contains(nextTrimmed, nilCheck1) ||
						strings.Contains(nextTrimmed, nilCheck2) ||
						strings.Contains(nextTrimmed, nilCheck3) ||
						strings.Contains(nextTrimmed, nilCheck4) {
						break
					}

					// If an assignment or return occurs, stop tracking
					if strings.Contains(nextTrimmed, varName+" =") || strings.Contains(nextTrimmed, varName+" :=") {
						break
					}

					// If accessed or dereferenced directly without prior nil check
					if (strings.Contains(nextTrimmed, varDot) || strings.Contains(nextTrimmed, starVar)) &&
						!strings.Contains(nextTrimmed, "if ") &&
						!strings.Contains(nextTrimmed, "!= nil") &&
						!strings.Contains(nextTrimmed, "== nil") {

						// Check if assignment might return a pointer / interface
						if strings.Contains(assignRight, "New") ||
							strings.Contains(assignRight, "Find") ||
							strings.Contains(assignRight, "Get") ||
							strings.Contains(assignRight, "Child") ||
							strings.Contains(assignRight, "Parent") ||
							strings.Contains(assignRight, ".(*") {

							finding := language.PotentialFinding{
								RuleID:          r.ID(),
								Category:        string(r.Category()),
								Severity:        string(r.Severity()),
								Title:           "Potential nil pointer dereference risk",
								Description:     "Variable `" + varName + "` is assigned from a call/assertion that can return nil, and is accessed directly without an intervening nil check.",
								Observation:     "A value from a pointer-like call or assertion is accessed within the bounded lookahead without a visible intervening nil comparison.",
								Hypothesis:      "If the producing expression can return nil on this path, the access may panic.",
								File:            node.File,
								StartLine:       node.StartLine + i + j,
								EndLine:         node.StartLine + i + j,
								CodeSnippet:     nextTrimmed,
								AffectedSymbols: []string{node.Name, varName},
							}

							findings = append(findings, finding)
							break
						}
					}
				}
			}
		}
	}

	return findings
}
