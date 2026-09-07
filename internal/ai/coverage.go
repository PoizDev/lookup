package ai

import (
	"fmt"

	"github.com/poizdev/lookup/internal/finding"
)

type ReviewSummary struct {
	TotalStaticCandidates         int  `json:"total_static_candidates"`
	StaticAuthoritativeCandidates int  `json:"static_authoritative_candidates"`
	ContextualCandidates          int  `json:"contextual_candidates"`
	ReviewEligibleContextual      int  `json:"review_eligible_contextual_candidates"`
	SelectedForAIReview           int  `json:"selected_for_ai_review"`
	ProviderReviewed              int  `json:"provider_reviewed"`
	Confirmed                     int  `json:"confirmed"`
	Downgraded                    int  `json:"downgraded"`
	Unsupported                   int  `json:"unsupported"`
	Uncertain                     int  `json:"uncertain"`
	NotReviewed                   int  `json:"not_reviewed"`
	ContextInsufficient           int  `json:"context_insufficient"`
	BudgetSkipped                 int  `json:"budget_skipped"`
	CacheHits                     int  `json:"cache_hits"`
	ContextPacketsBuilt           int  `json:"context_packets_built"`
	TruncatedPackets              int  `json:"truncated_packets"`
	ProviderFailures              int  `json:"provider_failures"`
	BudgetExhausted               bool `json:"budget_exhausted"`
}

func SummarizeReview(candidates []finding.PotentialFinding, plan ReviewPlan, runtime ReviewStats) ReviewSummary {
	s := ReviewSummary{TotalStaticCandidates: len(candidates), ReviewEligibleContextual: plan.EligibleCandidates, SelectedForAIReview: len(plan.SelectedIDs), ProviderReviewed: runtime.ProviderReviews, CacheHits: runtime.CacheHits, ContextPacketsBuilt: plan.ContextPacketsBuilt, TruncatedPackets: plan.TruncatedPackets, ProviderFailures: runtime.Failures}
	for _, candidate := range candidates {
		if candidate.ReviewPolicy == finding.ReviewPolicyStaticAuthoritative {
			s.StaticAuthoritativeCandidates++
			continue
		}
		s.ContextualCandidates++
		switch candidate.Decision.Status {
		case finding.AdjudicationConfirmed:
			s.Confirmed++
		case finding.AdjudicationDowngraded:
			s.Downgraded++
		case finding.AdjudicationUnsupported:
			s.Unsupported++
		case finding.AdjudicationUncertain:
			s.Uncertain++
		default:
			s.NotReviewed++
		}
	}
	for _, skipped := range plan.Skipped {
		switch skipped.Reason {
		case SkipBudgetExhausted:
			s.BudgetSkipped++
			s.BudgetExhausted = true
		case SkipContextInsufficient:
			s.ContextInsufficient++
		}
	}
	for _, skipped := range runtime.RuntimeSkipped {
		if skipped.Reason == SkipBudgetExhausted {
			s.BudgetSkipped++
			s.BudgetExhausted = true
			if s.ProviderFailures > 0 {
				s.ProviderFailures--
			}
		}
	}
	return s
}

func (s ReviewSummary) Validate() error {
	if s.TotalStaticCandidates != s.StaticAuthoritativeCandidates+s.ContextualCandidates {
		return fmt.Errorf("static candidate partition is inconsistent")
	}
	if s.ContextualCandidates != s.Confirmed+s.Downgraded+s.Unsupported+s.Uncertain+s.NotReviewed {
		return fmt.Errorf("contextual adjudication partition is inconsistent")
	}
	if s.ProviderReviewed+s.CacheHits > s.SelectedForAIReview {
		return fmt.Errorf("reviewed candidates exceed selected candidates")
	}
	if s.BudgetSkipped > s.NotReviewed {
		return fmt.Errorf("budget-skipped candidates exceed not-reviewed candidates")
	}
	return nil
}
