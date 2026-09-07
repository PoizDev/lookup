package main

import (
	"context"
	"testing"

	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
)

func TestAIOffRetainsContextCandidateWithoutFinalizingIt(t *testing.T) {
	items := reviewFindings(context.Background(), config.AIConfig{NoAI: true}, []language.PotentialFinding{{
		RuleID: "CTX-1", Severity: "High", Confidence: .8, Title: "observed pattern", Description: "possible defect",
	}}, nil, nil, nil)
	result := finalizeFindings(items)
	if len(result.Candidates) != 1 || result.Candidates[0].Decision.Status != finding.AdjudicationNotReviewed {
		t.Fatalf("candidates = %#v, want retained NOT_REVIEWED candidate", result.Candidates)
	}
	if len(result.FinalFindings) != 0 {
		t.Fatalf("final findings = %#v, want none", result.FinalFindings)
	}
}

func TestFinalizeFindingsExcludesUnsupportedAndIncludesConfirmed(t *testing.T) {
	unsupported := aiReviewedFinding("unsupported", finding.AdjudicationUnsupported)
	confirmed := aiReviewedFinding("confirmed", finding.AdjudicationConfirmed)
	result := finalizeFindings([]finding.Finding{unsupported, confirmed})
	if len(result.Candidates) != 2 || len(result.FinalFindings) != 1 || result.FinalFindings[0].ID != "confirmed" {
		t.Fatalf("analysis result = %#v", result)
	}
}

func aiReviewedFinding(id string, status finding.AdjudicationStatus) finding.Finding {
	decision := finding.ReviewDecision{Status: status, Source: finding.DecisionSourceAI, Severity: finding.SeverityMedium, Confidence: .6, Reason: "reviewed"}
	return finding.Finding{ID: id, Severity: finding.SeverityHigh, Confidence: .8, Observation: "fact", Hypothesis: "risk", ReviewPolicy: finding.ReviewPolicyContextRequired, Adjudication: &decision}
}
