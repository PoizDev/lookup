package ai

import (
	"context"
	"sort"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

type ReviewBudget struct {
	MaxReviewedFindings     int `json:"max_reviewed_findings" toml:"max_reviewed_findings"`
	MaxProviderRequests     int `json:"max_provider_requests" toml:"max_provider_requests"`
	MaxEstimatedInputTokens int `json:"max_estimated_input_tokens" toml:"max_estimated_input_tokens"`
	MaxOutputTokens         int `json:"max_output_tokens" toml:"max_output_tokens"`
}

func DefaultReviewBudget() ReviewBudget {
	return ReviewBudget{MaxReviewedFindings: 40, MaxProviderRequests: 6, MaxEstimatedInputTokens: 60000, MaxOutputTokens: 16000}
}

type SkipReason string

const (
	SkipNotEligible         SkipReason = "NOT_ELIGIBLE"
	SkipBudgetExhausted     SkipReason = "BUDGET_EXHAUSTED"
	SkipContextInsufficient SkipReason = "CONTEXT_INSUFFICIENT"
	SkipProviderFailure     SkipReason = "PROVIDER_FAILURE"
	SkipUserDisabled        SkipReason = "USER_DISABLED"
)

type SkippedCandidate struct {
	CandidateID string     `json:"candidate_id"`
	Reason      SkipReason `json:"reason"`
}

type PlannedCandidate struct {
	CandidateID           string           `json:"candidate_id"`
	RuleID                string           `json:"rule_id"`
	EstimatedInputTokens  int              `json:"estimated_input_tokens"`
	PlannedOutputTokens   int              `json:"planned_output_tokens"`
	PacketTruncated       bool             `json:"packet_truncated"`
	PromptTokenSource     TokenCountSource `json:"prompt_token_source,omitempty"`
	TokenEstimatorVersion string           `json:"token_estimator_version,omitempty"`
	ContextWindow         int              `json:"context_window,omitempty"`
	TemplateAllowance     int              `json:"template_allowance,omitempty"`
	PredictedContextUse   int              `json:"predicted_context_use,omitempty"`
	Request               ReviewRequest    `json:"-"`
}

type ReviewPlan struct {
	Mode                      ReviewMode           `json:"mode"`
	Budget                    ReviewBudget         `json:"budget"`
	ContextualCandidates      int                  `json:"contextual_candidates"`
	EligibleCandidates        int                  `json:"eligible_candidates"`
	SelectedIDs               []string             `json:"selected_candidate_ids"`
	Selected                  []PlannedCandidate   `json:"selected"`
	EstimatedProviderRequests int                  `json:"estimated_provider_requests"`
	EstimatedInputTokens      int                  `json:"estimated_input_tokens"`
	PlannedOutputTokens       int                  `json:"planned_output_tokens"`
	ContextPacketsBuilt       int                  `json:"context_packets_built"`
	TruncatedPackets          int                  `json:"truncated_packets"`
	Skipped                   []SkippedCandidate   `json:"skipped"`
	Batches                   [][]ReviewRequest    `json:"-"`
	SizingStrategy            PromptSizingStrategy `json:"-"`
	ContextWindow             int                  `json:"-"`
}

const (
	planBatchInputLimit  = 8000
	planBatchOutputLimit = 2048
)

func BuildReviewPlan(mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket) ReviewPlan {
	return buildReviewPlan(mode, items, budget, build, BatchConstraints{InputBudget: planBatchInputLimit, OutputBudget: planBatchOutputLimit})
}

func BuildReviewPlanForProvider(provider string, mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket) ReviewPlan {
	plan, _ := BuildReviewPlanForProviderContext(context.Background(), provider, mode, items, budget, build)
	return plan
}

func BuildReviewPlanForProviderContext(ctx context.Context, provider string, mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket) (ReviewPlan, error) {
	return BuildReviewPlanForProviderResolvedContext(ctx, provider, 0, mode, items, budget, build)
}

func BuildReviewPlanForProviderResolvedContext(ctx context.Context, provider string, contextWindow int, mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket) (ReviewPlan, error) {
	return BuildReviewPlanForProviderResolvedSizingContext(ctx, provider, contextWindow, PromptSizingStrategy{}, mode, items, budget, build)
}

func BuildReviewPlanForProviderResolvedSizingContext(ctx context.Context, provider string, contextWindow int, strategy PromptSizingStrategy, mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket) (ReviewPlan, error) {
	constraints := BatchConstraints{InputBudget: planBatchInputLimit, OutputBudget: planBatchOutputLimit}
	if strings.EqualFold(strings.TrimSpace(provider), "ollama") {
		constraints = OllamaBatchConstraintsForContext(contextWindow)
		constraints.SizingStrategy = strategy
	}
	return buildReviewPlanContext(ctx, mode, items, budget, build, constraints)
}

func buildReviewPlan(mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket, constraints BatchConstraints) ReviewPlan {
	plan, _ := buildReviewPlanContext(context.Background(), mode, items, budget, build, constraints)
	return plan
}

func buildReviewPlanContext(ctx context.Context, mode ReviewMode, items []finding.Finding, budget ReviewBudget, build func(finding.Finding) reviewcontext.ReviewPacket, constraints BatchConstraints) (ReviewPlan, error) {
	budget = normalizeReviewBudget(budget)
	plan := ReviewPlan{Mode: mode, Budget: budget, SizingStrategy: constraints.SizingStrategy, ContextWindow: constraints.ContextWindow}
	eligible := make([]finding.Finding, 0)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if item.ReviewPolicy != "" && item.ReviewPolicy != finding.ReviewPolicyContextRequired {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipNotEligible})
			continue
		}
		plan.ContextualCandidates++
		if mode == ReviewModeOff {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipUserDisabled})
			continue
		}
		if !Eligible(mode, item) {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipNotEligible})
			continue
		}
		eligible = append(eligible, item)
	}
	plan.EligibleCandidates = len(eligible)
	if mode == ReviewModeSmart {
		eligible = diverseOrder(eligible)
	}
	selectedPerRule := map[string]int{}
	perRuleCap := max(2, budget.MaxReviewedFindings/4)
	for _, item := range eligible {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if mode == ReviewModeSmart && selectedPerRule[item.RuleID] >= perRuleCap {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipNotEligible})
			continue
		}
		if len(plan.Selected) >= budget.MaxReviewedFindings {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipBudgetExhausted})
			continue
		}
		request := ReviewRequest{Finding: item, CodeSnippet: item.Evidence.CodeSnippet, CallChain: append([]string(nil), item.Evidence.CallChain...)}
		if build != nil {
			request.Packet = build(item)
			if err := ctx.Err(); err != nil {
				return plan, err
			}
			plan.ContextPacketsBuilt++
			if request.Packet.Metadata.Truncated {
				plan.TruncatedPackets++
			}
			if !request.Packet.Reviewable() {
				plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipContextInsufficient})
				continue
			}
		}
		requestInputBudget := min(constraints.InputBudget, budget.MaxEstimatedInputTokens)
		request = minimizeToBudget(request, requestInputBudget)
		if request.Packet.Candidate.ID != "" && !request.Packet.Reviewable() {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipContextInsufficient})
			continue
		}
		// Apply the same context-aware minimization that AdaptiveBatchesWithConstraints
		// uses internally, so plan.Selected[i].Request stores the identical request
		// that will be placed in plan.Batches. Without this, the executor receives a
		// pre-context-minimization copy that can be 3–4x too large for the provider.
		if constraints.ContextWindow > 0 {
			request = minimizeOllamaToContextWithSizing(ctx, request, constraints.ContextWindow, constraints.SizingStrategy)
			if request.Packet.Candidate.ID != "" && !request.Packet.Reviewable() {
				plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipContextInsufficient})
				continue
			}
		}
		tentative := append(selectedRequests(plan.Selected), request)
		batchConstraints := constraints
		batchConstraints.InputBudget = min(constraints.InputBudget, budget.MaxEstimatedInputTokens)
		batchConstraints.OutputBudget = min(constraints.OutputBudget, budget.MaxOutputTokens)
		batches, rejected := AdaptiveBatchesWithConstraintsContext(ctx, tentative, batchConstraints)
		if len(rejected) > 0 {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipContextInsufficient})
			continue
		}
		input, output := planBatchTokens(ctx, batches, batchConstraints)
		if len(batches) > budget.MaxProviderRequests || input > budget.MaxEstimatedInputTokens || output > budget.MaxOutputTokens {
			plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipBudgetExhausted})
			continue
		}
		planned := PlannedCandidate{CandidateID: item.ID, RuleID: item.RuleID, EstimatedInputTokens: estimateBatchInputContext(ctx, []ReviewRequest{request}, batchConstraints), PlannedOutputTokens: EstimateBatchOutputTokens(1), PacketTruncated: request.Packet.Metadata.Truncated, Request: request}
		if constraints.ContextWindow > 0 {
			contract, contractErr := buildOllamaRequestWithContextPolicy(ctx, []ReviewRequest{request}, constraints.ContextWindow, constraints.PromptOverheadTokens, constraints.SizingStrategy)
			if contractErr != nil {
				plan.Skipped = append(plan.Skipped, SkippedCandidate{CandidateID: item.ID, Reason: SkipContextInsufficient})
				continue
			}
			planned.PromptTokenSource = contract.CountSource
			planned.TokenEstimatorVersion = contract.EstimatorVersion
			planned.ContextWindow = contract.NumCtx
			planned.TemplateAllowance = contract.ChatTemplateAllowance
			planned.PredictedContextUse = contract.ContextUse()
		}
		plan.Selected = append(plan.Selected, planned)
		selectedPerRule[item.RuleID]++
		plan.SelectedIDs = append(plan.SelectedIDs, item.ID)
		plan.Batches, plan.EstimatedInputTokens, plan.PlannedOutputTokens = batches, input, output
		plan.EstimatedProviderRequests = len(batches)
	}
	return plan, nil
}

func normalizeReviewBudget(value ReviewBudget) ReviewBudget {
	defaults := DefaultReviewBudget()
	if value.MaxReviewedFindings <= 0 {
		value.MaxReviewedFindings = defaults.MaxReviewedFindings
	}
	if value.MaxProviderRequests <= 0 {
		value.MaxProviderRequests = defaults.MaxProviderRequests
	}
	if value.MaxEstimatedInputTokens <= 0 {
		value.MaxEstimatedInputTokens = defaults.MaxEstimatedInputTokens
	}
	if value.MaxOutputTokens <= 0 {
		value.MaxOutputTokens = defaults.MaxOutputTokens
	}
	return value
}

func selectedRequests(items []PlannedCandidate) []ReviewRequest {
	result := make([]ReviewRequest, len(items))
	for i := range items {
		result[i] = items[i].Request
	}
	return result
}

func planBatchTokens(ctx context.Context, batches [][]ReviewRequest, constraints BatchConstraints) (int, int) {
	input, output := 0, 0
	for _, batch := range batches {
		input += estimateBatchInputContext(ctx, batch, constraints)
		output += EstimateBatchOutputTokens(len(batch))
	}
	return input, output
}

func diverseOrder(items []finding.Finding) []finding.Finding {
	sorted := append([]finding.Finding(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if severityPriority(sorted[i].Severity) != severityPriority(sorted[j].Severity) {
			return severityPriority(sorted[i].Severity) > severityPriority(sorted[j].Severity)
		}
		if sorted[i].Confidence != sorted[j].Confidence {
			return sorted[i].Confidence > sorted[j].Confidence
		}
		if categoryPriority(sorted[i].Category) != categoryPriority(sorted[j].Category) {
			return categoryPriority(sorted[i].Category) > categoryPriority(sorted[j].Category)
		}
		if evidencePriority(sorted[i].EvidenceStrength) != evidencePriority(sorted[j].EvidenceStrength) {
			return evidencePriority(sorted[i].EvidenceStrength) > evidencePriority(sorted[j].EvidenceStrength)
		}
		if sorted[i].RuleID != sorted[j].RuleID {
			return sorted[i].RuleID < sorted[j].RuleID
		}
		return candidateLocation(sorted[i])+sorted[i].ID < candidateLocation(sorted[j])+sorted[j].ID
	})
	buckets, rules := map[string][]finding.Finding{}, []string{}
	for _, item := range sorted {
		if _, ok := buckets[item.RuleID]; !ok {
			rules = append(rules, item.RuleID)
		}
		buckets[item.RuleID] = append(buckets[item.RuleID], item)
	}
	result := make([]finding.Finding, 0, len(items))
	for round := 0; ; round++ {
		added := false
		for _, rule := range rules {
			if round < len(buckets[rule]) {
				result = append(result, buckets[rule][round])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return result
}

func categoryPriority(value finding.Category) int {
	switch value {
	case finding.CategorySecurity:
		return 5
	case finding.CategoryCorrectness:
		return 4
	case finding.CategoryPerformance:
		return 3
	case finding.CategoryArchitecture:
		return 2
	default:
		return 1
	}
}

func evidencePriority(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strong":
		return 3
	case "moderate":
		return 2
	default:
		return 1
	}
}

func severityPriority(value finding.Severity) int {
	switch value {
	case finding.SeverityCritical:
		return 5
	case finding.SeverityHigh:
		return 4
	case finding.SeverityMedium:
		return 3
	case finding.SeverityLow:
		return 2
	default:
		return 1
	}
}

func candidateLocation(item finding.Finding) string {
	if item.Location != nil {
		return item.Location.File
	}
	return strings.Join(item.Evidence.AffectedFiles, "\x00")
}
