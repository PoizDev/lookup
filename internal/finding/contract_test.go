package finding

import "testing"

func contractCandidate(policy ReviewPolicy) PotentialFinding {
	return PotentialFinding{
		Finding:      Finding{ID: "candidate-1", Severity: SeverityHigh, Confidence: .8},
		Observation:  "A single-value type assertion exists.",
		Hypothesis:   "The assertion may panic if its dynamic type invariant is violated.",
		ReviewPolicy: policy,
	}
}

func TestInvalidConfirmedDecisionFailsClosed(t *testing.T) {
	for name, decision := range map[string]ReviewDecision{
		"unknown severity":    {Status: AdjudicationConfirmed, Source: DecisionSourceAI, Severity: "Extreme", Confidence: .5, Reason: "claimed"},
		"missing reason":      {Status: AdjudicationConfirmed, Source: DecisionSourceAI, Severity: SeverityMedium, Confidence: .5},
		"unknown status":      {Status: "MAYBE", Source: DecisionSourceAI, Severity: SeverityMedium, Confidence: .5, Reason: "claimed"},
		"negative confidence": {Status: AdjudicationConfirmed, Source: DecisionSourceAI, Severity: SeverityMedium, Confidence: -.1, Reason: "claimed"},
		"non-AI confirmation": {Status: AdjudicationConfirmed, Source: DecisionSourceStatic, Severity: SeverityMedium, Confidence: .5, Reason: "claimed"},
	} {
		t.Run(name, func(t *testing.T) {
			result := Adjudicate(contractCandidate(ReviewPolicyContextRequired), decision)
			if len(result.FinalFindings) != 0 || result.Candidates[0].Decision.Status != AdjudicationUncertain {
				t.Fatalf("result = %#v, want fail-closed UNCERTAIN", result)
			}
		})
	}
}

func TestUnknownReviewPolicyFailsClosed(t *testing.T) {
	candidate := contractCandidate(ReviewPolicy("MAGIC"))
	result := Adjudicate(candidate, ReviewDecision{Status: AdjudicationConfirmed, Source: DecisionSourceAI, Severity: SeverityHigh, Confidence: .5, Reason: "claimed"})
	if len(result.FinalFindings) != 0 || result.Candidates[0].Decision.Status != AdjudicationUncertain {
		t.Fatalf("result = %#v, want fail-closed UNCERTAIN", result)
	}
}

func TestStaticAuthoritativeCandidateBecomesFinalWithoutAI(t *testing.T) {
	result := Adjudicate(contractCandidate(ReviewPolicyStaticAuthoritative), ReviewDecision{})
	if len(result.FinalFindings) != 1 {
		t.Fatalf("final findings = %d, want 1", len(result.FinalFindings))
	}
	if result.Candidates[0].Decision.Status != AdjudicationConfirmed || result.Candidates[0].Decision.Source != DecisionSourceStatic {
		t.Fatalf("decision = %#v, want statically confirmed", result.Candidates[0].Decision)
	}
}

func TestMalformedStaticAuthoritativeCandidateFailsClosed(t *testing.T) {
	candidate := contractCandidate(ReviewPolicyStaticAuthoritative)
	candidate.Severity = "Extreme"
	result := Adjudicate(candidate, ReviewDecision{})
	if len(result.FinalFindings) != 0 || result.Candidates[0].Decision.Status != AdjudicationUncertain {
		t.Fatalf("result = %#v, want fail-closed UNCERTAIN", result)
	}
}

func TestContextCandidateConfirmedBecomesFinal(t *testing.T) {
	decision := ReviewDecision{Status: AdjudicationConfirmed, Source: DecisionSourceAI, Severity: SeverityHigh, Confidence: .7, Reason: "Context supports the hypothesis."}
	result := Adjudicate(contractCandidate(ReviewPolicyContextRequired), decision)
	if len(result.FinalFindings) != 1 || result.FinalFindings[0].Severity != SeverityHigh {
		t.Fatalf("final findings = %#v, want confirmed high finding", result.FinalFindings)
	}
}

func TestContextCandidateDowngradedBecomesBoundedFinal(t *testing.T) {
	decision := ReviewDecision{Status: AdjudicationDowngraded, Source: DecisionSourceAI, Severity: SeverityMedium, Confidence: .5, Reason: "Impact is locally contained."}
	result := Adjudicate(contractCandidate(ReviewPolicyContextRequired), decision)
	if len(result.FinalFindings) != 1 || result.FinalFindings[0].Severity != SeverityMedium || result.FinalFindings[0].Confidence != .5 {
		t.Fatalf("final findings = %#v, want downgraded medium/.5 finding", result.FinalFindings)
	}
}

func TestNonConfirmedContextCandidatesRemainInAuditOnly(t *testing.T) {
	for _, status := range []AdjudicationStatus{AdjudicationUnsupported, AdjudicationUncertain, AdjudicationNotReviewed} {
		t.Run(string(status), func(t *testing.T) {
			result := Adjudicate(contractCandidate(ReviewPolicyContextRequired), ReviewDecision{Status: status})
			if len(result.Candidates) != 1 || result.Candidates[0].Decision.Status != status {
				t.Fatalf("audit candidates = %#v, want retained %s candidate", result.Candidates, status)
			}
			if len(result.FinalFindings) != 0 {
				t.Fatalf("final findings = %#v, want none", result.FinalFindings)
			}
		})
	}
}
