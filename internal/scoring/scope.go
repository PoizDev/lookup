package scoring

import "github.com/poizdev/lookup/internal/finding"

type Scope string

const (
	ScopeStaticRisk     Scope = "STATIC_RISK"
	ScopeVerifiedHealth Scope = "VERIFIED_HEALTH"
)

type Result struct {
	Scope             Scope   `json:"scope"`
	Available         bool    `json:"available"`
	Scores            *Scores `json:"scores,omitempty"`
	UnavailableReason string  `json:"unavailable_reason,omitempty"`
}

func CalculateScoped(analysis finding.AnalysisResult) (Result, Result) {
	staticInputs := make([]finding.FinalFinding, 0, len(analysis.Candidates))
	complete := true
	for _, candidate := range analysis.Candidates {
		staticInputs = append(staticInputs, finding.FinalFinding{Finding: candidate.Finding})
		if candidate.ReviewPolicy != finding.ReviewPolicyStaticAuthoritative &&
			(candidate.Decision.Status == finding.AdjudicationUncertain || candidate.Decision.Status == finding.AdjudicationNotReviewed || candidate.Decision.Status == "") {
			complete = false
		}
	}
	staticScores := Calculate(staticInputs)
	staticRisk := Result{Scope: ScopeStaticRisk, Available: true, Scores: &staticScores}
	verified := Result{Scope: ScopeVerifiedHealth}
	if !complete {
		verified.UnavailableReason = "contextual verification incomplete"
		return staticRisk, verified
	}
	verifiedScores := Calculate(analysis.FinalFindings)
	verified.Available = true
	verified.Scores = &verifiedScores
	return staticRisk, verified
}
