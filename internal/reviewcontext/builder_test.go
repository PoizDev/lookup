package reviewcontext

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/semantic"
)

func TestBuilderIsDeterministicBoundedAndKeepsPrimaryEvidence(t *testing.T) {
	g := contextGraph()
	limits := Limits{MaxSourceLines: 8, MaxSymbols: 2, MaxCallers: 1, MaxCallees: 1, MaxGraphDepth: 2, MaxProvenanceSteps: 1, MaxEvidenceItems: 7, MaxApproxTokens: 600}
	builder := NewBuilder(g, limits)
	candidate := contextCandidate()

	first := builder.Build(candidate)
	second := builder.Build(candidate)
	if !reflect.DeepEqual(first, second) {
		left, _ := json.Marshal(first)
		right, _ := json.Marshal(second)
		t.Fatalf("packets differ:\n%s\n%s", left, right)
	}
	if len(first.Evidence) > limits.MaxEvidenceItems || first.Metadata.EstimatedInputTokens > limits.MaxApproxTokens {
		t.Fatalf("packet exceeds limits: %#v", first.Metadata)
	}
	seen := map[string]bool{}
	primary := false
	for _, item := range first.Evidence {
		if item.ID == "" || seen[item.ID] {
			t.Fatalf("unstable/duplicate evidence ID %q", item.ID)
		}
		seen[item.ID] = true
		if item.Kind == EvidencePrimary {
			primary = true
		}
	}
	if !primary {
		t.Fatalf("primary evidence was truncated: %#v", first.Evidence)
	}
	if len(first.Callers) > 1 || len(first.Callees) > 1 || len(first.Provenance) > 1 {
		t.Fatalf("relationship limits not enforced: callers=%d callees=%d provenance=%d", len(first.Callers), len(first.Callees), len(first.Provenance))
	}
}

func TestBuilderIncludesAdjacentCommentAndNolint(t *testing.T) {
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(contextCandidate())
	found := false
	for _, item := range packet.Evidence {
		if item.Kind == EvidenceComment && strings.Contains(item.Content, "nolint") {
			found = true
		}
	}
	if !found {
		t.Fatalf("adjacent nolint comment missing: %#v", packet.Evidence)
	}
}

func TestBuilderGraphCycleDoesNotEscapeDepthOrCountLimits(t *testing.T) {
	g := contextGraph()
	packet := NewBuilder(g, Limits{MaxSourceLines: 20, MaxSymbols: 4, MaxCallers: 1, MaxCallees: 1, MaxGraphDepth: 1, MaxProvenanceSteps: 2, MaxEvidenceItems: 20, MaxApproxTokens: 2000}).Build(contextCandidate())
	if len(packet.Callers) > 1 || len(packet.Callees) > 1 {
		t.Fatalf("cycle traversal escaped bounds: %#v", packet)
	}
}

func TestBuilderReusesCachedBoundedDataflowTrace(t *testing.T) {
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(contextCandidate())
	found := false
	for _, item := range packet.Provenance {
		if strings.Contains(item.Content, "dataflow source") {
			found = true
		}
	}
	if !found {
		t.Fatalf("dataflow trace evidence missing: %#v", packet.Provenance)
	}
}

func TestBuilderEnforcesGlobalSourceLineLimit(t *testing.T) {
	g := contextGraph()
	packet := NewBuilder(g, Limits{MaxSourceLines: 4, MaxEvidenceItems: 32, MaxApproxTokens: 3000}).Build(contextCandidate())
	lines := 0
	for _, item := range packet.Evidence {
		if item.Kind == EvidencePrimary || item.Kind == EvidenceEnclosing {
			lines += lineCount(item.Content)
		}
	}
	if lines > 4 {
		t.Fatalf("source evidence used %d lines, limit is 4", lines)
	}
}

func TestBuilderMissingPrimaryContextIsInsufficient(t *testing.T) {
	packet := NewBuilder(graph.NewGraph(), DefaultLimits()).Build(contextCandidate())
	if packet.Sufficiency != SufficiencyInsufficient || len(packet.MissingInformation) == 0 {
		t.Fatalf("packet = %#v, want explicit insufficiency", packet)
	}
}

func TestBuilderOutOfRangePrimaryLocationIsInsufficient(t *testing.T) {
	item := contextCandidate()
	item.Location.StartLine = 500
	item.Location.EndLine = 500
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	if packet.Sufficiency != SufficiencyInsufficient {
		t.Fatalf("out-of-range primary remained reviewable: %#v", packet)
	}
}

func TestBuilderNeverSendsPacketAboveConfiguredTokenLimit(t *testing.T) {
	item := contextCandidate()
	item.Observation = strings.Repeat("observation", 2000)
	item.Hypothesis = strings.Repeat("hypothesis", 2000)
	item.Evidence.CodeSnippet = strings.Repeat("legacy", 10000)
	packet := NewBuilder(contextGraph(), Limits{MaxSourceLines: 4, MaxEvidenceItems: 2, MaxApproxTokens: 64}).Build(item)
	if packet.Metadata.EstimatedInputTokens > 64 && packet.Sufficiency != SufficiencyInsufficient {
		t.Fatalf("oversized packet remained reviewable: %#v", packet.Metadata)
	}
	if packet.Candidate.Evidence.CodeSnippet != item.Evidence.CodeSnippet {
		t.Fatal("internal packet did not preserve the original candidate")
	}
}

func TestIncompletePacketWithRequiredEvidenceIntactIsReviewable(t *testing.T) {
	packet := ReviewPacket{
		Candidate:          finding.PotentialFinding{Finding: finding.Finding{ID: "partial"}, Observation: "observed", Hypothesis: "risk"},
		Evidence:           []EvidenceItem{{ID: "source.primary", Kind: EvidencePrimary, Content: "source", Required: true}},
		MissingInformation: []string{"callee context unavailable"},
		Sufficiency:        SufficiencyIncomplete,
	}
	if !packet.Reviewable() {
		t.Fatal("incomplete packet with intact required evidence was not reviewable")
	}
}

func TestReevaluateIncompletePacketFailsClosedAfterRequiredEvidenceLoss(t *testing.T) {
	original := ReviewPacket{
		Candidate:          finding.PotentialFinding{Finding: finding.Finding{ID: "partial"}, Observation: "observed", Hypothesis: "risk"},
		Evidence:           []EvidenceItem{{ID: "source.primary", Kind: EvidencePrimary, Content: "source", Required: true}, {ID: "required.guard", Kind: EvidenceGuard, Content: "guard", Required: true}},
		MissingInformation: []string{"callee context unavailable"},
		Sufficiency:        SufficiencyIncomplete,
	}
	transformed := original
	transformed.Evidence = transformed.Evidence[:1]
	got := ReevaluateSufficiency(original, transformed)
	if got.Sufficiency != SufficiencyInsufficient || got.Reviewable() {
		t.Fatalf("required evidence loss remained reviewable: %#v", got)
	}
}

func TestReevaluateOptionalEvidenceLossMakesMissingContextVisible(t *testing.T) {
	original := ReviewPacket{
		Candidate:   finding.PotentialFinding{Finding: finding.Finding{ID: "complete"}, Observation: "observed", Hypothesis: "risk"},
		Evidence:    []EvidenceItem{{ID: "source.primary", Kind: EvidencePrimary, Content: "source", Required: true}, {ID: "callee.optional", Kind: EvidenceCallee, Content: "callee"}},
		Sufficiency: SufficiencySufficient,
	}
	transformed := original
	transformed.Evidence = transformed.Evidence[:1]
	got := ReevaluateSufficiency(original, transformed)
	if got.Sufficiency != SufficiencyIncomplete || !got.Reviewable() {
		t.Fatalf("optional evidence loss did not produce reviewable incomplete packet: %#v", got)
	}
	if len(got.MissingInformation) == 0 {
		t.Fatal("optional evidence loss was hidden from the reviewer")
	}
}

func TestBuilderRetainsHypothesisCriticalCandidateEvidence(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "GO-COR-004"
	item.Evidence.Steps = []finding.EvidenceStep{
		{Kind: "iteration", File: "handler.go", Line: 7, Expression: "scanner.Scan()", Message: "iteration"},
		{Kind: "post_use", File: "handler.go", Line: 8, Expression: "cleanup()\nreturn output.Bytes()", Message: "complete post-loop region"},
	}
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	for _, id := range []string{"source.iteration", "source.post_use"} {
		evidence := evidenceByID(packet.Evidence, id)
		if evidence == nil || !evidence.Required || evidence.Retention != RetentionCritical {
			t.Fatalf("required evidence %q = %#v", id, evidence)
		}
	}
	postUse := evidenceByID(packet.Evidence, "source.post_use")
	if postUse.Location == nil || postUse.Location.StartLine != 8 || postUse.Location.EndLine != 9 {
		t.Fatalf("post-use evidence range = %#v", postUse.Location)
	}
}

func TestBuilderPostUseEvidenceExposesLaterScannerErrCheck(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "GO-COR-004"
	item.Evidence.Steps = []finding.EvidenceStep{
		{Kind: "iteration", File: "handler.go", Line: 7, Expression: "scanner.Scan()", Message: "iteration"},
		{Kind: "post_use", File: "handler.go", Line: 8, Expression: "if err := scanner.Err(); err != nil { return err }\nreturn nil", Message: "complete post-loop region"},
	}
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	postUse := evidenceByID(packet.Evidence, "source.post_use")
	if postUse == nil || !strings.Contains(postUse.Content, "scanner.Err()") || !postUse.Required {
		t.Fatalf("post-use counterevidence = %#v", postUse)
	}
}

func TestBuilderFailsClosedWhenCompletePostUseEvidenceCannotFit(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "GO-COR-004"
	item.Evidence.Steps = []finding.EvidenceStep{
		{Kind: "iteration", File: "handler.go", Line: 7, Expression: "scanner.Scan()", Message: "iteration"},
		{Kind: "post_use", File: "handler.go", Line: 8, Expression: strings.Repeat("post-loop source ", 2000), Message: "complete post-loop region"},
	}
	packet := NewBuilder(contextGraph(), Limits{MaxSourceLines: 12, MaxEvidenceItems: 32, MaxApproxTokens: 180}).Build(item)
	if packet.Sufficiency != SufficiencyInsufficient || packet.Reviewable() {
		t.Fatalf("packet without retained complete post-use evidence remained reviewable: %#v", packet)
	}
	if !containsString(packet.MissingInformation, "adjudication-critical evidence removed during context minimization") && !containsString(packet.MissingInformation, "context token limit reached") {
		t.Fatalf("required-evidence loss not reported: %#v", packet.MissingInformation)
	}
}

func TestBuilderFailsClosedWhenGO004RequiredStepWasNeverEmitted(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "GO-COR-004"
	item.Evidence.Steps = []finding.EvidenceStep{{Kind: "iteration", File: "handler.go", Line: 7, Expression: "scanner.Scan()"}}
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	if packet.Reviewable() || packet.Sufficiency != SufficiencyInsufficient || !containsString(packet.MissingInformation, "complete post-iteration region unavailable") {
		t.Fatalf("GO-COR-004 missing required step remained reviewable: %#v", packet)
	}
}

func TestBuilderFailsClosedForMissingBuiltInArchitecturePolicy(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "ARC-001"
	item.Evidence.Steps = []finding.EvidenceStep{
		{Kind: "architecture_basis", File: "handler.go", Line: 7, Expression: "source_keyword=/handler import_keyword=/db origin=built_in_heuristic", Message: "built-in path mapping"},
		{Kind: "architecture_policy_missing", Message: "repository architecture policy unavailable"},
	}
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	basis := evidenceByID(packet.Evidence, "architecture.basis")
	if basis == nil || !basis.Required || basis.Retention != RetentionCritical {
		t.Fatalf("architecture basis = %#v", basis)
	}
	if packet.Sufficiency != SufficiencyInsufficient || packet.Reviewable() {
		t.Fatalf("heuristic-only packet remained reviewable: %#v", packet)
	}
	if !containsString(packet.MissingInformation, "repository architecture policy unavailable") {
		t.Fatalf("missing information = %#v", packet.MissingInformation)
	}
}

func TestBuilderKeepsExplicitArchitecturePolicyReviewable(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "ARC-001"
	item.Evidence.Steps = []finding.EvidenceStep{
		{Kind: "architecture_import", File: "handler.go", Line: 7, Expression: `import "example.dev/persistence"`, Message: "direct import"},
		{Kind: "architecture_basis", File: "handler.go", Line: 7, Expression: "source_keyword=/ui/ import_keyword=/persistence origin=explicit_repository_policy basis=repository-manifest", Message: "explicit policy"},
	}
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	if !packet.Reviewable() || packet.Sufficiency == SufficiencyInsufficient {
		t.Fatalf("explicit policy packet is not reviewable: %#v", packet)
	}
	for _, id := range []string{"architecture.import", "architecture.basis"} {
		evidence := evidenceByID(packet.Evidence, id)
		if evidence == nil || !evidence.Required || evidence.Retention != RetentionCritical {
			t.Fatalf("explicit architecture evidence %q = %#v", id, evidence)
		}
	}
}

func TestBuilderFailsClosedWhenARC001RequiredStepWasNeverEmitted(t *testing.T) {
	item := contextCandidate()
	item.RuleID = "ARC-001"
	item.Evidence.Steps = []finding.EvidenceStep{{Kind: "architecture_import", File: "handler.go", Line: 7, Expression: `import "example.dev/db"`}}
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	if packet.Reviewable() || packet.Sufficiency != SufficiencyInsufficient || !containsString(packet.MissingInformation, "architecture layer-classification basis unavailable") {
		t.Fatalf("ARC-001 missing required basis remained reviewable: %#v", packet)
	}
}

func TestBuilderRequiresPythonDecoratorPremise(t *testing.T) {
	source := "@flow\nasync def bar(model={\"x\": 42}):\n    return model.x\nresult = await run_flow(bar)\nassert result.x == 42\n"
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "flow.py", Language: "Python", Source: []byte(source), Functions: []semantic.Function{{Name: "bar", Location: semantic.Location{File: "flow.py", StartLine: 2, EndLine: 3}}}})
	item := finding.Finding{ID: "py07", RuleID: "PY-COR-001", Observation: "mutable default", Hypothesis: "shared between calls", Location: &finding.Location{File: "flow.py", StartLine: 2, EndLine: 2}}
	packet := NewBuilder(g, DefaultLimits()).Build(item)
	premise := evidenceByID(packet.Evidence, "hypothesis.decorator")
	if premise == nil || !premise.Required || premise.Retention != RetentionCritical || !strings.Contains(premise.Content, "@flow") || !strings.Contains(premise.Content, "run_flow(bar)") || !strings.Contains(premise.Content, "assert result.x == 42") {
		t.Fatalf("decorator premise = %#v", premise)
	}
}

func TestBuilderRequiresPythonDynamicExecutionPremises(t *testing.T) {
	for _, tc := range []struct {
		name, rule, source, derivation, guard string
		line                                  int
	}{
		{name: "namespace derivation", rule: "PY-SEC-001", source: "def signature(source_code):\n    namespace = safe_load_namespace(source_code)\n    parsed = ast.parse(source_code)\n    annotation = parsed.body[0]\n    if annotation is not None:\n        code = compile(annotation, '<s>', 'eval')\n        return eval(code, namespace)\n", line: 7, derivation: "safe_load_namespace", guard: "compile"},
		{name: "source selection", rule: "PY-SEC-002", source: "def safe_load(source_code):\n    parsed = ast.parse(source_code)\n    selected = []\n    for node in parsed.body:\n        if isinstance(node, (ast.ClassDef, ast.FunctionDef)):\n            selected.append(node)\n    if selected:\n        code = compile(ast.Module(body=selected), '<ast>', 'exec')\n        exec(code, namespace)\n", line: 9, derivation: "ast.parse(source_code)", guard: "if selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := graph.NewGraph()
			lineCount := strings.Count(tc.source, "\n")
			g.AddDocument(&semantic.Document{Path: "dynamic.py", Language: "Python", Source: []byte(tc.source), Functions: []semantic.Function{{Name: "target", Location: semantic.Location{File: "dynamic.py", StartLine: 1, EndLine: lineCount}}}})
			item := finding.Finding{ID: tc.name, RuleID: tc.rule, Observation: "dynamic execution", Hypothesis: "input may be untrusted", Location: &finding.Location{File: "dynamic.py", StartLine: tc.line, EndLine: tc.line}}
			packet := NewBuilder(g, DefaultLimits()).Build(item)
			derivation := evidenceByID(packet.Evidence, "hypothesis.source_derivation")
			guard := evidenceByID(packet.Evidence, "hypothesis.execution_guard")
			if derivation == nil || !derivation.Required || !strings.Contains(derivation.Content, tc.derivation) {
				t.Fatalf("source derivation = %#v", derivation)
			}
			if guard == nil || !guard.Required || !strings.Contains(guard.Content, tc.guard) {
				t.Fatalf("execution guard = %#v", guard)
			}
			if packet.Reviewable() || packet.Sufficiency != SufficiencyInsufficient || !containsString(packet.MissingInformation, "dynamic-execution trust premise unavailable") {
				t.Fatalf("missing trust premise remained reviewable: %#v", packet)
			}
		})
	}
}

func TestBuilderRequiresRustSafetyContract(t *testing.T) {
	source := "/// # Warning\n/// returned reference is unbounded\n/// # Safety\n/// caller preserves lifetime\nunsafe fn map(mmap: &mut Mmap) -> &mut T {\n    if mmap.len() < size { panic!() }\n    let bytes = unsafe { from_raw_parts_mut(ptr, size) };\n    unsafe { &mut *bytes.as_mut_ptr().cast::<T>() }\n}\n"
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "mmap.rs", Language: "Rust", Source: []byte(source), Functions: []semantic.Function{{Name: "map", Location: semantic.Location{File: "mmap.rs", StartLine: 5, EndLine: 9}}}})
	item := finding.Finding{ID: "rs07", RuleID: "RUST-COR-001", Observation: "unsafe block", Hypothesis: "safety invariants may be violated", Location: &finding.Location{File: "mmap.rs", StartLine: 7, EndLine: 7}}
	packet := NewBuilder(g, DefaultLimits()).Build(item)
	contract := evidenceByID(packet.Evidence, "hypothesis.safety_contract")
	if contract == nil || !contract.Required || contract.Retention != RetentionCritical || !strings.Contains(contract.Content, "returned reference is unbounded") {
		t.Fatalf("safety contract = %#v", contract)
	}
}

func TestBuilderFailsClosedWithoutRustUnsafeCalleePremise(t *testing.T) {
	source := "fn similarity(v1: &[T], v2: &[T]) -> Score {\n    if feature_detected() && v1.len() >= MIN_DIM {\n        return unsafe { simd(v1, v2) };\n    }\n    scalar(v1, v2)\n}\n"
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "metric.rs", Language: "Rust", Source: []byte(source), Functions: []semantic.Function{{Name: "similarity", Location: semantic.Location{File: "metric.rs", StartLine: 1, EndLine: 6}}}})
	item := finding.Finding{ID: "rs09", RuleID: "RUST-COR-001", Observation: "unsafe block", Hypothesis: "safety invariants may be violated", Location: &finding.Location{File: "metric.rs", StartLine: 3, EndLine: 3}}
	packet := NewBuilder(g, DefaultLimits()).Build(item)
	guard := evidenceByID(packet.Evidence, "hypothesis.caller_guard")
	if guard == nil || !guard.Required || !strings.Contains(guard.Content, "v1.len()") {
		t.Fatalf("caller guard = %#v", guard)
	}
	if packet.Reviewable() || packet.Sufficiency != SufficiencyInsufficient || !containsString(packet.MissingInformation, "unsafe callee safety premise unavailable") {
		t.Fatalf("missing callee premise remained reviewable: %#v", packet)
	}
}

func TestBuilderFailsClosedWhenHypothesisPremiseCannotFit(t *testing.T) {
	source := "@flow\ndef bar(value={}):\n    return value\n"
	g := graph.NewGraph()
	g.AddDocument(&semantic.Document{Path: "flow.py", Language: "Python", Source: []byte(source), Functions: []semantic.Function{{Name: "bar", Location: semantic.Location{File: "flow.py", StartLine: 2, EndLine: 3}}}})
	item := finding.Finding{ID: "py07", RuleID: "PY-COR-001", Observation: "mutable default", Hypothesis: "shared", Location: &finding.Location{File: "flow.py", StartLine: 2, EndLine: 2}}
	packet := NewBuilder(g, Limits{MaxSourceLines: 1, MaxEvidenceItems: 32, MaxApproxTokens: 3000}).Build(item)
	if packet.Reviewable() || packet.Sufficiency != SufficiencyInsufficient {
		t.Fatalf("packet without room for decorator premise remained reviewable: %#v", packet)
	}
}

func TestBuilderRejectsMalformedArchitecturePolicyProvenance(t *testing.T) {
	for name, basis := range map[string]string{
		"empty basis":     "source_keyword=/ui/ import_keyword=/db origin=explicit_repository_policy basis=",
		"unknown origin":  "source_keyword=/ui/ import_keyword=/db origin=unknown basis=manifest",
		"embedded origin": "source_keyword=/ui/ import_keyword=/db origin=unknown basis=origin=explicit_repository_policy",
	} {
		t.Run(name, func(t *testing.T) {
			item := contextCandidate()
			item.RuleID = "ARC-001"
			item.Evidence.Steps = []finding.EvidenceStep{
				{Kind: "architecture_import", File: "handler.go", Line: 7, Expression: `import "example.dev/db"`},
				{Kind: "architecture_basis", File: "handler.go", Line: 7, Expression: basis},
			}
			packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
			if packet.Reviewable() || packet.Sufficiency != SufficiencyInsufficient {
				t.Fatalf("malformed architecture provenance remained reviewable: %#v", packet)
			}
		})
	}
}

func TestArchitectureBasisRemovalCannotRemainReviewable(t *testing.T) {
	original := ReviewPacket{
		Candidate: finding.PotentialFinding{Finding: finding.Finding{ID: "arc", RuleID: "ARC-001"}, Observation: "import exists", Hypothesis: "boundary may be violated"},
		Evidence: []EvidenceItem{
			{ID: "source.primary", Kind: EvidencePrimary, Content: `import "example.dev/db"`, Required: true},
			{ID: "architecture.basis", Kind: EvidenceProvenance, Content: "origin=explicit_repository_policy", Required: true, Retention: RetentionCritical},
		},
		Sufficiency: SufficiencySufficient,
	}
	transformed := original
	transformed.Evidence = transformed.Evidence[:1]
	got := ReevaluateSufficiency(original, transformed)
	if got.Reviewable() || got.Sufficiency != SufficiencyInsufficient {
		t.Fatalf("packet without required architecture basis remained reviewable: %#v", got)
	}
}

func evidenceByID(items []EvidenceItem, id string) *EvidenceItem {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDefaultBuilderBoundDoesNotImposeSmallProviderWindow(t *testing.T) {
	item := contextCandidate()
	item.Observation = strings.Repeat("observation ", 800)
	item.Hypothesis = strings.Repeat("hypothesis ", 800)
	packet := NewBuilder(contextGraph(), DefaultLimits()).Build(item)
	if packet.Metadata.EstimatedInputTokens <= 3000 {
		t.Fatalf("fixture did not exceed the former builder ceiling: %#v", packet.Metadata)
	}
	if !packet.Reviewable() {
		t.Fatalf("canonical packet was destroyed before provider planning: state=%s missing=%v", packet.Sufficiency, packet.MissingInformation)
	}
}

func TestProviderViewPreservesSemanticClaimsWithoutSilentTruncation(t *testing.T) {
	observation := strings.Repeat("deterministic observation ", 200)
	hypothesis := strings.Repeat("contextual hypothesis ", 200)
	packet := ReviewPacket{
		Candidate:   finding.PotentialFinding{Finding: finding.Finding{ID: "f", RuleID: "R"}, Observation: observation, Hypothesis: hypothesis},
		Sufficiency: SufficiencySufficient,
	}
	view := ProviderView(packet)
	if view.Candidate.Observation != observation || view.Candidate.Hypothesis != hypothesis {
		t.Fatalf("provider view changed semantic claims: observation=%d/%d hypothesis=%d/%d", len(view.Candidate.Observation), len(observation), len(view.Candidate.Hypothesis), len(hypothesis))
	}
	if view.Sufficiency != SufficiencySufficient {
		t.Fatalf("lossless provider view changed sufficiency: %s", view.Sufficiency)
	}
}

func contextCandidate() finding.Finding {
	return finding.Finding{ID: "f-1", RuleID: "GO-COR-002", Severity: finding.SeverityHigh, Confidence: .8, Observation: "A single-value type assertion exists.", Hypothesis: "The assertion may panic.", Location: &finding.Location{File: "handler.go", StartLine: 5, EndLine: 5}, Evidence: finding.Evidence{AffectedSymbols: []string{"v"}, Steps: []finding.EvidenceStep{{Kind: "evidence", File: "handler.go", Line: 5, Expression: "v := input.(T)", Message: "assertion"}}}}
}

func contextGraph() *graph.Graph {
	g := graph.NewGraph()
	source := "package p\n\nfunc caller() { handler() }\n\n// invariant: middleware sets T //nolint:forcetypeassert\nfunc handler() {\n\tv := input.(T)\n\tuse(v)\n}\n\nfunc use(v T) {}\n"
	doc := &semantic.Document{Path: "handler.go", Language: "go", Source: []byte(source), Imports: []string{"net/http"}, Functions: []semantic.Function{{Name: "handler", Location: semantic.Location{File: "handler.go", StartLine: 6, EndLine: 9}}}, Facts: []semantic.Fact{
		{ID: "source-1", Kind: semantic.FactSource, Operation: "input", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 7}, Expression: "input", Outputs: []string{"input"}},
		{ID: "guard-1", Kind: semantic.FactGuard, Operation: "type_assertion.unchecked", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 7}, Expression: "v := input.(T)", Inputs: []string{"input"}, Outputs: []string{"v"}},
		{ID: "prop-1", Kind: semantic.FactPropagation, Operation: "assignment", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 7}, Expression: "v := input.(T)", Inputs: []string{"input"}, Outputs: []string{"v"}},
		{ID: "prop-2", Kind: semantic.FactPropagation, Operation: "argument", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 8}, Expression: "use(v)", Inputs: []string{"v"}},
		{ID: "sink-1", Kind: semantic.FactSink, Operation: "use", Function: "handler", Location: semantic.Location{File: "handler.go", StartLine: 8}, Expression: "use(v)", Inputs: []string{"v"}},
	}}
	g.AddDocument(doc)
	for _, node := range []*graph.Node{
		{ID: "caller", Kind: graph.NodeFunction, Name: "caller", File: "handler.go", StartLine: 3, EndLine: 3},
		{ID: "handler", Kind: graph.NodeFunction, Name: "handler", File: "handler.go", StartLine: 6, EndLine: 9},
		{ID: "use", Kind: graph.NodeFunction, Name: "use", File: "handler.go", StartLine: 11, EndLine: 11},
	} {
		g.AddNode(node)
	}
	g.AddEdge(&graph.Edge{ID: "e1", Kind: graph.EdgeCalls, From: "caller", To: "handler"})
	g.AddEdge(&graph.Edge{ID: "e2", Kind: graph.EdgeCalls, From: "handler", To: "use"})
	g.AddEdge(&graph.Edge{ID: "e3", Kind: graph.EdgeCalls, From: "use", To: "caller"})
	return g
}
