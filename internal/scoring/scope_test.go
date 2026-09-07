package scoring

import (
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestCalculateScopedScoresRequiresCompleteContextForVerifiedHealth(t *testing.T) {
	static := finding.Finding{ID: "static", RuleID: "SEC-1", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: 1, ReviewPolicy: finding.ReviewPolicyStaticAuthoritative}
	contextual := finding.Finding{ID: "context", RuleID: "COR-1", Category: finding.CategoryCorrectness, Severity: finding.SeverityMedium, Confidence: .8, ReviewPolicy: finding.ReviewPolicyContextRequired}
	analysis := finding.AnalysisResult{
		Candidates: []finding.PotentialFinding{
			{Finding: static, ReviewPolicy: finding.ReviewPolicyStaticAuthoritative, Decision: finding.ReviewDecision{Status: finding.AdjudicationConfirmed}},
			{Finding: contextual, ReviewPolicy: finding.ReviewPolicyContextRequired, Decision: finding.ReviewDecision{Status: finding.AdjudicationNotReviewed}},
		},
		FinalFindings: []finding.FinalFinding{{Finding: static}},
	}

	staticRisk, verified := CalculateScoped(analysis)
	if staticRisk.Scope != ScopeStaticRisk || !staticRisk.Available || staticRisk.Scores == nil {
		t.Fatalf("static risk = %#v", staticRisk)
	}
	if verified.Scope != ScopeVerifiedHealth || verified.Available || verified.Scores != nil {
		t.Fatalf("verified health = %#v, want unavailable", verified)
	}
}

func TestCalculateScopedKeepsVerifiedHealthUnavailableForMalformedAdjudication(t *testing.T) {
	analysis := finding.AnalysisResult{Candidates: []finding.PotentialFinding{{
		Finding:      finding.Finding{ID: "malformed", RuleID: "CTX", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .8},
		ReviewPolicy: finding.ReviewPolicyContextRequired,
		Decision:     finding.ReviewDecision{Status: finding.AdjudicationUncertain, Reason: "Provider adjudication malformed: required reason is missing or blank."},
	}}}
	staticRisk, verified := CalculateScoped(analysis)
	if !staticRisk.Available || verified.Available || verified.UnavailableReason == "" {
		t.Fatalf("static=%#v verified=%#v", staticRisk, verified)
	}
}

func TestCalculateScopedScoresExcludesUnsupportedAndUsesDowngrade(t *testing.T) {
	unsupported := finding.Finding{ID: "unsupported", RuleID: "SEC-1", Category: finding.CategorySecurity, Severity: finding.SeverityCritical, Confidence: 1, ReviewPolicy: finding.ReviewPolicyContextRequired}
	downgraded := finding.Finding{ID: "down", RuleID: "COR-1", Category: finding.CategoryCorrectness, Severity: finding.SeverityLow, Confidence: .4, ReviewPolicy: finding.ReviewPolicyContextRequired}
	analysis := finding.AnalysisResult{
		Candidates: []finding.PotentialFinding{
			{Finding: unsupported, ReviewPolicy: finding.ReviewPolicyContextRequired, Decision: finding.ReviewDecision{Status: finding.AdjudicationUnsupported}},
			{Finding: downgraded, ReviewPolicy: finding.ReviewPolicyContextRequired, Decision: finding.ReviewDecision{Status: finding.AdjudicationDowngraded, Severity: finding.SeverityLow, Confidence: .4}},
		},
		FinalFindings: []finding.FinalFinding{{Finding: downgraded}},
	}

	_, verified := CalculateScoped(analysis)
	if !verified.Available || verified.Scores == nil {
		t.Fatalf("verified health = %#v, want available", verified)
	}
	want := Calculate([]finding.FinalFinding{{Finding: downgraded}})
	if verified.Scores.Overall != want.Overall {
		t.Fatalf("verified overall = %v, want %v", verified.Scores.Overall, want.Overall)
	}
}
