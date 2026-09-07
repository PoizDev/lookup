package ai

import (
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestSummarizeReviewKeepsCountsConsistent(t *testing.T) {
	items := []finding.PotentialFinding{
		{Finding: finding.Finding{ID: "static"}, ReviewPolicy: finding.ReviewPolicyStaticAuthoritative, Decision: finding.ReviewDecision{Status: finding.AdjudicationConfirmed}},
		{Finding: finding.Finding{ID: "confirmed"}, ReviewPolicy: finding.ReviewPolicyContextRequired, Decision: finding.ReviewDecision{Status: finding.AdjudicationConfirmed}},
		{Finding: finding.Finding{ID: "not"}, ReviewPolicy: finding.ReviewPolicyContextRequired, Decision: finding.ReviewDecision{Status: finding.AdjudicationNotReviewed}},
	}
	plan := ReviewPlan{EligibleCandidates: 2, SelectedIDs: []string{"confirmed"}, Skipped: []SkippedCandidate{{CandidateID: "not", Reason: SkipBudgetExhausted}}}
	summary := SummarizeReview(items, plan, ReviewStats{ProviderReviews: 1, PacketsBuilt: 1})
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	if summary.TotalStaticCandidates != 3 || summary.StaticAuthoritativeCandidates != 1 || summary.ContextualCandidates != 2 || summary.Confirmed != 1 || summary.NotReviewed != 1 || summary.BudgetSkipped != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
