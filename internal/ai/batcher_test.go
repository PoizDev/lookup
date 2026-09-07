package ai

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/reviewcontext"
	"github.com/poizdev/lookup/internal/semantic"
)

func batchRequest(id, snippet string) ReviewRequest {
	return ReviewRequest{Finding: finding.Finding{ID: id, RuleID: "R"}, CodeSnippet: snippet}
}

func TestContextAwareBatchesFitInputOutputAndPromptOverhead(t *testing.T) {
	requests := []ReviewRequest{
		batchRequest("a", strings.Repeat("a", 7000)),
		batchRequest("b", strings.Repeat("b", 7000)),
		batchRequest("c", strings.Repeat("c", 2000)),
	}
	constraints := OllamaBatchConstraints()
	batches, rejected := AdaptiveBatchesWithConstraints(requests, constraints)
	if len(rejected) != 0 {
		t.Fatalf("rejected requests = %v", rejected)
	}
	for index, batch := range batches {
		total := EstimateBatchTokens(batch) + EstimateBatchOutputTokens(len(batch)) + constraints.PromptOverheadTokens
		if total > 4096 {
			t.Fatalf("batch %d context use = %d, want <= 4096", index, total)
		}
	}
}

func TestOllamaBatchingUsesSerializedPromptContract(t *testing.T) {
	request := batchRequest("formerly-4073", strings.Repeat("func x() { return value }; ", 1800))
	original, err := BuildOllamaRequest([]ReviewRequest{request}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if original.ContextUse() <= 4096 {
		t.Fatalf("fixture context use = %d, want oversized", original.ContextUse())
	}
	batches, rejected := AdaptiveBatchesWithConstraints([]ReviewRequest{request}, OllamaBatchConstraints())
	if len(rejected) != 0 || len(batches) != 1 {
		t.Fatalf("batches=%d rejected=%d", len(batches), len(rejected))
	}
	executable, err := BuildOllamaRequest(batches[0], 4096)
	if err != nil {
		t.Fatal(err)
	}
	if executable.ContextUse() > executable.NumCtx {
		t.Fatalf("executable request uses %d > %d", executable.ContextUse(), executable.NumCtx)
	}
	if batches[0][0].CodeSnippet == request.CodeSnippet {
		t.Fatal("oversized request was not minimized")
	}
}

func TestContextAwareMinimizationRetainsPrimaryEvidenceFirst(t *testing.T) {
	request := batchRequest("primary", "")
	primary := strings.Repeat("primary-evidence", 20)
	request.Packet = reviewcontext.ReviewPacket{
		Candidate:   finding.PotentialFinding{Finding: request.Finding},
		Sufficiency: reviewcontext.SufficiencySufficient,
		Evidence: []reviewcontext.EvidenceItem{
			{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: primary},
			{ID: "source.secondary", Kind: reviewcontext.EvidenceEnclosing, Content: strings.Repeat("secondary", 500)},
		},
	}
	request.Packet = reviewcontext.RecalculateMetadata(request.Packet)
	batches, rejected := AdaptiveBatchesWithConstraints([]ReviewRequest{request}, BatchConstraints{InputBudget: 8000, OutputBudget: 2048, ContextWindow: 4096, PromptOverheadTokens: OllamaChatTemplateAllowance})
	if len(rejected) != 0 || len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("batches=%#v rejected=%#v", batches, rejected)
	}
	got := batches[0][0].Packet.Evidence
	if len(got) == 0 || got[0].ID != "source.primary" || got[0].Content != primary {
		t.Fatalf("primary evidence was not retained: %#v", got)
	}
}

func TestMinimizationRetainsCounterevidenceBeforeGenericContext(t *testing.T) {
	request := batchRequest("counterevidence", "")
	request.Packet = reviewcontext.ReviewPacket{
		Candidate:   finding.PotentialFinding{Finding: request.Finding, Observation: "observed", Hypothesis: "hypothesis"},
		Sufficiency: reviewcontext.SufficiencySufficient,
		Evidence: []reviewcontext.EvidenceItem{
			{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: strings.Repeat("primary", 30)},
			{ID: "source.enclosing", Kind: reviewcontext.EvidenceEnclosing, Content: strings.Repeat("generic", 900)},
			{ID: "comment.1", Kind: reviewcontext.EvidenceComment, Content: "// invariant: middleware guarantees T //nolint:forcetypeassert"},
		},
	}
	request.Packet = reviewcontext.RecalculateMetadata(request.Packet)
	got := minimizeToBudget(request, 600)
	again := minimizeToBudget(request, 600)
	if !reflect.DeepEqual(got.Packet.Evidence, again.Packet.Evidence) {
		t.Fatalf("priority minimization is not deterministic: %#v != %#v", got.Packet.Evidence, again.Packet.Evidence)
	}
	ids := got.Packet.EvidenceIDs()
	if _, ok := ids["source.primary"]; !ok {
		t.Fatalf("primary evidence removed: %#v", got.Packet.Evidence)
	}
	if _, ok := ids["comment.1"]; !ok {
		t.Fatalf("invariant counterevidence removed before generic context: %#v", got.Packet.Evidence)
	}
	if _, ok := ids["source.enclosing"]; ok {
		t.Fatalf("generic context survived before invariant: %#v", got.Packet.Evidence)
	}
}

func TestMinimizationRecomputesSufficiencyAfterCriticalEvidenceLoss(t *testing.T) {
	request := batchRequest("insufficient", "")
	request.Packet = reviewcontext.ReviewPacket{
		Candidate:   finding.PotentialFinding{Finding: request.Finding, Observation: "observed", Hypothesis: "hypothesis"},
		Sufficiency: reviewcontext.SufficiencySufficient,
		Evidence: []reviewcontext.EvidenceItem{
			{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: strings.Repeat("primary", 800)},
			{ID: "guard.1", Kind: reviewcontext.EvidenceGuard, Content: strings.Repeat("guard", 800), Required: true},
		},
	}
	request.Packet = reviewcontext.RecalculateMetadata(request.Packet)
	got := minimizeToBudget(request, 80)
	if got.Packet.Sufficiency == reviewcontext.SufficiencySufficient {
		t.Fatalf("semantically truncated packet remained sufficient: %#v", got.Packet)
	}
}

func TestMinimizationOfIncompletePacketRejectsRequiredEvidenceLoss(t *testing.T) {
	request := batchRequest("partial-required", "")
	request.Packet = reviewcontext.ReviewPacket{
		Candidate:          finding.PotentialFinding{Finding: request.Finding, Observation: "observed", Hypothesis: "risk"},
		Sufficiency:        reviewcontext.SufficiencyIncomplete,
		MissingInformation: []string{"optional caller unavailable"},
		Evidence: []reviewcontext.EvidenceItem{
			{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: strings.Repeat("primary", 800), Required: true},
			{ID: "guard.required", Kind: reviewcontext.EvidenceGuard, Content: strings.Repeat("guard", 800), Required: true},
		},
	}
	request.Packet = reviewcontext.RecalculateMetadata(request.Packet)
	got := minimizeToBudget(request, 80)
	if got.Packet.Sufficiency != reviewcontext.SufficiencyInsufficient {
		t.Fatalf("required loss from incomplete packet did not fail closed: %#v", got.Packet)
	}
}

func TestOllamaPromptEstimateUsesTokenUnits(t *testing.T) {
	request, err := BuildOllamaRequest([]ReviewRequest{batchRequest("token-units", strings.Repeat("abcd", 100))}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	want := EstimateGenericPromptTokens(request.Prompt.System + request.Prompt.User).Tokens
	if request.PromptEstimate != want {
		t.Fatalf("PromptEstimate=%d, want token estimate %d", request.PromptEstimate, want)
	}
	if request.ContextUse() != request.PromptEstimate+request.NumPredict+request.ChatTemplateAllowance {
		t.Fatalf("context equation drifted: %#v", request)
	}
}

func TestMinimizationDoesNotMutateCallerOwnedEvidence(t *testing.T) {
	request := batchRequest("immutable", strings.Repeat("legacy", 1000))
	request.Finding.Evidence.Steps = []finding.EvidenceStep{{File: strings.Repeat("path", 100), Expression: strings.Repeat("expression", 100), Message: strings.Repeat("message", 100)}}
	request.CallChain = []string{"one", "two", "three"}
	original := request
	original.Finding.Evidence.Steps = append([]finding.EvidenceStep(nil), request.Finding.Evidence.Steps...)
	original.CallChain = append([]string(nil), request.CallChain...)
	_ = minimizeToBudget(request, 100)
	if !reflect.DeepEqual(request, original) {
		t.Fatalf("minimization mutated caller request:\n got %#v\nwant %#v", request, original)
	}
}

func TestOllamaMinimizationRetainsBuilderBoundednessEvidence(t *testing.T) {
	g := graph.NewGraph()
	source := []byte("package p\nfunc shutdown(hooks []func()) {\n for _, hook := range hooks { go hook() }\n}\n")
	g.AddDocument(&semantic.Document{Path: "shutdown.go", Language: "go", Source: source,
		Functions: []semantic.Function{{Name: "shutdown", Location: semantic.Location{File: "shutdown.go", StartLine: 2, EndLine: 4}}},
		Facts:     []semantic.Fact{{ID: "go-1", Kind: semantic.FactConcurrencyOperation, Operation: "goroutine.loop", Function: "shutdown", Location: semantic.Location{File: "shutdown.go", StartLine: 3, EndLine: 3}, Expression: "go hook()", Metadata: map[string]string{"loop_bound": "finite_collection", "loop_expression": "hooks"}}}})
	candidate := finding.Finding{ID: "boundedness", RuleID: "GO-CON-001", Observation: "A goroutine is started once per finite collection iteration.", Hypothesis: "The collection size may permit excessive concurrent work.", Location: &finding.Location{File: "shutdown.go", StartLine: 3, EndLine: 3}, Evidence: finding.Evidence{Steps: []finding.EvidenceStep{{Expression: "go hook()"}}}}
	packet := reviewcontext.NewBuilder(g, reviewcontext.Limits{MaxSourceLines: 20, MaxSymbols: 4, MaxEvidenceItems: 20, MaxApproxTokens: 10000}).Build(candidate)
	packet.Evidence = append(packet.Evidence, reviewcontext.EvidenceItem{ID: "generic.large", Kind: reviewcontext.EvidenceSymbol, Content: strings.Repeat("generic ", 2000)})
	packet = reviewcontext.RecalculateMetadata(packet)
	request := ReviewRequest{Finding: candidate, Packet: packet}
	got := minimizeOllamaToContext(request, 4096)
	var boundedness reviewcontext.EvidenceItem
	for _, evidence := range got.Packet.Evidence {
		if evidence.FactID == "go-1" {
			boundedness = evidence
		}
	}
	if !boundedness.Required || boundedness.Retention != reviewcontext.RetentionCritical || !strings.Contains(boundedness.Content, "loop_bound=finite_collection") || !strings.Contains(boundedness.Content, "loop_expression=hooks") {
		t.Fatalf("boundedness evidence did not survive provider minimization: %#v", got.Packet.Evidence)
	}
	if _, ok := got.Packet.EvidenceIDs()["generic.large"]; ok {
		t.Fatalf("generic context survived before required boundedness evidence: %#v", got.Packet.Evidence)
	}
}

func TestContextAwareBatchingRejectsPacketThatCannotFit(t *testing.T) {
	request := batchRequest("cannot-fit", "small")
	batches, rejected := AdaptiveBatchesWithConstraints([]ReviewRequest{request}, BatchConstraints{InputBudget: 8000, OutputBudget: 2048, ContextWindow: 800, PromptOverheadTokens: 512})
	if len(batches) != 0 || len(rejected) != 1 || rejected[0].Finding.ID != "cannot-fit" {
		t.Fatalf("batches=%#v rejected=%#v", batches, rejected)
	}
}

func TestAdaptiveBatchesGroupSmallRequestsWithinBudget(t *testing.T) {
	requests := []ReviewRequest{batchRequest("1", "small"), batchRequest("2", "small"), batchRequest("3", strings.Repeat("x", 800))}
	batches := AdaptiveBatches(requests, 300)
	if len(batches) != 2 || len(batches[0]) != 2 || batches[1][0].Finding.ID != "3" {
		t.Fatalf("batches = %#v", batches)
	}
	for _, batch := range batches {
		if EstimateBatchTokens(batch) > 300 {
			t.Fatalf("batch exceeds budget: %d", EstimateBatchTokens(batch))
		}
	}
}

func TestAdaptiveBatchesOutputBudgetBoundsSmallFindings(t *testing.T) {
	// 50 small findings will easily fit 20000 input tokens, but exceed a 4096 output budget
	// (4096 - 64) / 384 = ~10 findings per batch max.
	requests := make([]ReviewRequest, 50)
	for i := range requests {
		requests[i] = batchRequest(fmt.Sprintf("f-%d", i), "small snippet")
	}

	batches := AdaptiveBatches(requests, 20000, 4096)
	if len(batches) <= 1 {
		t.Fatalf("expected multiple batches due to output budget, got %d batches", len(batches))
	}

	for i, batch := range batches {
		if EstimateBatchTokens(batch) > 20000 {
			t.Fatalf("batch %d exceeds input budget: %d", i, EstimateBatchTokens(batch))
		}
		if EstimateBatchOutputTokens(len(batch)) > 4096 && len(batch) > 1 {
			t.Fatalf("batch %d exceeds output budget: %d tokens for %d findings", i, EstimateBatchOutputTokens(len(batch)), len(batch))
		}
	}
}

func TestAdaptiveBatchesInputBudgetWinsWhenMoreRestrictive(t *testing.T) {
	// Large snippets with very small input budget (e.g. 500 tokens) but large output budget (8192)
	requests := make([]ReviewRequest, 10)
	for i := range requests {
		requests[i] = batchRequest(fmt.Sprintf("f-%d", i), strings.Repeat("code-line\n", 30))
	}

	batches := AdaptiveBatches(requests, 500, 8192)
	for i, batch := range batches {
		if EstimateBatchTokens(batch) > 500 && len(batch) > 1 {
			t.Fatalf("batch %d exceeds restrictive input budget: %d tokens", i, EstimateBatchTokens(batch))
		}
	}
}

func TestAdaptiveBatchesOutputBudgetWinsWhenMoreRestrictive(t *testing.T) {
	// Tiny snippets with massive input budget (50000 tokens) but small output budget (1024)
	requests := make([]ReviewRequest, 10)
	for i := range requests {
		requests[i] = batchRequest(fmt.Sprintf("f-%d", i), "x")
	}

	batches := AdaptiveBatches(requests, 50000, 1024)
	// (1024 - 64) / 384 = 2 findings per batch max.
	for i, batch := range batches {
		if len(batch) > 2 {
			t.Fatalf("batch %d has %d findings, exceeds max allowed by 1024 output budget", i, len(batch))
		}
		if EstimateBatchOutputTokens(len(batch)) > 1024 && len(batch) > 1 {
			t.Fatalf("batch %d exceeds output budget: %d tokens", i, EstimateBatchOutputTokens(len(batch)))
		}
	}
}

func TestAdaptiveBatchesPreservesDeterministicOrder(t *testing.T) {
	requests := make([]ReviewRequest, 35)
	for i := range requests {
		requests[i] = batchRequest(fmt.Sprintf("order-%02d", i), "snippet")
	}

	batches := AdaptiveBatches(requests, 20000, 4096)
	var reassembled []string
	for _, batch := range batches {
		for _, req := range batch {
			reassembled = append(reassembled, req.Finding.ID)
		}
	}

	if len(reassembled) != len(requests) {
		t.Fatalf("reassembled count %d != original count %d", len(reassembled), len(requests))
	}
	for i, id := range reassembled {
		want := fmt.Sprintf("order-%02d", i)
		if id != want {
			t.Fatalf("position %d = %q, want %q", i, id, want)
		}
	}
}

func TestAdaptiveBatchesMinimizeOversizedFindingWithoutDroppingIt(t *testing.T) {
	request := batchRequest("large", strings.Repeat("line-of-code\n", 1000))
	request.Finding.Title = strings.Repeat("large-title", 200)
	request.Finding.Reason = strings.Repeat("large-reason", 200)
	request.Finding.Evidence.Steps = []finding.EvidenceStep{{Kind: "source", Expression: strings.Repeat("large-expression", 200), Message: strings.Repeat("large-message", 200)}}
	batches := AdaptiveBatches([]ReviewRequest{request}, 220, 4096)
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0].Finding.ID != "large" {
		t.Fatalf("batches = %#v", batches)
	}
	if EstimateBatchTokens(batches[0]) > 220 {
		t.Fatalf("oversized batch = %d tokens", EstimateBatchTokens(batches[0]))
	}
}
