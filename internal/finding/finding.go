// Package finding defines the normalized results produced by Lookup.
package finding

import (
	"math"
	"strings"
)

// Severity describes the impact of a finding.
type Severity string

const (
	SeverityCritical Severity = "Critical"
	SeverityHigh     Severity = "High"
	SeverityMedium   Severity = "Medium"
	SeverityLow      Severity = "Low"
	SeverityInfo     Severity = "Info"
)

// Category describes the concern represented by a finding.
type Category string

const (
	CategorySecurity        Category = "Security"
	CategoryPerformance     Category = "Performance"
	CategoryArchitecture    Category = "Architecture"
	CategoryCorrectness     Category = "Correctness"
	CategoryMaintainability Category = "Maintainability"
)

// ReviewPolicy declares whether a deterministic observation is itself
// authoritative or whether its risk hypothesis requires contextual judgment.
type ReviewPolicy string

const (
	ReviewPolicyContextRequired     ReviewPolicy = "CONTEXT_REQUIRED"
	ReviewPolicyStaticAuthoritative ReviewPolicy = "STATIC_AUTHORITATIVE"
)

// AdjudicationStatus is the bounded judgment applied to a potential finding.
type AdjudicationStatus string

const (
	AdjudicationConfirmed   AdjudicationStatus = "CONFIRMED"
	AdjudicationDowngraded  AdjudicationStatus = "DOWNGRADED"
	AdjudicationUnsupported AdjudicationStatus = "UNSUPPORTED"
	AdjudicationUncertain   AdjudicationStatus = "UNCERTAIN"
	AdjudicationNotReviewed AdjudicationStatus = "NOT_REVIEWED"
)

type DecisionSource string

const (
	DecisionSourceStatic DecisionSource = "STATIC"
	DecisionSourceAI     DecisionSource = "AI"
)

// ReviewDecision records adjudication without replacing deterministic evidence.
type ReviewDecision struct {
	Status         AdjudicationStatus `json:"status"`
	Source         DecisionSource     `json:"source,omitempty"`
	Severity       Severity           `json:"severity,omitempty"`
	Confidence     float64            `json:"confidence,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	Recommendation string             `json:"recommendation,omitempty"`
	Provider       string             `json:"provider,omitempty"`
	Model          string             `json:"model,omitempty"`
	EvidenceIDs    []string           `json:"evidence_ids,omitempty"`
}

// Evidence contains the source context supporting a finding.
type Evidence struct {
	AffectedFiles   []string       `json:"affected_files"`
	AffectedSymbols []string       `json:"affected_symbols"`
	CodeSnippet     string         `json:"code_snippet"`
	CallChain       []string       `json:"call_chain"`
	Steps           []EvidenceStep `json:"steps,omitempty"`
}

type EvidenceStep struct {
	Kind          string `json:"kind"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	Column        int    `json:"column,omitempty"`
	Expression    string `json:"expression,omitempty"`
	Message       string `json:"message"`
	RelatedSymbol string `json:"related_symbol,omitempty"`
}

// Location is the deterministic primary analyzer location. AffectedFiles is
// retained for backward compatibility with existing JSON consumers.
type Location struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// Finding is the normalized representation consumed by review and reporting.
type Finding struct {
	ID               string          `json:"id"`
	RuleID           string          `json:"rule_id"`
	Category         Category        `json:"category"`
	Severity         Severity        `json:"severity"`
	Confidence       float64         `json:"confidence"`
	EvidenceStrength string          `json:"evidence_strength,omitempty"`
	Title            string          `json:"title"`
	Reason           string          `json:"reason"`
	Recommendation   string          `json:"recommendation"`
	Observation      string          `json:"observation,omitempty"`
	Hypothesis       string          `json:"hypothesis,omitempty"`
	ReviewPolicy     ReviewPolicy    `json:"review_policy,omitempty"`
	Adjudication     *ReviewDecision `json:"adjudication,omitempty"`
	Location         *Location       `json:"location,omitempty"`
	Evidence         Evidence        `json:"evidence"`
	AIReviewed       bool            `json:"ai_reviewed"`
	AIReview         *AIAssessment   `json:"ai_assessment,omitempty"`
}

// PotentialFinding is a normalized deterministic observation plus a bounded
// hypothesis. Detection is not judgment.
type PotentialFinding struct {
	Finding      `json:"finding"`
	Observation  string         `json:"observation"`
	Hypothesis   string         `json:"hypothesis"`
	ReviewPolicy ReviewPolicy   `json:"review_policy"`
	Decision     ReviewDecision `json:"decision"`
}

// FinalFinding is an authoritative finding accepted by the certainty gate.
// AI adjudicates bounded static candidates; it does not independently search
// the repository for new findings.
type FinalFinding struct {
	Finding `json:"finding"`
}

// AnalysisResult retains the complete audit history separately from the final
// findings accepted by deterministic scoring.
type AnalysisResult struct {
	Candidates    []PotentialFinding `json:"candidates"`
	FinalFindings []FinalFinding     `json:"final_findings"`
}

// Adjudicate applies one decision to one candidate and derives final state.
func Adjudicate(candidate PotentialFinding, decision ReviewDecision) AnalysisResult {
	if candidate.ReviewPolicy == "" {
		candidate.ReviewPolicy = ReviewPolicyContextRequired
	}
	if candidate.ReviewPolicy != ReviewPolicyContextRequired && candidate.ReviewPolicy != ReviewPolicyStaticAuthoritative {
		decision = ReviewDecision{Status: AdjudicationUncertain, Severity: candidate.Severity, Confidence: candidate.Confidence, Reason: "Invalid review policy."}
	} else if severityRank(candidate.Severity) == 0 || math.IsNaN(candidate.Confidence) || math.IsInf(candidate.Confidence, 0) || candidate.Confidence < 0 || candidate.Confidence > 1 {
		decision = ReviewDecision{Status: AdjudicationUncertain, Severity: candidate.Severity, Confidence: candidate.Confidence, Reason: "Invalid deterministic candidate bounds."}
	} else if candidate.ReviewPolicy == ReviewPolicyStaticAuthoritative {
		decision = ReviewDecision{Status: AdjudicationConfirmed, Source: DecisionSourceStatic, Severity: candidate.Severity, Confidence: candidate.Confidence}
	} else {
		if !validRawDecision(decision) {
			decision = ReviewDecision{Status: AdjudicationUncertain, Source: decision.Source, Severity: candidate.Severity, Confidence: candidate.Confidence, Reason: "Invalid adjudication result."}
		} else {
			decision = boundDecision(candidate, decision)
		}
	}
	candidate.Decision = decision
	candidate.Finding.Observation = candidate.Observation
	candidate.Finding.Hypothesis = candidate.Hypothesis
	candidate.Finding.ReviewPolicy = candidate.ReviewPolicy
	candidate.Finding.Adjudication = &candidate.Decision
	result := AnalysisResult{Candidates: []PotentialFinding{candidate}}
	if decision.Status != AdjudicationConfirmed && decision.Status != AdjudicationDowngraded {
		return result
	}
	final := candidate.Finding
	if decision.Severity != "" {
		final.Severity = decision.Severity
	}
	final.Confidence = decision.Confidence
	if decision.Reason != "" {
		final.Reason = decision.Reason
	}
	if decision.Recommendation != "" {
		final.Recommendation = decision.Recommendation
	}
	result.FinalFindings = []FinalFinding{{Finding: final}}
	return result
}

func validRawDecision(decision ReviewDecision) bool {
	if decision.Status == "" {
		return true
	}
	switch decision.Status {
	case AdjudicationConfirmed, AdjudicationDowngraded, AdjudicationUnsupported, AdjudicationUncertain, AdjudicationNotReviewed:
	default:
		return false
	}
	if math.IsNaN(decision.Confidence) || math.IsInf(decision.Confidence, 0) || decision.Confidence < 0 || decision.Confidence > 1 {
		return false
	}
	if decision.Severity != "" && severityRank(decision.Severity) == 0 {
		return false
	}
	if decision.Status == AdjudicationConfirmed || decision.Status == AdjudicationDowngraded {
		return decision.Source == DecisionSourceAI && strings.TrimSpace(decision.Reason) != ""
	}
	return true
}

func boundDecision(candidate PotentialFinding, decision ReviewDecision) ReviewDecision {
	if decision.Status == "" {
		decision.Status = AdjudicationNotReviewed
	}
	if decision.Severity == "" {
		decision.Severity = candidate.Severity
	} else if severityRank(decision.Severity) > severityRank(candidate.Severity) {
		decision.Severity = candidate.Severity
	}
	if decision.Confidence > candidate.Confidence {
		decision.Confidence = candidate.Confidence
	}
	return decision
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// AIAssessment is optional, non-authoritative verification metadata. Static
// severity, confidence, evidence, and scoring remain deterministic.
type AIAssessment struct {
	EvidenceRelation string    `json:"evidence_relation,omitempty"`
	ClaimStrength    string    `json:"claim_strength,omitempty"`
	Verdict          AIVerdict `json:"verdict,omitempty"`
	IsRealIssue      bool      `json:"is_real_issue"`
	Severity         Severity  `json:"severity"`
	Confidence       float64   `json:"confidence"`
	Reason           string    `json:"reason,omitempty"`
	Recommendation   string    `json:"recommendation,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	EvidenceIDs      []string  `json:"evidence_ids,omitempty"`
	// Malformed is internal provenance used to prevent an invalid provider
	// decision from being cached as an ordinary adjudication.
	Malformed bool `json:"-"`
}

type AIVerdict = AdjudicationStatus

const (
	AIVerdictLikelyReal = AdjudicationConfirmed
	AIVerdictDisputed   = AdjudicationUnsupported
	AIVerdictUncertain  = AdjudicationUncertain
)
