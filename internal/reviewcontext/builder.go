package reviewcontext

import (
	"fmt"
	"sort"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
)

type Builder struct {
	graph  *graph.Graph
	limits Limits
}

func NewBuilder(g *graph.Graph, limits Limits) *Builder {
	defaults := DefaultLimits()
	if limits.MaxSourceLines <= 0 {
		limits.MaxSourceLines = defaults.MaxSourceLines
	}
	if limits.MaxSymbols <= 0 {
		limits.MaxSymbols = defaults.MaxSymbols
	}
	if limits.MaxCallers <= 0 {
		limits.MaxCallers = defaults.MaxCallers
	}
	if limits.MaxCallees <= 0 {
		limits.MaxCallees = defaults.MaxCallees
	}
	if limits.MaxGraphDepth <= 0 {
		limits.MaxGraphDepth = defaults.MaxGraphDepth
	}
	if limits.MaxProvenanceSteps <= 0 {
		limits.MaxProvenanceSteps = defaults.MaxProvenanceSteps
	}
	if limits.MaxEvidenceItems <= 0 {
		limits.MaxEvidenceItems = defaults.MaxEvidenceItems
	}
	if limits.MaxApproxTokens <= 0 {
		limits.MaxApproxTokens = defaults.MaxApproxTokens
	}
	return &Builder{graph: g, limits: limits}
}

func (builder *Builder) Build(item finding.Finding) ReviewPacket {
	packet := ReviewPacket{Candidate: finding.PotentialFinding{Finding: item, Observation: item.Observation, Hypothesis: item.Hypothesis, ReviewPolicy: item.ReviewPolicy}, PrimaryLocation: cloneLocation(item.Location), Sufficiency: SufficiencyIncomplete}
	if builder == nil || builder.graph == nil || item.Location == nil || item.Location.File == "" {
		packet.Sufficiency = SufficiencyInsufficient
		packet.MissingInformation = []string{"primary source location unavailable"}
		return finalize(packet, builderLimits(builder))
	}
	document := findDocument(builder.graph.Documents(), item.Location.File)
	if document == nil || len(document.Source) == 0 {
		packet.Sufficiency = SufficiencyInsufficient
		packet.MissingInformation = []string{"primary source document unavailable"}
		return finalize(packet, builder.limits)
	}

	lines := strings.Split(string(document.Source), "\n")
	if item.Location.StartLine <= 0 || item.Location.StartLine > len(lines) {
		packet.Sufficiency = SufficiencyInsufficient
		packet.MissingInformation = []string{"primary source location is outside the semantic document"}
		return finalize(packet, builder.limits)
	}
	primary := excerpt(lines, item.Location.StartLine, item.Location.EndLine, min(builder.limits.MaxSourceLines, 12))
	if primary == "" {
		packet.Sufficiency = SufficiencyInsufficient
		packet.MissingInformation = []string{"primary source evidence unavailable"}
		return finalize(packet, builder.limits)
	}
	packet.Evidence = append(packet.Evidence, EvidenceItem{ID: "source.primary", Kind: EvidencePrimary, Location: cloneLocation(item.Location), Content: primary, Required: true})
	sourceLinesUsed := lineCount(primary)
	criticalMissing := appendCandidateEvidence(&packet, item.Evidence.Steps)

	function := enclosingFunction(document.Functions, item.Location.StartLine)
	if function != nil {
		packet.EnclosingSymbol = function.Name
		used, missing := appendHypothesisEvidence(&packet, builder.graph, item, document, function, lines, max(0, builder.limits.MaxSourceLines-sourceLinesUsed))
		sourceLinesUsed += used
		criticalMissing = missing || criticalMissing
		content := excerpt(lines, function.Location.StartLine, function.Location.EndLine, max(0, builder.limits.MaxSourceLines-sourceLinesUsed))
		if content != "" {
			packet.Evidence = append(packet.Evidence, EvidenceItem{ID: "source.enclosing_function", Kind: EvidenceEnclosing, Location: semanticLocation(function.Location), Symbol: function.Name, Content: content})
		}
	} else {
		packet.MissingInformation = append(packet.MissingInformation, "enclosing function unavailable")
		if hypothesisPremiseRule(item.RuleID) {
			packet.MissingInformation = appendUnique(packet.MissingInformation, "hypothesis-critical enclosing source unavailable")
			criticalMissing = true
		}
	}
	relevantFacts := selectFacts(document.Facts, item, function)
	for _, evidence := range dataflowEvidence(builder.graph, item, function, builder.limits.MaxProvenanceSteps) {
		evidence.Required = true
		packet.Provenance = append(packet.Provenance, evidence)
		packet.Evidence = append(packet.Evidence, evidence)
	}
	for _, fact := range relevantFacts {
		kind := evidenceKind(fact.Kind)
		evidence := EvidenceItem{Kind: kind, Location: semanticLocation(fact.Location), Symbol: fact.Function, Content: fact.Expression, FactID: fact.ID}
		evidence.Required = factDirectlySupportsCandidate(fact, item)
		if evidence.Content == "" {
			evidence.Content = fact.Operation
		}
		if bound := fact.Metadata["loop_bound"]; bound != "" {
			evidence.Content += "\nloop_bound=" + bound
			if expression := fact.Metadata["loop_expression"]; expression != "" {
				evidence.Content += "\nloop_expression=" + expression
			}
		}
		if evidence.Required {
			evidence.Retention = RetentionCritical
		}
		packet.Evidence = append(packet.Evidence, evidence)
		if kind == EvidenceProvenance && len(packet.Provenance) < builder.limits.MaxProvenanceSteps && !containsFact(packet.Provenance, fact.ID) {
			packet.Provenance = append(packet.Provenance, evidence)
		}
	}
	criticalMissing = validateRequiredEvidence(&packet, item.RuleID) || criticalMissing

	node := enclosingNode(builder.graph, item.Location.File, item.Location.StartLine)
	if node != nil {
		packet.Callers = graphRelations(builder.graph, node.ID, false, builder.limits.MaxGraphDepth, builder.limits.MaxCallers)
		packet.Callees = graphRelations(builder.graph, node.ID, true, builder.limits.MaxGraphDepth, builder.limits.MaxCallees)
		packet.Evidence = append(packet.Evidence, packet.Callers...)
		packet.Evidence = append(packet.Evidence, packet.Callees...)
	}
	packet.Evidence = append(packet.Evidence, symbolEvidence(builder.graph, item.Evidence.AffectedSymbols, builder.limits.MaxSymbols)...)
	packet.Evidence = append(packet.Evidence, adjacentComments(item.Location.File, lines, item.Location.StartLine)...)
	for _, ecosystem := range semantic.EcosystemsForImports(document.Imports) {
		packet.Evidence = append(packet.Evidence, EvidenceItem{Kind: EvidenceEcosystem, Content: string(ecosystem)})
	}
	packet.Sufficiency = SufficiencySufficient
	if criticalMissing {
		packet.Sufficiency = SufficiencyInsufficient
	} else if len(packet.MissingInformation) > 0 {
		packet.Sufficiency = SufficiencyIncomplete
	}
	assignIDs(packet.Evidence)
	assignIDs(packet.Provenance)
	assignIDs(packet.Callers)
	assignIDs(packet.Callees)
	return finalize(packet, builder.limits)
}

func hypothesisPremiseRule(ruleID string) bool {
	switch ruleID {
	case "PY-COR-001", "PY-SEC-001", "PY-SEC-002", "RUST-COR-001":
		return true
	default:
		return false
	}
}

func appendHypothesisEvidence(packet *ReviewPacket, g *graph.Graph, item finding.Finding, document *semantic.Document, function *semantic.Function, lines []string, budget int) (int, bool) {
	used := 0
	missing := false
	appendRequired := func(id string, kind EvidenceKind, start, end int, message string) bool {
		content := excerpt(lines, start, end, budget-used)
		if content == "" || lineCount(content) < end-start+1 {
			packet.MissingInformation = appendUnique(packet.MissingInformation, message)
			missing = true
			return false
		}
		packet.Evidence = append(packet.Evidence, EvidenceItem{ID: id, Kind: kind, Location: &finding.Location{File: document.Path, StartLine: start, EndLine: end}, Symbol: function.Name, Content: content, Required: true, Retention: RetentionCritical})
		used += lineCount(content)
		return true
	}

	switch item.RuleID {
	case "PY-COR-001":
		start := function.Location.StartLine - 1
		for start >= 1 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "@") {
			start--
		}
		start++
		if start >= function.Location.StartLine {
			packet.MissingInformation = appendUnique(packet.MissingInformation, "decorator or wrapper premise unavailable")
			return used, true
		}
		end := min(len(lines), function.Location.EndLine+6)
		appendRequired("hypothesis.decorator", EvidenceGuard, start, end, "decorator or wrapper premise unavailable")
	case "PY-SEC-001", "PY-SEC-002":
		entryEnd := min(function.Location.EndLine, function.Location.StartLine+24)
		appendRequired("hypothesis.source_derivation", EvidenceProvenance, function.Location.StartLine, entryEnd, "dynamic-execution source derivation unavailable")
		guardStart := max(function.Location.StartLine, item.Location.StartLine-12)
		appendRequired("hypothesis.execution_guard", EvidenceGuard, guardStart, item.Location.EndLine, "dynamic-execution selection guard unavailable")
	case "RUST-COR-001":
		signature := strings.TrimSpace(lines[function.Location.StartLine-1])
		if strings.Contains(signature, "unsafe fn ") {
			start := contiguousRustContractStart(lines, function.Location.StartLine)
			if start == function.Location.StartLine {
				packet.MissingInformation = appendUnique(packet.MissingInformation, "unsafe function safety contract unavailable")
				missing = true
			} else {
				appendRequired("hypothesis.safety_contract", EvidenceComment, start, function.Location.StartLine-1, "unsafe function safety contract unavailable")
			}
			appendRequired("hypothesis.unsafe_function", EvidenceEnclosing, function.Location.StartLine, function.Location.EndLine, "unsafe function body unavailable")
			break
		}
		guardStart := max(function.Location.StartLine, item.Location.StartLine-12)
		appendRequired("hypothesis.caller_guard", EvidenceGuard, guardStart, item.Location.EndLine, "unsafe caller guard unavailable")
		callee, calleeDocument := resolvedUnsafeCallee(g, item.Location.File, item.Location.StartLine)
		if callee == nil || calleeDocument == nil {
			packet.MissingInformation = appendUnique(packet.MissingInformation, "unsafe callee safety premise unavailable")
			missing = true
			break
		}
		calleeLines := strings.Split(string(calleeDocument.Source), "\n")
		start := contiguousRustContractStart(calleeLines, callee.StartLine)
		content := excerpt(calleeLines, start, callee.EndLine, budget-used)
		if content == "" || lineCount(content) < callee.EndLine-start+1 {
			packet.MissingInformation = appendUnique(packet.MissingInformation, "unsafe callee safety premise unavailable")
			missing = true
			break
		}
		packet.Evidence = append(packet.Evidence, EvidenceItem{ID: "hypothesis.callee_safety", Kind: EvidenceCallee, Location: &finding.Location{File: callee.File, StartLine: start, EndLine: callee.EndLine}, Symbol: callee.Name, Content: content, Required: true, Retention: RetentionCritical})
		used += lineCount(content)
	}
	return used, missing
}

func contiguousRustContractStart(lines []string, functionStart int) int {
	start := functionStart
	for index := functionStart - 1; index >= 1; index-- {
		text := strings.TrimSpace(lines[index-1])
		if text == "" || strings.HasPrefix(text, "///") || strings.HasPrefix(text, "//!") || strings.HasPrefix(text, "#[") {
			start = index
			continue
		}
		break
	}
	return start
}

func resolvedUnsafeCallee(g *graph.Graph, file string, line int) (*graph.Node, *semantic.Document) {
	caller := enclosingNode(g, file, line)
	if caller == nil {
		return nil, nil
	}
	g.Mu().RLock()
	var matches []*graph.Node
	for _, edge := range g.AdjList[caller.ID] {
		if edge.Kind != graph.EdgeCalls {
			continue
		}
		node := g.Nodes[edge.To]
		if node == nil {
			continue
		}
		body, _ := node.Metadata["Body"].(string)
		if strings.Contains(body, "unsafe fn ") {
			matches = append(matches, node)
		}
	}
	g.Mu().RUnlock()
	if len(matches) != 1 {
		return nil, nil
	}
	return matches[0], findDocument(g.Documents(), matches[0].File)
}

func validateRequiredEvidence(packet *ReviewPacket, ruleID string) bool {
	byID := make(map[string]EvidenceItem, len(packet.Evidence))
	for _, item := range packet.Evidence {
		byID[item.ID] = item
	}
	missing := func(id, message string) bool {
		item, ok := byID[id]
		if ok && strings.TrimSpace(item.Content) != "" {
			return false
		}
		packet.MissingInformation = appendUnique(packet.MissingInformation, message)
		return true
	}
	switch ruleID {
	case "GO-COR-004":
		iterationMissing := missing("source.iteration", "scanner iteration evidence unavailable")
		postUseMissing := missing("source.post_use", "complete post-iteration region unavailable")
		return iterationMissing || postUseMissing
	case "ARC-001":
		importMissing := missing("architecture.import", "direct import evidence unavailable")
		basisMissing := missing("architecture.basis", "architecture layer-classification basis unavailable")
		criticalMissing := importMissing || basisMissing
		basis := byID["architecture.basis"].Content
		if !trustedArchitectureBasis(basis) {
			packet.MissingInformation = appendUnique(packet.MissingInformation, "repository architecture policy unavailable or heuristic-only")
			criticalMissing = true
		}
		return criticalMissing
	case "PY-SEC-001", "PY-SEC-002":
		derivationMissing := missing("hypothesis.source_derivation", "dynamic-execution source derivation unavailable")
		guardMissing := missing("hypothesis.execution_guard", "dynamic-execution selection guard unavailable")
		trustMissing := true
		for _, item := range packet.Evidence {
			if item.Kind == EvidenceProvenance && strings.HasPrefix(item.Content, "dataflow source:") {
				trustMissing = false
				break
			}
		}
		if trustMissing {
			packet.MissingInformation = appendUnique(packet.MissingInformation, "dynamic-execution trust premise unavailable")
		}
		return derivationMissing || guardMissing || trustMissing
	default:
		return false
	}
}

func trustedArchitectureBasis(content string) bool {
	fields := map[string]string{}
	for _, field := range strings.Fields(content) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) == 2 {
			fields[parts[0]] = parts[1]
		}
	}
	origin := fields["origin"]
	return (origin == "user_configured_policy" || origin == "explicit_repository_policy") && strings.TrimSpace(fields["basis"]) != ""
}

func appendCandidateEvidence(packet *ReviewPacket, steps []finding.EvidenceStep) bool {
	criticalMissing := false
	for _, step := range steps {
		item := EvidenceItem{Location: &finding.Location{File: step.File, StartLine: step.Line, EndLine: step.Line}, Content: step.Expression, Required: true, Retention: RetentionCritical}
		if item.Content == "" {
			item.Content = step.Message
		}
		switch step.Kind {
		case "iteration":
			item.ID, item.Kind = "source.iteration", EvidenceLifecycle
		case "post_use":
			item.ID, item.Kind = "source.post_use", EvidenceLifecycle
			if item.Location != nil && item.Content != "" {
				item.Location.EndLine = item.Location.StartLine + lineCount(item.Content) - 1
			}
		case "architecture_import":
			item.ID, item.Kind = "architecture.import", EvidencePrimary
		case "architecture_basis":
			item.ID, item.Kind = "architecture.basis", EvidenceProvenance
		case "architecture_policy_missing", "hypothesis_evidence_missing":
			criticalMissing = true
			packet.MissingInformation = appendUnique(packet.MissingInformation, step.Message)
			continue
		default:
			continue
		}
		if step.File == "" {
			item.Location = nil
		}
		packet.Evidence = append(packet.Evidence, item)
	}
	return criticalMissing
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if value != "" {
		values = append(values, value)
	}
	return values
}

func builderLimits(builder *Builder) Limits {
	if builder == nil {
		return DefaultLimits()
	}
	return builder.limits
}

func finalize(packet ReviewPacket, limits Limits) ReviewPacket {
	original := packet
	if limits.MaxEvidenceItems > 0 && len(packet.Evidence) > limits.MaxEvidenceItems {
		for len(packet.Evidence) > limits.MaxEvidenceItems {
			var removed bool
			packet, removed = RemoveLowestPriorityEvidence(packet)
			if !removed {
				break
			}
		}
	}
	for {
		packet = RecalculateMetadata(packet)
		if packet.Metadata.EstimatedInputTokens <= limits.MaxApproxTokens {
			break
		}
		if len(packet.Evidence) > 1 {
			var removed bool
			packet, removed = RemoveLowestPriorityEvidence(packet)
			if !removed {
				break
			}
		} else if len(packet.Evidence) == 1 && len(packet.Evidence[0].Content) > 32 {
			packet.Evidence[0].Content = packet.Evidence[0].Content[:len(packet.Evidence[0].Content)*3/4]
		} else {
			// Candidate identity, observation, and hypothesis are structurally
			// required; an exceptionally large candidate is explicitly incomplete.
			packet.Sufficiency = SufficiencyInsufficient
			packet.MissingInformation = append(packet.MissingInformation, "context token limit reached")
			break
		}
		packet.Metadata.Truncated = true
	}
	packet = ReevaluateSufficiency(original, packet)
	packet = RecalculateMetadata(packet)
	synchronizeIndexes(&packet, limits)
	return packet
}

func synchronizeIndexes(packet *ReviewPacket, limits Limits) {
	packet.Provenance = nil
	packet.Callers = nil
	packet.Callees = nil
	for _, item := range packet.Evidence {
		switch item.Kind {
		case EvidenceProvenance:
			if len(packet.Provenance) < limits.MaxProvenanceSteps {
				packet.Provenance = append(packet.Provenance, item)
			}
		case EvidenceCaller:
			if len(packet.Callers) < limits.MaxCallers {
				packet.Callers = append(packet.Callers, item)
			}
		case EvidenceCallee:
			if len(packet.Callees) < limits.MaxCallees {
				packet.Callees = append(packet.Callees, item)
			}
		}
	}
}

func findDocument(documents []*semantic.Document, file string) *semantic.Document {
	for _, document := range documents {
		if document != nil && document.Path == file {
			return document
		}
	}
	return nil
}
func cloneLocation(location *finding.Location) *finding.Location {
	if location == nil {
		return nil
	}
	copy := *location
	return &copy
}
func semanticLocation(location semantic.Location) *finding.Location {
	return &finding.Location{File: location.File, StartLine: location.StartLine, EndLine: location.EndLine}
}

func enclosingFunction(functions []semantic.Function, line int) *semantic.Function {
	var matches []semantic.Function
	for _, function := range functions {
		if function.Location.StartLine <= line && (function.Location.EndLine == 0 || line <= function.Location.EndLine) {
			matches = append(matches, function)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Location.StartLine != matches[j].Location.StartLine {
			return matches[i].Location.StartLine > matches[j].Location.StartLine
		}
		return matches[i].Name < matches[j].Name
	})
	if len(matches) == 0 {
		return nil
	}
	return &matches[0]
}

func excerpt(lines []string, start, end, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	if end-start+1 > maximum {
		end = start + maximum - 1
	}
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func dataflowEvidence(g *graph.Graph, item finding.Finding, function *semantic.Function, maximum int) []EvidenceItem {
	if item.Location == nil || maximum <= 0 {
		return nil
	}
	var out []EvidenceItem
	for _, trace := range g.DataflowTraces() {
		if trace.Sink.Location.File != item.Location.File {
			continue
		}
		if function != nil && trace.Sink.Function != "" && trace.Sink.Function != function.Name {
			continue
		}
		facts := append([]semantic.Fact{trace.Source}, trace.Propagation...)
		facts = append(facts, trace.Sink)
		if !traceRelatesToCandidate(facts, item) {
			continue
		}
		for index, fact := range facts {
			if len(out) >= maximum || containsFact(out, fact.ID) {
				break
			}
			role := "dataflow propagation"
			if index == 0 {
				role = "dataflow source"
			} else if index == len(facts)-1 {
				role = "dataflow sink"
			}
			content := fact.Expression
			if content == "" {
				content = fact.Operation
			}
			out = append(out, EvidenceItem{Kind: EvidenceProvenance, Location: semanticLocation(fact.Location), Symbol: fact.Function, Content: role + ": " + content, FactID: fact.ID})
		}
		if len(out) >= maximum {
			break
		}
	}
	return out
}

func traceRelatesToCandidate(facts []semantic.Fact, item finding.Finding) bool {
	symbols := map[string]bool{}
	for _, symbol := range item.Evidence.AffectedSymbols {
		symbols[symbol] = true
	}
	expressions := map[string]bool{}
	for _, step := range item.Evidence.Steps {
		if step.Expression != "" {
			expressions[step.Expression] = true
		}
	}
	for _, fact := range facts {
		if item.Location != nil && fact.Location.StartLine >= item.Location.StartLine-2 && fact.Location.StartLine <= item.Location.EndLine+2 {
			return true
		}
		if expressions[fact.Expression] {
			return true
		}
		for _, value := range append(append([]string(nil), fact.Inputs...), fact.Outputs...) {
			if symbols[value] {
				return true
			}
		}
	}
	return false
}

func factDirectlySupportsCandidate(fact semantic.Fact, item finding.Finding) bool {
	if item.Location != nil && fact.Location.StartLine >= item.Location.StartLine-1 && fact.Location.StartLine <= item.Location.EndLine+1 {
		return true
	}
	for _, step := range item.Evidence.Steps {
		if step.Expression != "" && step.Expression == fact.Expression {
			return true
		}
	}
	return false
}

func containsFact(items []EvidenceItem, factID string) bool {
	if factID == "" {
		return false
	}
	for _, item := range items {
		if item.FactID == factID {
			return true
		}
	}
	return false
}

func selectFacts(facts []semantic.Fact, item finding.Finding, function *semantic.Function) []semantic.Fact {
	var selected []semantic.Fact
	for _, fact := range facts {
		if item.Location == nil || fact.Location.File != item.Location.File {
			continue
		}
		if function != nil && fact.Function != "" && fact.Function != function.Name {
			continue
		}
		selected = append(selected, fact)
	}
	return selected
}

func evidenceKind(kind semantic.FactKind) EvidenceKind {
	switch kind {
	case semantic.FactGuard, semantic.FactSanitizer:
		return EvidenceGuard
	case semantic.FactPropagation, semantic.FactSource, semantic.FactSink, semantic.FactReturn:
		return EvidenceProvenance
	case semantic.FactResourceAcquire, semantic.FactResourceRelease, semantic.FactConcurrencyOperation:
		return EvidenceLifecycle
	default:
		return EvidenceSymbol
	}
}

func enclosingNode(g *graph.Graph, file string, line int) *graph.Node {
	g.Mu().RLock()
	defer g.Mu().RUnlock()
	var nodes []*graph.Node
	for _, node := range g.Nodes {
		if (node.Kind == graph.NodeFunction || node.Kind == graph.NodeMethod) && node.File == file && node.StartLine <= line && (node.EndLine == 0 || line <= node.EndLine) {
			nodes = append(nodes, node)
		}
	}
	sortNodes(nodes)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func graphRelations(g *graph.Graph, start graph.NodeID, outgoing bool, depth, maximum int) []EvidenceItem {
	g.Mu().RLock()
	defer g.Mu().RUnlock()
	type state struct {
		id    graph.NodeID
		depth int
	}
	queue := []state{{start, 0}}
	visited := map[graph.NodeID]bool{start: true}
	var nodes []*graph.Node
	for len(queue) > 0 && len(nodes) < maximum {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= depth {
			continue
		}
		edges := g.AdjList[current.id]
		if !outgoing {
			edges = g.RevAdj[current.id]
		}
		edges = append([]*graph.Edge(nil), edges...)
		sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
		for _, edge := range edges {
			if edge.Kind != graph.EdgeCalls {
				continue
			}
			next := edge.To
			if !outgoing {
				next = edge.From
			}
			if visited[next] {
				continue
			}
			visited[next] = true
			if node := g.Nodes[next]; node != nil {
				nodes = append(nodes, node)
				queue = append(queue, state{next, current.depth + 1})
			}
			if len(nodes) >= maximum {
				break
			}
		}
	}
	sortNodes(nodes)
	result := make([]EvidenceItem, 0, len(nodes))
	kind := EvidenceCallee
	if !outgoing {
		kind = EvidenceCaller
	}
	for _, node := range nodes {
		result = append(result, EvidenceItem{Kind: kind, Location: &finding.Location{File: node.File, StartLine: node.StartLine, EndLine: node.EndLine}, Symbol: node.Name, Content: fmt.Sprintf("%s %s", node.Kind, node.Name)})
	}
	assignIDs(result)
	return result
}

func sortNodes(nodes []*graph.Node) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].File != nodes[j].File {
			return nodes[i].File < nodes[j].File
		}
		if nodes[i].StartLine != nodes[j].StartLine {
			return nodes[i].StartLine < nodes[j].StartLine
		}
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func symbolEvidence(g *graph.Graph, names []string, maximum int) []EvidenceItem {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	g.Mu().RLock()
	var nodes []*graph.Node
	for _, node := range g.Nodes {
		if wanted[node.Name] {
			nodes = append(nodes, node)
		}
	}
	g.Mu().RUnlock()
	sortNodes(nodes)
	if len(nodes) > maximum {
		nodes = nodes[:maximum]
	}
	out := make([]EvidenceItem, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, EvidenceItem{Kind: EvidenceSymbol, Location: &finding.Location{File: node.File, StartLine: node.StartLine, EndLine: node.EndLine}, Symbol: node.Name, Content: node.Kind.String() + " definition"})
	}
	return out
}

func adjacentComments(file string, lines []string, line int) []EvidenceItem {
	start := max(1, line-3)
	end := min(len(lines), line+1)
	var out []EvidenceItem
	for index := start; index <= end; index++ {
		text := strings.TrimSpace(lines[index-1])
		if strings.HasPrefix(text, "//") || strings.HasPrefix(text, "#") || strings.Contains(strings.ToLower(text), "nolint") {
			out = append(out, EvidenceItem{Kind: EvidenceComment, Location: &finding.Location{File: file, StartLine: index, EndLine: index}, Content: text, Required: true})
		}
	}
	return out
}

func assignIDs(items []EvidenceItem) {
	counts := map[EvidenceKind]int{}
	for i := range items {
		if items[i].ID != "" {
			continue
		}
		counts[items[i].Kind]++
		items[i].ID = fmt.Sprintf("%s.%d", items[i].Kind, counts[items[i].Kind])
	}
}
