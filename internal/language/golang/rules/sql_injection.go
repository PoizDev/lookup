package rules

import (
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// SQLInjection implements SEC-002: detection of potential SQL injection in Go code.
//
// It scans function/method bodies for database query calls (db.Query, db.Exec, etc.)
// where the SQL string is built via string concatenation or fmt.Sprintf instead
// of using parameterized queries with placeholders (?, $1, etc.).
type SQLInjection struct{}

var _ language.AnalysisRule = (*SQLInjection)(nil)

func (r *SQLInjection) ID() string           { return "SEC-002" }
func (r *SQLInjection) CategoryName() string { return "Security" }
func (r *SQLInjection) SeverityName() string { return "High" }

// dbFuncPattern matches common database function calls that accept SQL strings.
var dbFuncPattern = regexp.MustCompile(
	`\b(?:db|tx|conn|pool|stmt|rows)\s*\.\s*` +
		`(?:Query|QueryRow|Exec|QueryContext|QueryRowContext|ExecContext|Prepare|PrepareContext)\s*\(`,
)

// concatInQueryPattern matches string concatenation inside a db function call line.
// Detects patterns like: db.Query("SELECT * FROM users WHERE id = " + id)
var concatInQueryPattern = regexp.MustCompile(
	`(?:Query|QueryRow|Exec|QueryContext|QueryRowContext|ExecContext|Prepare|PrepareContext)\s*\([^)]*"[^"]*"\s*\+`,
)

// sprintfInQueryPattern matches fmt.Sprintf used to build SQL inside a db call.
// Detects patterns like: db.Query(fmt.Sprintf("SELECT * FROM users WHERE id = %s", id))
var sprintfInQueryPattern = regexp.MustCompile(
	`(?:Query|QueryRow|Exec|QueryContext|QueryRowContext|ExecContext|Prepare|PrepareContext)\s*\(\s*fmt\.Sprintf\s*\(`,
)

// parameterizedPattern matches safe parameterized query placeholders.
var parameterizedPattern = regexp.MustCompile(`\?\s*[,)]|\$\d+`)

func (r *SQLInjection) Check(g *graph.Graph) []language.PotentialFinding {
	var findings []language.PotentialFinding

	g.Mu().RLock()
	defer g.Mu().RUnlock()

	for _, node := range g.Nodes {
		if node.Kind != graph.NodeFunction && node.Kind != graph.NodeMethod {
			continue
		}

		body := getBodyString(node)
		if body == "" {
			continue
		}

		// Quick check: does the body contain any database call at all?
		if !dbFuncPattern.MatchString(body) {
			continue
		}

		lines := strings.Split(body, "\n")
		for lineIdx, line := range lines {
			trimmed := strings.TrimSpace(line)

			// Check for string concatenation in query
			if concatInQueryPattern.MatchString(trimmed) {
				// Make sure it's not just literal concatenation (no variables)
				if isLiteralOnlyConcat(trimmed) {
					continue
				}
				// Make sure it's not using parameterized queries
				if parameterizedPattern.MatchString(trimmed) {
					continue
				}

				findings = append(findings, language.PotentialFinding{
					RuleID:   r.ID(),
					Category: r.CategoryName(),
					Severity: r.SeverityName(),
					Title:    "Potential SQL injection via string concatenation",
					Description: "A database query is built using string concatenation instead of " +
						"parameterized queries. This can lead to SQL injection if user input " +
						"is included in the concatenated string. Use query placeholders (?, $1) instead.",
					Observation:     "A database-call argument on the supplied line is constructed with non-literal string concatenation and no recognized placeholder pattern.",
					Hypothesis:      "If an untrusted value contributes to the concatenation, the query may permit SQL injection.",
					File:            node.File,
					StartLine:       node.StartLine + lineIdx,
					EndLine:         node.StartLine + lineIdx,
					CodeSnippet:     trimmed,
					AffectedSymbols: []string{node.Name},
				})
				continue
			}

			// Check for fmt.Sprintf in query
			if sprintfInQueryPattern.MatchString(trimmed) {
				findings = append(findings, language.PotentialFinding{
					RuleID:   r.ID(),
					Category: r.CategoryName(),
					Severity: r.SeverityName(),
					Title:    "Potential SQL injection via fmt.Sprintf",
					Description: "A database query is built using fmt.Sprintf instead of " +
						"parameterized queries. This can lead to SQL injection if user input " +
						"is interpolated into the format string. Use query placeholders (?, $1) instead.",
					Observation:     "A database-call argument on the supplied line is constructed with fmt.Sprintf.",
					Hypothesis:      "If an untrusted value is formatted into SQL syntax, the query may permit SQL injection.",
					File:            node.File,
					StartLine:       node.StartLine + lineIdx,
					EndLine:         node.StartLine + lineIdx,
					CodeSnippet:     trimmed,
					AffectedSymbols: []string{node.Name},
				})
			}
		}
	}

	return findings
}

// isLiteralOnlyConcat checks if a string concatenation only involves string literals.
// e.g., "SELECT " + "* FROM users" is safe (no variable injection).
func isLiteralOnlyConcat(line string) bool {
	// Find the part after the opening parenthesis of the db call
	idx := strings.Index(line, "(")
	if idx < 0 {
		return false
	}
	queryPart := line[idx+1:]

	// Split by + and check each part
	parts := strings.Split(queryPart, "+")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		// Remove trailing ), if present
		trimmed = strings.TrimRight(trimmed, " ),")
		if trimmed == "" {
			continue
		}
		// A literal must be a quoted string
		if !isStringLiteral(trimmed) {
			return false
		}
	}
	return true
}

// isStringLiteral checks if a value is a Go string literal.
func isStringLiteral(s string) bool {
	s = strings.TrimSpace(s)
	return (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`"))
}

// getBodyString safely retrieves the Body metadata from a function/method node.
func getBodyString(node *graph.Node) string {
	if node.Metadata == nil {
		return ""
	}
	val, ok := node.Metadata["Body"]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}
