package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
)

func adjudicationCandidate() finding.Finding {
	decision := finding.ReviewDecision{Status: finding.AdjudicationNotReviewed}
	return finding.Finding{ID: "f-1", Severity: finding.SeverityHigh, Confidence: .8, ReviewPolicy: finding.ReviewPolicyContextRequired, Adjudication: &decision}
}

func TestNormalizeDecisionDerivesVerdictFromSemanticJudgment(t *testing.T) {
	tests := []struct {
		name     string
		relation EvidenceRelation
		strength ClaimStrength
		want     finding.AdjudicationStatus
	}{
		{"supports unchanged", EvidenceSupports, ClaimStrengthUnchanged, finding.AdjudicationConfirmed},
		{"supports lower", EvidenceSupports, ClaimStrengthLower, finding.AdjudicationDowngraded},
		{"contradicts", EvidenceContradicts, "", finding.AdjudicationUnsupported},
		{"insufficient", EvidenceInsufficient, "", finding.AdjudicationUncertain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			severity, confidence := finding.SeverityHigh, .8
			if test.strength == ClaimStrengthLower {
				severity, confidence = finding.SeverityMedium, .6
			}
			decision, err := normalizeDecision(adjudicationCandidate(), ReviewResponse{ID: "f-1", EvidenceRelation: test.relation, ClaimStrength: test.strength, Severity: severity, Confidence: confidence, Reason: "bounded judgment"}, "fake", "model")
			if err != nil || decision.Status != test.want {
				t.Fatalf("decision = %#v, err = %v, want %s", decision, err, test.want)
			}
		})
	}
}

func TestNormalizeDecisionRejectsSemanticallyIrrelevantStrength(t *testing.T) {
	decision, err := NormalizeDecision(adjudicationCandidate(), ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceContradicts, ClaimStrength: ClaimStrengthUnchanged, Reason: "counterevidence exists"}, "fake", "model")
	if err != nil || decision.Status != finding.AdjudicationUncertain {
		t.Fatalf("decision = %#v, err = %v, want fail-closed UNCERTAIN", decision, err)
	}
}

func TestNormalizeDecisionIgnoresReasonWordingWhenDerivingVerdict(t *testing.T) {
	responses := []ReviewResponse{
		{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthLower, Severity: finding.SeverityMedium, Confidence: .6, Reason: "the supplied code supports a lower-impact form"},
		{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthLower, Severity: finding.SeverityMedium, Confidence: .6, Reason: "evidence does not support this hypothesis"},
	}
	for _, response := range responses {
		decision, err := normalizeDecision(adjudicationCandidate(), response, "fake", "model")
		if err != nil || decision.Status != finding.AdjudicationDowngraded {
			t.Fatalf("decision = %#v, err = %v, want DOWNGRADED solely from structured fields", decision, err)
		}
	}
}

func TestNormalizeDecisionRejectsClaimStrengthThatDisagreesWithProposedImpact(t *testing.T) {
	tests := []ReviewResponse{
		{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityMedium, Confidence: .8, Reason: "lower severity"},
		{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthLower, Severity: finding.SeverityHigh, Confidence: .8, Reason: "no actual reduction"},
	}
	for _, response := range tests {
		decision, err := normalizeDecision(adjudicationCandidate(), response, "fake", "model")
		if err != nil || decision.Status != finding.AdjudicationUncertain {
			t.Fatalf("decision = %#v, err = %v, want fail-closed UNCERTAIN", decision, err)
		}
	}
}

func TestNormalizeDecisionCapsSeverityAndConfidenceEscalation(t *testing.T) {
	decision, err := normalizeDecision(adjudicationCandidate(), ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityCritical, Confidence: .95, Reason: "claim supported"}, "fake", "model")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Severity != finding.SeverityHigh || decision.Confidence != .8 {
		t.Fatalf("decision = %#v, want static high/.8 caps", decision)
	}
}

type failingAdjudicationProvider struct{}

func (failingAdjudicationProvider) Name() string { return "failing" }
func (failingAdjudicationProvider) Review(context.Context, ReviewRequest) (ReviewResponse, error) {
	return ReviewResponse{}, errors.New("provider unavailable")
}

func TestProviderFailureLeavesCandidateNotReviewed(t *testing.T) {
	reviewer := NewTriageReviewer(failingAdjudicationProvider{}, TriageOptions{Mode: ReviewModeAll, Cache: NewAssessmentCache("")}, nil)
	got, _ := reviewer.ReviewFindings(context.Background(), []finding.Finding{adjudicationCandidate()}, nil)
	if len(got) != 1 || got[0].Adjudication == nil || got[0].Adjudication.Status != finding.AdjudicationNotReviewed {
		t.Fatalf("findings = %#v, want NOT_REVIEWED", got)
	}
	if got[0].AIReviewed {
		t.Fatal("provider failure implicitly marked candidate reviewed")
	}
}

func TestNormalizePotentialPreservesExplicitZeroConfidence(t *testing.T) {
	got := NormalizePotential(language.PotentialFinding{RuleID: "ZERO", Confidence: 0, ConfidenceSpecified: true})
	if got.Confidence != 0 {
		t.Fatalf("confidence = %v, want explicit zero", got.Confidence)
	}
}

func TestMalformedResponseCannotBecomeConfirmed(t *testing.T) {
	responses, err := NormalizeResponses(`{"assessments":[{"id":"f-1","evidence_relation":"SUPPORTS","claim_strength":"UNCHANGED","confidence":2,"reason":"bad"}]}`, []string{"f-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 || responses[0].Verdict != finding.AdjudicationUncertain || !responses[0].Malformed {
		t.Fatalf("responses = %#v, want malformed UNCERTAIN", responses)
	}
}
