package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/poizdev/lookup/internal/dataflow"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/semantic"
)

type semanticRule struct {
	id       string
	category string
	severity string
	required []semantic.Ecosystem
	check    func(*semanticRule, *graph.Graph, *semantic.Index, []dataflow.Trace) []language.PotentialFinding
	policy   language.ReviewPolicy
}

func (r *semanticRule) ID() string           { return r.id }
func (r *semanticRule) CategoryName() string { return r.category }
func (r *semanticRule) SeverityName() string { return r.severity }
func (r *semanticRule) ReviewPolicy() language.ReviewPolicy {
	if r.policy == "" {
		return language.ReviewPolicyContextRequired
	}
	return r.policy
}
func (r *semanticRule) RequiredEcosystems() []semantic.Ecosystem {
	return append([]semantic.Ecosystem(nil), r.required...)
}
func (r *semanticRule) Check(g *graph.Graph) []language.PotentialFinding {
	index := g.SemanticIndex()
	for _, ecosystem := range r.required {
		if !index.HasEcosystem(ecosystem) {
			return nil
		}
	}
	return r.check(r, g, index, g.DataflowTraces())
}

// All returns the deterministic Go Analyzer v2 catalog.
func All() []language.AnalysisRule {
	rules := []language.AnalysisRule{
		factRule("GO-COR-001", "Correctness", "Medium", semantic.FactGuard, "error.ignored", "Ignored returned error", "A call result that can contain an error is discarded."),
		factRule("GO-COR-002", "Correctness", "High", semantic.FactGuard, "type_assertion.unchecked", "Unchecked type assertion", "A single-value type assertion can panic when the dynamic type does not match."),
		customRule("GO-COR-003", "Correctness", "Medium", []semantic.Ecosystem{semantic.EcosystemDatabaseSQL}, errMethodRule("database.rows", "Next", "Err", "rows.Err() is not checked")),
		customRule("GO-COR-004", "Correctness", "Medium", nil, errMethodRule("scanner", "Scan", "Err", "scanner.Err() is not checked")),
		customRule("GO-COR-005", "Correctness", "High", []semantic.Ecosystem{semantic.EcosystemDatabaseSQL}, lifecycleRule("database.transaction", []string{"commit", "rollback"}, "Database transaction is not completed")),

		customRule("GO-RES-001", "Correctness", "Medium", []semantic.Ecosystem{semantic.EcosystemNetHTTP}, lifecycleRule("http.response.body", []string{"close"}, "HTTP response body is not closed")),
		customRule("GO-RES-002", "Correctness", "Medium", []semantic.Ecosystem{semantic.EcosystemDatabaseSQL}, lifecycleRule("database.rows", []string{"close"}, "Database rows are not closed")),
		customRule("GO-RES-003", "Correctness", "Medium", []semantic.Ecosystem{semantic.EcosystemFilesystem}, lifecycleRule("file", []string{"close"}, "File is not closed")),
		customRule("GO-RES-004", "Correctness", "Medium", nil, lifecycleRule("ticker", []string{"stop"}, "Ticker is not stopped")),
		customRule("GO-RES-005", "Correctness", "Medium", nil, lifecycleRule("context.cancel", []string{"context.cancel"}, "Context cancellation function is not called")),

		customRule("GO-CON-001", "Correctness", "Medium", nil, goroutineLoopRule),
		factRule("GO-CON-002", "Correctness", "Low", semantic.FactConcurrencyOperation, "defer.in_loop", "Defer scheduled inside loop", "Deferred calls run when the surrounding function returns, not at the end of each iteration."),
		factRule("GO-CON-003", "Correctness", "High", semantic.FactConcurrencyOperation, "waitgroup.add_in_goroutine", "WaitGroup.Add called inside goroutine", "Calling Add after the goroutine starts can race with Wait."),
		customRule("GO-CON-004", "Correctness", "Medium", nil, lifecycleRule("mutex", []string{"unlock"}, "Mutex lock is not released")),

		customRule("GO-SEC-002", "Security", "Medium", nil, weakHashRule("crypto.weak.md5", "Weak MD5 primitive", "MD5 is unsuitable for security-sensitive integrity or password use.")),
		customRule("GO-SEC-003", "Security", "Medium", nil, weakHashRule("crypto.weak.sha1", "Weak SHA-1 primitive", "SHA-1 is unsuitable for collision-resistant security uses.")),
		factRule("GO-SEC-004", "Security", "High", semantic.FactCryptoOperation, "tls.insecure_skip_verify", "TLS certificate verification disabled", "InsecureSkipVerify permits connections without normal certificate verification."),
		customRule("GO-SEC-005", "Security", "Medium", []semantic.Ecosystem{semantic.EcosystemFilesystem}, unsafePermissionRule),

		authoritativeRule("GO-MNT-001", "Maintainability", "Medium", longFunctionRule),
		authoritativeRule("GO-MNT-002", "Maintainability", "Medium", complexityRule),
		customRule("GO-PER-001", "Performance", "Low", nil, expensiveLoopRule),

		taintRule("GO-SQL-SEC-001", "High", []semantic.Ecosystem{semantic.EcosystemDatabaseSQL}, "net/http", sqlSink, "Tainted HTTP input reaches database/sql execution"),
		taintRule("GO-CMD-SEC-001", "High", []semantic.Ecosystem{semantic.EcosystemOSExec}, "net/http", commandSink, "Tainted HTTP input reaches command execution"),
		taintSourceOperationRule("GO-CMD-SEC-002", "Medium", []semantic.Ecosystem{semantic.EcosystemOSExec, semantic.EcosystemFilesystem}, "environment", commandSink, "Environment input reaches shell command execution"),
		taintRule("GO-HTTP-SEC-001", "Medium", []semantic.Ecosystem{semantic.EcosystemNetHTTP}, "net/http", redirectSink, "User-controlled HTTP redirect"),
		taintFrameworksRule("GO-FS-SEC-001", "High", []semantic.Ecosystem{semantic.EcosystemFilesystem}, []string{"net/http", "gin", "fiber"}, fileSink, "Tainted HTTP input reaches a filesystem path"),

		taintRule("GIN-SEC-001", "High", []semantic.Ecosystem{semantic.EcosystemGin}, "gin", sqlSink, "Gin request input reaches SQL execution"),
		taintRule("GIN-SEC-002", "High", []semantic.Ecosystem{semantic.EcosystemGin, semantic.EcosystemOSExec}, "gin", commandSink, "Gin request input reaches command execution"),
		taintRule("GIN-SEC-003", "Medium", []semantic.Ecosystem{semantic.EcosystemGin}, "gin", redirectSink, "Gin request input controls a redirect"),

		taintRule("FIBER-SEC-001", "High", []semantic.Ecosystem{semantic.EcosystemFiber}, "fiber", sqlSink, "Fiber request input reaches SQL execution"),
		taintRule("FIBER-SEC-002", "High", []semantic.Ecosystem{semantic.EcosystemFiber, semantic.EcosystemOSExec}, "fiber", commandSink, "Fiber request input reaches command execution"),
		taintRule("FIBER-SEC-003", "Medium", []semantic.Ecosystem{semantic.EcosystemFiber}, "fiber", redirectSink, "Fiber request input controls a redirect"),

		customRule("GORM-SEC-001", "Security", "High", []semantic.Ecosystem{semantic.EcosystemGORM}, gormSQLRule),
		factRuleScoped("GORM-SEC-002", "Security", "High", []semantic.Ecosystem{semantic.EcosystemGORM}, semantic.FactDatabaseOperation, "gorm.allow_global_update", "GORM global updates enabled", "AllowGlobalUpdate permits update/delete operations without a WHERE condition."),
		factRuleScoped("GORM-SEC-003", "Security", "High", []semantic.Ecosystem{semantic.EcosystemGORM}, semantic.FactDatabaseOperation, "gorm.unscoped.delete", "Unscoped destructive GORM operation", "Unscoped delete bypasses soft-delete protections."),
		customRule("GORM-COR-001", "Correctness", "Medium", []semantic.Ecosystem{semantic.EcosystemGORM}, gormIgnoredErrorRule),
		customRule("GORM-COR-002", "Correctness", "High", []semantic.Ecosystem{semantic.EcosystemGORM}, lifecycleRule("gorm.transaction", []string{"commit", "rollback"}, "GORM transaction is not completed")),
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID() < rules[j].ID() })
	return rules
}

func customRule(id, category, severity string, required []semantic.Ecosystem, check func(*semanticRule, *graph.Graph, *semantic.Index, []dataflow.Trace) []language.PotentialFinding) language.AnalysisRule {
	return &semanticRule{id: id, category: category, severity: severity, required: required, check: check}
}

func authoritativeRule(id, category, severity string, check func(*semanticRule, *graph.Graph, *semantic.Index, []dataflow.Trace) []language.PotentialFinding) language.AnalysisRule {
	return &semanticRule{id: id, category: category, severity: severity, policy: language.ReviewPolicyStaticAuthoritative, check: check}
}

func factRule(id, category, severity string, kind semantic.FactKind, operation, title, description string) language.AnalysisRule {
	return factRuleScoped(id, category, severity, nil, kind, operation, title, description)
}

func factRuleScoped(id, category, severity string, required []semantic.Ecosystem, kind semantic.FactKind, operation, title, description string) language.AnalysisRule {
	return customRule(id, category, severity, required, func(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
		var findings []language.PotentialFinding
		for _, fact := range index.Facts(kind) {
			if fact.Operation == operation {
				findings = append(findings, factFinding(rule, fact, title, description, 0.85, "strong"))
			}
		}
		return findings
	})
}

func taintRule(id, severity string, required []semantic.Ecosystem, framework string, sinkMatch func(string) bool, title string) language.AnalysisRule {
	return taintFrameworksRule(id, severity, required, []string{framework}, sinkMatch, title)
}

func taintFrameworksRule(id, severity string, required []semantic.Ecosystem, frameworks []string, sinkMatch func(string) bool, title string) language.AnalysisRule {
	return customRule(id, "Security", severity, required, func(rule *semanticRule, _ *graph.Graph, _ *semantic.Index, traces []dataflow.Trace) []language.PotentialFinding {
		var findings []language.PotentialFinding
		for _, trace := range traces {
			if !containsString(frameworks, trace.Source.Metadata["framework"]) || !sinkMatch(trace.Sink.Operation) {
				continue
			}
			findings = append(findings, traceFinding(rule, trace, title))
		}
		return findings
	})
}

func taintSourceOperationRule(id, severity string, required []semantic.Ecosystem, sourceOperation string, sinkMatch func(string) bool, title string) language.AnalysisRule {
	return customRule(id, "Security", severity, required, func(rule *semanticRule, _ *graph.Graph, _ *semantic.Index, traces []dataflow.Trace) []language.PotentialFinding {
		var findings []language.PotentialFinding
		for _, trace := range traces {
			if trace.Source.Operation == sourceOperation && sinkMatch(trace.Sink.Operation) {
				findings = append(findings, traceFinding(rule, trace, title))
			}
		}
		return findings
	})
}

func traceFinding(rule *semanticRule, trace dataflow.Trace, title string) language.PotentialFinding {
	steps := []language.EvidenceStep{evidenceStep("source", trace.Source, "User-controlled input originates here")}
	chain := []string{trace.Source.Expression}
	for _, propagation := range trace.Propagation {
		steps = append(steps, evidenceStep("propagation", propagation, "Tainted value propagates through this expression"))
		chain = append(chain, propagation.Expression)
	}
	steps = append(steps, evidenceStep("sink", trace.Sink, "Tainted value reaches this sensitive operation"))
	chain = append(chain, trace.Sink.Expression)
	return language.PotentialFinding{
		RuleID: rule.id, Category: rule.category, Severity: rule.severity, Title: title,
		Description: "A deterministic bounded dataflow trace connects user-controlled HTTP input to a sensitive sink.",
		Observation: "A bounded dataflow trace connects the supplied external-input source to the supplied sensitive sink.",
		Hypothesis:  "The traced external input may influence the sensitive operation without a sufficient intervening control.",
		File:        trace.Sink.Location.File, StartLine: trace.Sink.Location.StartLine, EndLine: trace.Sink.Location.EndLine,
		CodeSnippet: trace.Sink.Expression, AffectedSymbols: uniqueStrings(append(trace.Source.Outputs, trace.Sink.Inputs...)),
		CallChain: chain, Confidence: trace.Confidence, EvidenceStrength: string(trace.Strength), EvidenceSteps: steps,
	}
}

func evidenceStep(kind string, fact semantic.Fact, message string) language.EvidenceStep {
	return language.EvidenceStep{Kind: kind, File: fact.Location.File, Line: fact.Location.StartLine, Column: fact.Location.StartColumn, Expression: fact.Expression, Message: message, RelatedSymbol: fact.Function}
}

func factFinding(rule *semanticRule, fact semantic.Fact, title, description string, confidence float64, strength string) language.PotentialFinding {
	return language.PotentialFinding{
		RuleID: rule.id, Category: rule.category, Severity: rule.severity, Title: title, Description: description,
		Observation: fmt.Sprintf("The semantic extractor emitted operation %q at the supplied source location.", fact.Operation), Hypothesis: description,
		File: fact.Location.File, StartLine: fact.Location.StartLine, EndLine: fact.Location.EndLine, CodeSnippet: fact.Expression,
		AffectedSymbols: uniqueStrings(append(append([]string(nil), fact.Outputs...), fact.Inputs...)), Confidence: confidence, EvidenceStrength: strength,
		EvidenceSteps: []language.EvidenceStep{evidenceStep("evidence", fact, description)},
	}
}

func goroutineLoopRule(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
	var findings []language.PotentialFinding
	for _, fact := range index.Facts(semantic.FactConcurrencyOperation) {
		if fact.Operation != "goroutine.loop" {
			continue
		}
		if fact.Metadata["loop_bound"] == "finite_collection" {
			item := factFinding(rule, fact, "Goroutine started during range iteration", "Starting one goroutine per range iteration may create excessive concurrent work depending on the collection or stream size, producer lifecycle, and completion rate.", .75, "moderate")
			item.Observation = "A goroutine is started once for each iteration of a range loop; the source does not establish an infinite iteration space."
			item.Hypothesis = "The range source or collection size and goroutine completion rate may still permit excessive concurrent work without an explicit concurrency limit."
			findings = append(findings, item)
			continue
		}
		item := factFinding(rule, fact, "Goroutine started inside loop with no static bound", "Repeated iterations may create unbounded concurrent work without an explicit concurrency limit.", .85, "strong")
		item.Observation = "A goroutine is started inside a loop for which the extractor found no static iteration bound."
		item.Hypothesis = "If the loop continues while goroutines remain active, concurrent work may grow unbounded."
		findings = append(findings, item)
	}
	return findings
}

func lifecycleRule(acquireOperation string, releaseOperations []string, title string) func(*semanticRule, *graph.Graph, *semantic.Index, []dataflow.Trace) []language.PotentialFinding {
	return func(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
		releases := index.Facts(semantic.FactResourceRelease)
		var findings []language.PotentialFinding
		for _, acquire := range index.Facts(semantic.FactResourceAcquire) {
			if acquire.Operation != acquireOperation || len(acquire.Outputs) == 0 {
				continue
			}
			resource := acquire.Outputs[0]
			if acquireOperation == "context.cancel" {
				resource = acquire.Outputs[len(acquire.Outputs)-1]
			}
			closed := false
			for _, release := range releases {
				if sameFunctionAfter(acquire, release) && containsString(releaseOperations, release.Operation) && containsString(release.Inputs, resource) {
					closed = true
					break
				}
			}
			if !closed {
				for _, call := range index.Facts(semantic.FactCall) {
					if !sameFunctionAfter(acquire, call) {
						continue
					}
					for _, releaseOperation := range releaseOperations {
						if strings.EqualFold(call.Operation, resource+"."+releaseOperation) || (releaseOperation == "context.cancel" && call.Operation == resource) {
							closed = true
							break
						}
					}
					if closed {
						break
					}
				}
			}
			if !closed && !resourceEscapes(index, acquire, resource) {
				findings = append(findings, factFinding(rule, acquire, title, "A resource is acquired but no later matching release is visible in the same function; branch-sensitive ownership is not inferred.", 0.70, "moderate"))
			}
		}
		return findings
	}
}

func weakHashRule(operation, title, description string) func(*semanticRule, *graph.Graph, *semantic.Index, []dataflow.Trace) []language.PotentialFinding {
	return func(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
		var findings []language.PotentialFinding
		for _, fact := range index.Facts(semantic.FactCryptoOperation) {
			if fact.Operation != operation || !securitySensitiveHashContext(fact) || kAnonymityPrefixProtocol(index, fact) {
				continue
			}
			findings = append(findings, factFinding(rule, fact, title, description, .80, "moderate"))
		}
		return findings
	}
}

func kAnonymityPrefixProtocol(index *semantic.Index, hash semantic.Fact) bool {
	if hash.Operation != "crypto.weak.sha1" {
		return false
	}
	for _, propagation := range index.Facts(semantic.FactPropagation) {
		if propagation.Location.File != hash.Location.File || propagation.Function != hash.Function {
			continue
		}
		expression := strings.ReplaceAll(propagation.Expression, " ", "")
		if strings.Contains(expression, "[:5]") && strings.Contains(expression, "[5:]") {
			return true
		}
	}
	return false
}

func securitySensitiveHashContext(fact semantic.Fact) bool {
	context := strings.ToLower(fact.Function + " " + fact.Expression + " " + strings.Join(fact.Inputs, " "))
	for _, token := range []string{"password", "passwd", "credential", "secret", "signature", "authenticate", "authentication"} {
		if strings.Contains(context, token) {
			return true
		}
	}
	return false
}

func errMethodRule(acquireOperation, iterationMethod, errorMethod, title string) func(*semanticRule, *graph.Graph, *semantic.Index, []dataflow.Trace) []language.PotentialFinding {
	return func(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
		calls := index.Facts(semantic.FactCall)
		var findings []language.PotentialFinding
		for _, acquire := range index.Facts(semantic.FactResourceAcquire) {
			if acquire.Operation != acquireOperation || len(acquire.Outputs) == 0 {
				continue
			}
			resource := acquire.Outputs[0]
			var iteration *semantic.Fact
			for _, call := range calls {
				if !sameFunctionAfter(acquire, call) {
					continue
				}
				if call.Operation == resource+"."+iterationMethod {
					copy := call
					iteration = &copy
				}
			}
			if iteration == nil || resourceEscapes(index, acquire, resource) {
				continue
			}
			postUse, line, loopEnd, regionOK := postIterationRegion(index, acquire, *iteration)
			checked := false
			if regionOK {
				for _, call := range calls {
					if sameFunctionAfter(acquire, call) && call.Operation == resource+"."+errorMethod && locationAtOrAfter(call.Location, loopEnd) {
						checked = true
						break
					}
				}
			}
			if !checked {
				item := factFinding(rule, acquire, title, "Iteration is visible but no later error check is visible in the same function.", 0.80, "moderate")
				item.EvidenceSteps = append(item.EvidenceSteps, evidenceStep("iteration", *iteration, "The resource iteration call is visible here."))
				if regionOK {
					item.EvidenceSteps = append(item.EvidenceSteps, language.EvidenceStep{Kind: "post_use", File: acquire.Location.File, Line: line, Expression: postUse, Message: "Complete source region after iteration through the end of the enclosing function."})
				} else {
					item.EvidenceSteps = append(item.EvidenceSteps, language.EvidenceStep{Kind: "hypothesis_evidence_missing", Message: "complete post-iteration region unavailable"})
				}
				findings = append(findings, item)
			}
		}
		return findings
	}
}

func postIterationRegion(index *semantic.Index, acquire, iteration semantic.Fact) (string, int, semantic.Location, bool) {
	for _, document := range index.Documents() {
		if document == nil || document.Path != acquire.Location.File || len(document.Source) == 0 {
			continue
		}
		var function *semantic.Function
		for i := range document.Functions {
			candidate := &document.Functions[i]
			if candidate.Name == acquire.Function && candidate.Location.StartLine <= acquire.Location.StartLine && candidate.Location.EndLine >= iteration.Location.EndLine {
				function = candidate
				break
			}
		}
		if function == nil || function.Location.EndLine <= 0 {
			return "", 0, semantic.Location{}, false
		}
		var loopLocation semantic.Location
		for _, control := range document.Facts {
			if control.Kind != semantic.FactControl || control.Operation != "for" || control.Function != acquire.Function {
				continue
			}
			if control.Location.StartLine <= iteration.Location.StartLine && control.Location.EndLine >= iteration.Location.EndLine && (loopLocation.EndLine == 0 || control.Location.EndLine < loopLocation.EndLine || control.Location.EndLine == loopLocation.EndLine && control.Location.StartLine > loopLocation.StartLine) {
				loopLocation = control.Location
			}
		}
		if loopLocation.EndLine == 0 || loopLocation.EndLine > function.Location.EndLine {
			return "", 0, semantic.Location{}, false
		}
		lines := strings.Split(string(document.Source), "\n")
		if loopLocation.EndLine < 1 || function.Location.EndLine > len(lines) {
			return "", 0, semantic.Location{}, false
		}
		column := max(0, loopLocation.EndColumn-1)
		if column > len(lines[loopLocation.EndLine-1]) {
			return "", 0, semantic.Location{}, false
		}
		regionLines := append([]string{lines[loopLocation.EndLine-1][column:]}, lines[loopLocation.EndLine:function.Location.EndLine]...)
		return strings.Join(regionLines, "\n"), loopLocation.EndLine, loopLocation, true
	}
	return "", 0, semantic.Location{}, false
}

func locationAtOrAfter(candidate, boundary semantic.Location) bool {
	if candidate.StartLine != boundary.EndLine {
		return candidate.StartLine > boundary.EndLine
	}
	return candidate.StartColumn >= boundary.EndColumn
}

func sameFunctionAfter(earlier, later semantic.Fact) bool {
	if earlier.Location.File != later.Location.File || earlier.Function != later.Function {
		return false
	}
	if earlier.Location.StartLine != later.Location.StartLine {
		return earlier.Location.StartLine < later.Location.StartLine
	}
	return earlier.Location.StartColumn <= later.Location.StartColumn
}

func resourceEscapes(index *semantic.Index, acquire semantic.Fact, resource string) bool {
	for _, returned := range index.Facts(semantic.FactReturn) {
		if sameFunctionAfter(acquire, returned) && containsString(returned.Inputs, resource) {
			return true
		}
	}
	for _, call := range index.Facts(semantic.FactCall) {
		if !sameFunctionAfter(acquire, call) || !containsString(call.Inputs, resource) || strings.HasPrefix(call.Operation, resource+".") {
			continue
		}
		return true
	}
	for _, transfer := range index.Facts(semantic.FactPropagation) {
		if !sameFunctionAfter(acquire, transfer) || !containsString(transfer.Inputs, resource) {
			continue
		}
		left := strings.TrimSpace(strings.SplitN(transfer.Expression, "=", 2)[0])
		if !strings.Contains(left, ".") {
			continue
		}
		field := selectorTail(left)
		for _, release := range index.Facts(semantic.FactResourceRelease) {
			if containsString(release.Inputs, field) || containsString(release.Inputs, strings.TrimSpace(left)) {
				return true
			}
		}
	}
	return false
}

func selectorTail(value string) string {
	value = strings.TrimSpace(value)
	if dot := strings.LastIndex(value, "."); dot >= 0 {
		return strings.TrimSpace(value[dot+1:])
	}
	return value
}

func complexityRule(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
	counts := map[string]int{}
	locations := map[string]semantic.Fact{}
	goFiles := map[string]bool{}
	for _, document := range index.Documents() {
		if document.Language == "Go" {
			goFiles[document.Path] = true
		}
	}
	for _, fact := range index.Facts(semantic.FactControl) {
		if !goFiles[fact.Location.File] {
			continue
		}
		key := fact.Location.File + "\x00" + fact.Function
		counts[key]++
		locations[key] = fact
	}
	var findings []language.PotentialFinding
	for key, decisions := range counts {
		complexity := decisions + 1
		if complexity <= 10 {
			continue
		}
		fact := locations[key]
		severity := rule.severity
		if complexity > 20 {
			severity = "High"
		}
		copyRule := *rule
		copyRule.severity = severity
		findings = append(findings, factFinding(&copyRule, fact, fmt.Sprintf("High Go cyclomatic complexity (%d)", complexity), "Complexity is calculated from Go AST control-flow and logical-expression nodes.", 1, "strong"))
	}
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].File < findings[j].File || findings[i].File == findings[j].File && findings[i].StartLine < findings[j].StartLine
	})
	return findings
}

func longFunctionRule(rule *semanticRule, g *graph.Graph, _ *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
	var findings []language.PotentialFinding
	g.Mu().RLock()
	defer g.Mu().RUnlock()
	for _, node := range g.Nodes {
		if node.Kind != graph.NodeFunction && node.Kind != graph.NodeMethod {
			continue
		}
		lines := node.EndLine - node.StartLine + 1
		if lines <= 100 {
			continue
		}
		severity := rule.severity
		if lines > 200 {
			severity = "High"
		}
		findings = append(findings, language.PotentialFinding{RuleID: rule.id, Category: rule.category, Severity: severity, Title: fmt.Sprintf("Function is excessively long (%d lines)", lines), Description: "Long functions are a maintainability quality signal.", File: node.File, StartLine: node.StartLine, EndLine: node.EndLine, AffectedSymbols: []string{node.Name}, Confidence: 1, EvidenceStrength: "strong"})
	}
	return findings
}

func expensiveLoopRule(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
	var findings []language.PotentialFinding
	for _, call := range index.Facts(semantic.FactCall) {
		if call.Metadata["in_loop"] != "true" || !expensiveCall(call.Operation) {
			continue
		}
		findings = append(findings, factFinding(rule, call, "Expensive operation inside loop", "The AST places this allocation or expensive call inside a Go loop.", 0.85, "strong"))
	}
	return findings
}

func unsafePermissionRule(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
	var findings []language.PotentialFinding
	for _, fact := range index.Facts(semantic.FactFileOperation) {
		if fact.Operation != "file.permission" && fact.Operation != "file.write" && fact.Operation != "file.open" {
			continue
		}
		arguments := strings.Split(fact.Metadata["arguments"], "\x1f")
		modeIndex, err := strconv.Atoi(fact.Metadata["mode_index"])
		if err != nil || modeIndex < 0 || modeIndex >= len(arguments) {
			continue
		}
		literal := strings.ReplaceAll(strings.TrimSpace(arguments[modeIndex]), "_", "")
		if strings.HasPrefix(literal, "0O") {
			literal = "0o" + literal[2:]
		}
		mode, err := strconv.ParseUint(literal, 0, 32)
		if err == nil && mode&0o002 != 0 {
			item := factFinding(rule, fact, "Requested file mode includes other-write", "Depending on creation semantics, process umask, existing-file behavior, and deployment ownership, the resulting path may be writable by unintended users.", 0.95, "strong")
			item.Observation = "A literal mode argument supplied to a filesystem operation contains the other-write permission bit."
			if strings.HasSuffix(fact.Metadata["callee"], ".Chmod") || fact.Metadata["callee"] == "os.Chmod" {
				item.Hypothesis = "If the operation succeeds, explicitly enabling other-write may make an existing path writable by unintended users depending on filesystem, ownership, and deployment access semantics."
			} else {
				item.Hypothesis = "Depending on creation semantics, process umask, existing-file behavior, and deployment ownership, the resulting path may be writable by unintended users."
			}
			findings = append(findings, item)
		}
	}
	return findings
}

func gormSQLRule(rule *semanticRule, _ *graph.Graph, index *semantic.Index, traces []dataflow.Trace) []language.PotentialFinding {
	var findings []language.PotentialFinding
	tracedSinks := map[string]bool{}
	for _, trace := range traces {
		if !sqlSink(trace.Sink.Operation) || trace.Sink.Metadata["database"] != "gorm" {
			continue
		}
		tracedSinks[trace.Sink.ID] = true
		if trace.Source.Metadata["framework"] == "gin" || trace.Source.Metadata["framework"] == "fiber" {
			continue
		}
		findings = append(findings, traceFinding(rule, trace, "Tainted input reaches GORM dynamic SQL"))
	}
	for _, sink := range index.Facts(semantic.FactSink) {
		if !sqlSink(sink.Operation) || sink.Metadata["database"] != "gorm" || tracedSinks[sink.ID] {
			continue
		}
		contextRule := *rule
		contextRule.severity = "Info"
		findings = append(findings, factFinding(&contextRule, sink, "Dynamic SQL passed to GORM", "GORM receives a dynamic SQL expression; no user-controlled source was proven.", 0.50, "heuristic"))
	}
	return findings
}

func gormIgnoredErrorRule(rule *semanticRule, _ *graph.Graph, index *semantic.Index, _ []dataflow.Trace) []language.PotentialFinding {
	checked := map[string]bool{}
	for _, fact := range index.Facts(semantic.FactGuard) {
		if fact.Operation != "gorm.error.checked" {
			continue
		}
		for _, input := range fact.Inputs {
			checked[fact.Location.File+"\x00"+fact.Function+"\x00"+input] = true
		}
	}
	propagations := index.Facts(semantic.FactPropagation)
	var findings []language.PotentialFinding
	for _, operation := range index.Facts(semantic.FactDatabaseOperation) {
		method := lastOperationSelector(operation.Operation)
		if operation.Metadata["database"] != "gorm" || operation.Metadata["error_checked_inline"] == "true" || !gormTerminalOperation(method) {
			continue
		}
		result := ""
		for _, propagation := range propagations {
			if propagation.Metadata["database"] == "gorm" && propagation.Location.File == operation.Location.File && propagation.Function == operation.Function && propagation.Location.StartLine == operation.Location.StartLine && strings.EqualFold(lastOperationSelector(propagation.Operation), method) && len(propagation.Outputs) == 1 {
				result = propagation.Outputs[0]
				break
			}
		}
		if result != "" && checked[operation.Location.File+"\x00"+operation.Function+"\x00"+result] {
			continue
		}
		findings = append(findings, factFinding(rule, operation, "GORM result error is not checked", "A terminal GORM operation has no visible inline or assigned-result Error check.", 0.80, "moderate"))
	}
	return findings
}

func lastOperationSelector(operation string) string {
	if index := strings.LastIndex(operation, "."); index >= 0 {
		return operation[index+1:]
	}
	return operation
}

func gormTerminalOperation(method string) bool {
	switch strings.ToLower(method) {
	case "exec", "first", "find", "take", "last", "scan", "create", "save", "delete", "updates":
		return true
	default:
		return false
	}
}

func sqlSink(operation string) bool {
	return operation == "sql.raw" || operation == "sql.exec" || operation == "gorm.where.dynamic" || strings.HasPrefix(operation, "database/sql.")
}
func commandSink(operation string) bool  { return operation == "command.exec" }
func redirectSink(operation string) bool { return operation == "http.redirect" }
func fileSink(operation string) bool {
	return operation == "file.open" || operation == "file.read" || operation == "file.write"
}

func expensiveCall(operation string) bool {
	switch operation {
	case "regexp.Compile", "regexp.MustCompile", "json.Marshal", "json.Unmarshal", "time.After", "make":
		return true
	default:
		return false
	}
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
