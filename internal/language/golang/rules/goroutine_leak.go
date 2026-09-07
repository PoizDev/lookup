package rules

import (
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// GoroutineLeak implements PER-002: Go-specific goroutine and memory leak detection.
//
// Identifies common Go concurrency leak patterns:
//  1. `time.After` inside loops/select blocks (each call allocates a timer that is not GC'd until expiry).
//  2. `context.WithCancel`, `WithTimeout`, `WithDeadline` without a corresponding `defer cancel()`.
//  3. Unbounded goroutine creation in `for` loops without worker pools or concurrency control.
type GoroutineLeak struct{}

var _ language.AnalysisRule = (*GoroutineLeak)(nil)

func (r *GoroutineLeak) ID() string           { return "PER-002" }
func (r *GoroutineLeak) CategoryName() string { return "Performance" }
func (r *GoroutineLeak) SeverityName() string { return "High" }

var contextWithPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:,\s*([a-zA-Z0-9_]+)\s*(?::=|=)\s*context\.(?:WithCancel|WithTimeout|WithDeadline|WithCancelCause)\s*\()`),
}

var timeAfterPattern = regexp.MustCompile(`\btime\.After\s*\(`)
var goFuncPattern = regexp.MustCompile(`\bgo\s+(?:func|[a-zA-Z0-9_\.]+\s*\()`)

func (r *GoroutineLeak) Check(g *graph.Graph) []language.PotentialFinding {
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

		lines := strings.Split(body, "\n")
		loopDepth := 0
		inLoop := false

		for lineIdx, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
				continue
			}

			// 1. Check for loop tracking
			if strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "for{") || strings.HasPrefix(trimmed, "while ") {
				inLoop = true
			}

			openCount := strings.Count(trimmed, "{")
			closeCount := strings.Count(trimmed, "}")

			if inLoop {
				loopDepth += openCount - closeCount
				if loopDepth <= 0 {
					inLoop = false
					loopDepth = 0
				}
			}

			// 2. Check for time.After inside loop (memory leak pattern)
			if inLoop && loopDepth > 0 && timeAfterPattern.MatchString(trimmed) {
				findings = append(findings, language.PotentialFinding{
					RuleID:   r.ID(),
					Category: r.CategoryName(),
					Severity: r.SeverityName(),
					Title:    "Memory leak: time.After inside loop",
					Description: "`time.After` allocates a new `time.Timer` on every iteration which is NOT garbage collected " +
						"until the timer duration elapses. In repeated loops or select blocks, this leads to significant memory leaks. " +
						"Use `time.NewTimer` and reuse it with `timer.Reset()`, or use `time.NewTicker` instead.",
					Observation:     "A call to time.After occurs syntactically inside a loop body.",
					Hypothesis:      "Repeated iterations before prior timers expire may retain enough timers to cause material memory pressure.",
					File:            node.File,
					StartLine:       node.StartLine + lineIdx,
					EndLine:         node.StartLine + lineIdx,
					CodeSnippet:     trimmed,
					AffectedSymbols: []string{node.Name},
				})
			}

			// 3. Check for unbounded `go func()` in loop
			if inLoop && loopDepth > 0 && goFuncPattern.MatchString(trimmed) {
				findings = append(findings, language.PotentialFinding{
					RuleID:   r.ID(),
					Category: r.CategoryName(),
					Severity: "Medium",
					Title:    "Goroutine leak risk: unbounded goroutine spawning in loop",
					Description: "Spawning goroutines directly inside a loop without a worker pool, semaphore, or rate limiter " +
						"can cause unbounded concurrency, high memory consumption, and goroutine starvation under load.",
					Observation:     "A go statement occurs syntactically inside a loop body.",
					Hypothesis:      "Depending on the loop bound and goroutine completion rate, active goroutines may accumulate without an effective concurrency limit.",
					File:            node.File,
					StartLine:       node.StartLine + lineIdx,
					EndLine:         node.StartLine + lineIdx,
					CodeSnippet:     trimmed,
					AffectedSymbols: []string{node.Name},
				})
			}

			// 4. Check for context cancel missing defer
			for _, cp := range contextWithPatterns {
				matches := cp.FindStringSubmatch(trimmed)
				if len(matches) > 1 {
					cancelVar := matches[1]
					if cancelVar != "_" {
						// Check if defer cancelVar() exists in the function body
						deferPattern := "defer " + cancelVar + "()"
						if !strings.Contains(body, deferPattern) && !strings.Contains(body, cancelVar+"()") {
							findings = append(findings, language.PotentialFinding{
								RuleID:   r.ID(),
								Category: r.CategoryName(),
								Severity: r.SeverityName(),
								Title:    "Context leak: missing `defer " + cancelVar + "()`",
								Description: "The context cancellation function `" + cancelVar + "` is created but never deferred. " +
									"Failing to call `defer " + cancelVar + "()` will leak context resources and any underlying goroutines/timers " +
									"until parent context expiration.",
								Observation:     "The bounded function body contains a context cancellation constructor and no visible call to its cancellation value.",
								Hypothesis:      "If ownership does not escape and cancellation is not performed elsewhere, context-associated resources may remain live unnecessarily.",
								File:            node.File,
								StartLine:       node.StartLine + lineIdx,
								EndLine:         node.StartLine + lineIdx,
								CodeSnippet:     trimmed,
								AffectedSymbols: []string{node.Name, cancelVar},
							})
						}
					}
				}
			}
		}
	}

	return findings
}
