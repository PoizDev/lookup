package ai

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

func TestBuildReviewPlanContextStopsDuringPacketConstruction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	built := 0
	items := make([]finding.Finding, 100)
	for index := range items {
		items[index] = planFinding(fmt.Sprintf("candidate-%d", index), fmt.Sprintf("RULE-%d", index), finding.SeverityHigh, fmt.Sprintf("%d.go", index))
	}
	_, err := BuildReviewPlanForProviderContext(ctx, "ollama", ReviewModeAll, items, DefaultReviewBudget(), func(item finding.Finding) reviewcontext.ReviewPacket {
		built++
		cancel()
		return planPacket(item)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if built != 1 {
		t.Fatalf("built %d packets after cancellation, want 1", built)
	}
}

func TestBuildReviewPlanSmartIsDeterministicAndDiverse(t *testing.T) {
	items := []finding.Finding{}
	for i := 0; i < 6; i++ {
		items = append(items, planFinding(fmt.Sprintf("noise-%d", i), "NOISE", finding.SeverityHigh, "a.go"))
	}
	items = append(items, planFinding("other", "OTHER", finding.SeverityMedium, "b.go"))
	budget := ReviewBudget{MaxReviewedFindings: 3, MaxProviderRequests: 3, MaxEstimatedInputTokens: 100000, MaxOutputTokens: 100000}
	first := BuildReviewPlan(ReviewModeSmart, items, budget, planPacket)
	second := BuildReviewPlan(ReviewModeSmart, items, budget, planPacket)
	if !reflect.DeepEqual(first.SelectedIDs, second.SelectedIDs) {
		t.Fatalf("selection not deterministic: %v != %v", first.SelectedIDs, second.SelectedIDs)
	}
	seen := map[string]bool{}
	for _, item := range first.Selected {
		seen[item.Request.Finding.RuleID] = true
	}
	if !seen["NOISE"] || !seen["OTHER"] {
		t.Fatalf("selection lacks rule diversity: %#v", first.SelectedIDs)
	}
}

func TestBuildReviewPlanForOllamaSelectsRequestsWithinProviderRequestBudget(t *testing.T) {
	items := make([]finding.Finding, 7)
	for index := range items {
		items[index] = planFinding(fmt.Sprintf("candidate-%d", index), fmt.Sprintf("RULE-%d", index), finding.SeverityHigh, fmt.Sprintf("%d.go", index))
	}
	build := func(item finding.Finding) reviewcontext.ReviewPacket {
		packet := planPacket(item)
		packet.Evidence = []reviewcontext.EvidenceItem{
			{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "direct source"},
			{ID: "source.enclosing", Kind: reviewcontext.EvidenceEnclosing, Content: strings.Repeat("evidence ", 1200)},
		}
		return reviewcontext.RecalculateMetadata(packet)
	}
	plan := BuildReviewPlanForProvider("ollama", ReviewModeAll, items, DefaultReviewBudget(), build)
	if len(plan.Selected) != 7 || plan.EstimatedProviderRequests > plan.Budget.MaxProviderRequests {
		t.Fatalf("selected=%d requests=%d budget=%d", len(plan.Selected), plan.EstimatedProviderRequests, plan.Budget.MaxProviderRequests)
	}
	if len(plan.Skipped) != 0 {
		t.Fatalf("skipped=%#v", plan.Skipped)
	}
}

func TestBuildReviewPlanForOllamaFailsClosedWhenPacketCannotFit(t *testing.T) {
	item := planFinding("too-large", "RULE", finding.SeverityHigh, "large.go")
	plan := buildReviewPlan(ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), planPacket, BatchConstraints{InputBudget: 8000, OutputBudget: 2048, ContextWindow: 800, PromptOverheadTokens: 512})
	if len(plan.Selected) != 0 || len(plan.Batches) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != SkipContextInsufficient {
		t.Fatalf("plan did not fail closed: %#v", plan)
	}
}

func TestBuildReviewPlanRejectsPacketAfterRequiredEvidenceIsMinimized(t *testing.T) {
	item := planFinding("required-context", "RULE", finding.SeverityHigh, "large.go")
	build := func(f finding.Finding) reviewcontext.ReviewPacket {
		packet := reviewcontext.ReviewPacket{
			Candidate: finding.PotentialFinding{Finding: f, Observation: "observed", Hypothesis: "risk", ReviewPolicy: f.ReviewPolicy},
			Evidence: []reviewcontext.EvidenceItem{
				{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "source"},
				{ID: "comment.1", Kind: reviewcontext.EvidenceComment, Content: strings.Repeat("// invariant ", 1000), Required: true},
			},
			Sufficiency: reviewcontext.SufficiencySufficient,
		}
		return reviewcontext.RecalculateMetadata(packet)
	}
	plan := BuildReviewPlanForProvider("ollama", ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), build)
	if len(plan.Selected) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != SkipContextInsufficient {
		t.Fatalf("semantically insufficient minimized packet was planned: %#v", plan)
	}
}

func TestBuildReviewPlanDoesNotSelectGO004WithoutRetainedPostUseEvidence(t *testing.T) {
	item := planFinding("scanner", "GO-COR-004", finding.SeverityMedium, "scanner.go")
	build := func(f finding.Finding) reviewcontext.ReviewPacket {
		packet := reviewcontext.ReviewPacket{
			Candidate: finding.PotentialFinding{Finding: f, Observation: "scanner iteration is visible", Hypothesis: "scanner.Err is not checked", ReviewPolicy: f.ReviewPolicy},
			Evidence: []reviewcontext.EvidenceItem{
				{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "scanner := bufio.NewScanner(r)", Required: true},
				{ID: "source.iteration", Kind: reviewcontext.EvidenceLifecycle, Content: "scanner.Scan()", Required: true, Retention: reviewcontext.RetentionCritical},
				{ID: "source.post_use", Kind: reviewcontext.EvidenceLifecycle, Content: strings.Repeat("post-loop ", 2000), Required: true, Retention: reviewcontext.RetentionCritical},
			},
			Sufficiency: reviewcontext.SufficiencySufficient,
		}
		return reviewcontext.RecalculateMetadata(packet)
	}
	plan := buildReviewPlan(ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), build, BatchConstraints{InputBudget: 8000, OutputBudget: 2048, ContextWindow: 800, PromptOverheadTokens: 512})
	if len(plan.Selected) != 0 || len(plan.Batches) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != SkipContextInsufficient {
		t.Fatalf("GO-COR-004 without retained post-use evidence reached provider plan: %#v", plan)
	}
}

func TestBuildReviewPlanSelectsIncompletePacketWithRequiredEvidence(t *testing.T) {
	item := planFinding("partial", "RULE", finding.SeverityHigh, "partial.go")
	build := func(f finding.Finding) reviewcontext.ReviewPacket {
		return reviewcontext.RecalculateMetadata(reviewcontext.ReviewPacket{
			Candidate:          finding.PotentialFinding{Finding: f, Observation: "observed", Hypothesis: "risk", ReviewPolicy: f.ReviewPolicy},
			Evidence:           []reviewcontext.EvidenceItem{{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "source", Required: true}},
			MissingInformation: []string{"callee context unavailable"},
			Sufficiency:        reviewcontext.SufficiencyIncomplete,
		})
	}
	plan := BuildReviewPlanForProvider("ollama", ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), build)
	if len(plan.Selected) != 1 || len(plan.Batches) != 1 {
		t.Fatalf("reviewable incomplete packet was skipped: %#v", plan)
	}
	request := plan.Selected[0].Request
	if request.Packet.Sufficiency != reviewcontext.SufficiencyIncomplete || !reflect.DeepEqual(request.Packet.MissingInformation, []string{"callee context unavailable"}) {
		t.Fatalf("missing information did not survive planning: %#v", request.Packet)
	}
	prompt, err := BuildPrompt([]ReviewRequest{request})
	if err != nil || !strings.Contains(prompt.User, "callee context unavailable") {
		t.Fatalf("missing information did not survive provider serialization: err=%v prompt=%q", err, prompt.User)
	}
}

func TestResolvedOllamaContextIsSharedByPlanAndExecutableRequest(t *testing.T) {
	item := planFinding("resolved", "RULE", finding.SeverityHigh, "resolved.go")
	plan, err := BuildReviewPlanForProviderResolvedContext(context.Background(), "ollama", 6144, ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), planPacket)
	if err != nil || len(plan.Batches) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	request, err := BuildOllamaRequest(plan.Batches[0], 6144)
	if err != nil {
		t.Fatal(err)
	}
	if request.NumCtx != 6144 || request.NumPredict != plan.PlannedOutputTokens || !request.Fits() {
		t.Fatalf("request=%#v planned output=%d", request, plan.PlannedOutputTokens)
	}
	if plan.EstimatedInputTokens != request.PromptEstimate {
		t.Fatalf("planned input=%d request estimate=%d", plan.EstimatedInputTokens, request.PromptEstimate)
	}
}

func TestCloudReviewPlanBatchingRemainsUnchanged(t *testing.T) {
	items := []finding.Finding{planFinding("a", "A", finding.SeverityHigh, "a.go"), planFinding("b", "B", finding.SeverityHigh, "b.go")}
	want := BuildReviewPlan(ReviewModeAll, items, DefaultReviewBudget(), planPacket)
	got := BuildReviewPlanForProvider("openai", ReviewModeAll, items, DefaultReviewBudget(), planPacket)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cloud provider plan changed:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestBuildReviewPlanObeysEveryGlobalBudget(t *testing.T) {
	items := []finding.Finding{planFinding("a", "A", finding.SeverityHigh, "a.go"), planFinding("b", "B", finding.SeverityHigh, "b.go"), planFinding("c", "C", finding.SeverityHigh, "c.go")}
	budget := ReviewBudget{MaxReviewedFindings: 2, MaxProviderRequests: 1, MaxEstimatedInputTokens: 900, MaxOutputTokens: 900}
	plan := BuildReviewPlan(ReviewModeAll, items, budget, planPacket)
	if len(plan.Selected) > 2 || plan.EstimatedProviderRequests > 1 || plan.EstimatedInputTokens > 900 || plan.PlannedOutputTokens > 900 {
		t.Fatalf("plan exceeds budget: %#v", plan)
	}
	if len(plan.Skipped) == 0 || plan.Skipped[len(plan.Skipped)-1].Reason != SkipBudgetExhausted {
		t.Fatalf("budget skip missing: %#v", plan.Skipped)
	}
}

func TestBuildReviewPlanOffDoesNotBuildContext(t *testing.T) {
	built := 0
	plan := BuildReviewPlan(ReviewModeOff, []finding.Finding{planFinding("a", "A", finding.SeverityHigh, "a.go")}, DefaultReviewBudget(), func(item finding.Finding) reviewcontext.ReviewPacket {
		built++
		return planPacket(item)
	})
	if built != 0 || len(plan.Selected) != 0 || plan.Skipped[0].Reason != SkipUserDisabled {
		t.Fatalf("off plan = %#v, built=%d", plan, built)
	}
}

func planFinding(id, rule string, severity finding.Severity, file string) finding.Finding {
	return finding.Finding{ID: id, RuleID: rule, Category: finding.CategoryMaintainability, Severity: severity, Confidence: .7, EvidenceStrength: "weak", ReviewPolicy: finding.ReviewPolicyContextRequired, Location: &finding.Location{File: file}}
}

func planPacket(item finding.Finding) reviewcontext.ReviewPacket {
	packet := reviewcontext.ReviewPacket{Candidate: finding.PotentialFinding{Finding: item, ReviewPolicy: item.ReviewPolicy}, Sufficiency: reviewcontext.SufficiencySufficient}
	return reviewcontext.RecalculateMetadata(packet)
}

// TestPlanOllamaSelectedRequestMatchesBatch verifies Fix 1: the request stored
// in plan.Selected[i].Request is exactly the context-minimized form also stored
// in plan.Batches, so the executor never re-expands to the pre-minimization payload.
func TestPlanOllamaSelectedRequestMatchesBatch(t *testing.T) {
	// Build a request with evidence large enough to need context minimization.
	// The packet exceeds the executable 4096-token context after prompt/output allowance.
	bigEvidence := strings.Repeat("evidence line content here\n", 700)
	item := planFinding("large-context", "SEC-001", finding.SeverityHigh, "main.go")

	build := func(f finding.Finding) reviewcontext.ReviewPacket {
		packet := reviewcontext.ReviewPacket{
			Candidate: finding.PotentialFinding{Finding: f, ReviewPolicy: f.ReviewPolicy},
			Evidence: []reviewcontext.EvidenceItem{
				{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "direct source"},
				{ID: "source.enclosing", Kind: reviewcontext.EvidenceEnclosing, Content: bigEvidence},
			},
			Sufficiency: reviewcontext.SufficiencySufficient,
		}
		return reviewcontext.RecalculateMetadata(packet)
	}

	// Verify the raw evidence is actually oversized before minimization.
	rawReq := ReviewRequest{Finding: item, Packet: build(item)}
	rawContract, _ := BuildOllamaRequest([]ReviewRequest{rawReq}, 4096)
	if rawContract.Fits() {
		t.Skip("fixture no longer oversized — adjust evidence size")
	}

	plan := BuildReviewPlanForProvider("ollama", ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), build)
	if len(plan.Selected) != 1 {
		t.Fatalf("expected 1 selected, got %d (skipped: %v)", len(plan.Selected), plan.Skipped)
	}

	selected := plan.Selected[0].Request
	if len(plan.Batches) != 1 || len(plan.Batches[0]) != 1 {
		t.Fatalf("unexpected batches: %v", plan.Batches)
	}
	batched := plan.Batches[0][0]

	// Fix 1 invariant: selected request must equal batched request evidence content.
	if len(selected.Packet.Evidence) == 0 || len(batched.Packet.Evidence) == 0 {
		t.Fatal("evidence was completely removed from planned request")
	}
	if selected.Packet.Evidence[0].Content != batched.Packet.Evidence[0].Content {
		t.Errorf("plan.Selected evidence (%d bytes) != plan.Batches evidence (%d bytes)",
			len(selected.Packet.Evidence[0].Content), len(batched.Packet.Evidence[0].Content))
	}

	// The selected request must never re-expand to the pre-minimization evidence.
	if selected.Packet.Evidence[0].Content == bigEvidence {
		t.Error("plan.Selected[i].Request was not context-minimized — still holds full pre-minimization evidence")
	}

	// Both must satisfy the executor's fit check at the resolved context window.
	for _, label := range []string{"selected", "batched"} {
		var req ReviewRequest
		if label == "selected" {
			req = selected
		} else {
			req = batched
		}
		contract, err := BuildOllamaRequest([]ReviewRequest{req}, 4096)
		if err != nil {
			t.Fatalf("%s: BuildOllamaRequest error: %v", label, err)
		}
		if !contract.Fits() {
			t.Errorf("%s: ContextUse=%d > NumCtx=%d — executor would reject this request", label, contract.ContextUse(), contract.NumCtx)
		}
	}
}

// TestPlanOllamaMinimizationIsIdempotentOnAlreadyFittingRequests verifies that
// applying context minimization to a request that already fits does not alter it.
func TestPlanOllamaMinimizationIsIdempotentOnAlreadyFittingRequests(t *testing.T) {
	item := planFinding("small", "R", finding.SeverityHigh, "a.go")
	smallEvidence := strings.Repeat("x", 200) // small, well within limits

	build := func(f finding.Finding) reviewcontext.ReviewPacket {
		packet := reviewcontext.ReviewPacket{
			Candidate: finding.PotentialFinding{Finding: f, ReviewPolicy: f.ReviewPolicy},
			Evidence: []reviewcontext.EvidenceItem{
				{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: smallEvidence},
			},
			Sufficiency: reviewcontext.SufficiencySufficient,
		}
		return reviewcontext.RecalculateMetadata(packet)
	}

	plan := BuildReviewPlanForProvider("ollama", ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), build)
	if len(plan.Selected) != 1 {
		t.Fatalf("expected 1 selected, got %d (skipped: %v)", len(plan.Selected), plan.Skipped)
	}

	selected := plan.Selected[0].Request
	if len(selected.Packet.Evidence) == 0 || selected.Packet.Evidence[0].Content != smallEvidence {
		t.Errorf("small fitting request was unnecessarily truncated: evidence=%q", selected.Packet.Evidence[0].Content)
	}

	contract, _ := BuildOllamaRequest([]ReviewRequest{selected}, 4096)
	if !contract.Fits() {
		t.Errorf("small request should fit: ContextUse=%d > %d", contract.ContextUse(), contract.NumCtx)
	}
}

type recordingExecutionProvider struct {
	recorded []ReviewRequest
}

func (*recordingExecutionProvider) Name() string { return "ollama" }
func (p *recordingExecutionProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := p.ReviewBatch(ctx, []ReviewRequest{request})
	if len(responses) == 0 {
		return ReviewResponse{}, err
	}
	return responses[0], err
}
func (p *recordingExecutionProvider) ReviewBatch(ctx context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	p.recorded = append(p.recorded, requests...)
	ObserveProviderResponse(ctx, 200)
	ObserveProviderPromptTokens(ctx, 3072)
	responses := make([]ReviewResponse, len(requests))
	for i, req := range requests {
		responses[i] = supportedTestResponse(req, "ok")
	}
	return responses, nil
}

// TestOllamaExecutionUsesExactPlannedRequest verifies Fix 1 end-to-end:
// a request requiring context minimization reaches execution unchanged,
// matches the plan's minimized form, never re-expands to pre-minimization
// evidence, and satisfies BuildOllamaRequest.Fits().
func TestOllamaExecutionUsesExactPlannedRequest(t *testing.T) {
	bigEvidence := strings.Repeat("evidence line content here\n", 700)
	item := planFinding("exec-verify", "SEC-001", finding.SeverityHigh, "main.go")

	build := func(f finding.Finding) reviewcontext.ReviewPacket {
		packet := reviewcontext.ReviewPacket{
			Candidate: finding.PotentialFinding{Finding: f, ReviewPolicy: f.ReviewPolicy},
			Evidence: []reviewcontext.EvidenceItem{
				{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "direct source"},
				{ID: "source.enclosing", Kind: reviewcontext.EvidenceEnclosing, Content: bigEvidence},
			},
			Sufficiency: reviewcontext.SufficiencySufficient,
		}
		return reviewcontext.RecalculateMetadata(packet)
	}

	plan := BuildReviewPlanForProvider("ollama", ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), build)
	if len(plan.Selected) != 1 {
		t.Fatalf("expected 1 selected, got %d", len(plan.Selected))
	}

	provider := &recordingExecutionProvider{}
	reviewer := NewTriageReviewer(provider, TriageOptions{
		Mode:        ReviewModeAll,
		TokenBudget: 60000,
		Concurrency: 1,
		Budget:      plan.Budget,
		Plan:        &plan,
	}, nil)

	got, stats := reviewer.ReviewFindings(context.Background(), []finding.Finding{item}, nil)
	if len(got) != 1 || !got[0].AIReviewed || stats.ProviderReviews != 1 {
		t.Fatalf("review failed: got=%v, stats=%+v", got, stats)
	}
	if len(provider.recorded) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(provider.recorded))
	}

	executedReq := provider.recorded[0]
	plannedReq := plan.Selected[0].Request

	// Verify request reached execution unchanged from planned minimized form
	if executedReq.Packet.Evidence[0].Content != plannedReq.Packet.Evidence[0].Content {
		t.Errorf("executed evidence (%d bytes) != planned evidence (%d bytes)",
			len(executedReq.Packet.Evidence[0].Content), len(plannedReq.Packet.Evidence[0].Content))
	}
	// Verify it never re-expanded to pre-minimization evidence
	if executedReq.Packet.Evidence[0].Content == bigEvidence {
		t.Error("executed request re-expanded to full pre-minimization evidence")
	}
	// Verify it satisfies executor context fit
	contract, err := BuildOllamaRequest([]ReviewRequest{executedReq}, 4096)
	if err != nil {
		t.Fatalf("BuildOllamaRequest error: %v", err)
	}
	if !contract.Fits() {
		t.Errorf("executed request fails context fit: ContextUse=%d > %d", contract.ContextUse(), contract.NumCtx)
	}
}

func TestExactSizingStrategyIsSharedByPlanAndExecution(t *testing.T) {
	item := planFinding("exact-sizing", "RULE", finding.SeverityHigh, "main.go")
	counter := &fakePromptTokenCounter{count: PromptTokenCount{Tokens: 3000, TemplateIncluded: true, Source: TokenCountExact}}
	strategy := PromptSizingStrategy{Counter: counter}
	plan, err := BuildReviewPlanForProviderResolvedSizingContext(context.Background(), "ollama", 4096, strategy, ReviewModeAll, []finding.Finding{item}, DefaultReviewBudget(), planPacket)
	if err != nil || len(plan.Selected) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	selectedSizing := plan.Selected[0]
	if selectedSizing.PromptTokenSource != TokenCountExact || selectedSizing.EstimatedInputTokens != 3000 || selectedSizing.TemplateAllowance != 0 || selectedSizing.PredictedContextUse != 3448 {
		t.Fatalf("planned sizing metadata = %#v", selectedSizing)
	}
	provider := &recordingExecutionProvider{}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, Concurrency: 1, Budget: plan.Budget, Plan: &plan}, nil)
	_, stats := reviewer.ReviewFindings(context.Background(), []finding.Finding{item}, nil)
	if stats.ProviderReviews != 1 || len(provider.recorded) != 1 || len(stats.ProviderAttempts) != 1 {
		t.Fatalf("execution stats=%#v recorded=%d", stats, len(provider.recorded))
	}
	attempt := stats.ProviderAttempts[0]
	if attempt.PromptTokenSource != TokenCountExact || attempt.EstimatedInputTokens != 3000 || attempt.ChatTemplateAllowance != 0 || attempt.PredictedContextUse != 3448 || attempt.ActualPromptTokens != 3072 || attempt.PromptTokenError != 72 {
		t.Fatalf("execution sizing drifted from plan: %#v", attempt)
	}
	if !reflect.DeepEqual(provider.recorded[0], plan.Selected[0].Request) {
		t.Fatal("executor did not receive the exact minimized planned request")
	}
}
