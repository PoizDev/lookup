package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

var trailingCommaRegex = regexp.MustCompile(`,(\s*[\]}])`)

// NormalizeResponses validates structured provider content and restores input order.
func NormalizeResponses(content string, expectedIDs []string) ([]ReviewResponse, error) {
	if len(expectedIDs) == 0 {
		return nil, fmt.Errorf("normalize responses: no expected finding IDs")
	}

	cleaned, err := unwrapJSONFence(content)
	if err != nil {
		return nil, err
	}

	// Robustly clean trailing commas which often cause "invalid character '}' after array element"
	cleaned = trailingCommaRegex.ReplaceAllString(cleaned, "$1")

	var raw json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(cleaned))
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode structured review: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode structured review: trailing JSON value")
		}
		return nil, fmt.Errorf("decode structured review: %w", err)
	}

	var responses []ReviewResponse
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var envelope struct {
			Assessments []ReviewResponse `json:"assessments"`
		}
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return nil, fmt.Errorf("decode structured review object: %w", err)
		}
		if envelope.Assessments != nil {
			responses = envelope.Assessments
		} else {
			if len(expectedIDs) != 1 {
				return nil, fmt.Errorf("decode structured review: object response for %d findings", len(expectedIDs))
			}
			var response ReviewResponse
			if err := json.Unmarshal(trimmed, &response); err != nil {
				return nil, fmt.Errorf("decode structured review object: %w", err)
			}
			responses = []ReviewResponse{response}
		}
	} else if err := json.Unmarshal(trimmed, &responses); err != nil {
		return nil, fmt.Errorf("decode structured review array: %w", err)
	}
	return normalizeParsedResponses(responses, expectedIDs)
}

func normalizeParsedResponses(responses []ReviewResponse, expectedIDs []string) ([]ReviewResponse, error) {
	if len(responses) != len(expectedIDs) {
		return nil, fmt.Errorf("structured review returned %d responses for %d findings", len(responses), len(expectedIDs))
	}

	byID := make(map[string]ReviewResponse, len(responses))
	for _, response := range responses {
		if response.ID == "" {
			return nil, fmt.Errorf("structured review contains an empty finding ID")
		}
		if _, exists := byID[response.ID]; exists {
			return nil, fmt.Errorf("structured review contains duplicate finding ID %q", response.ID)
		}
		response = normalizeResponseFields(response)
		byID[response.ID] = response
	}

	ordered := make([]ReviewResponse, len(expectedIDs))
	for index, id := range expectedIDs {
		response, exists := byID[id]
		if !exists {
			return nil, fmt.Errorf("structured review is missing finding ID %q", id)
		}
		ordered[index] = response
	}
	return ordered, nil
}

func normalizeResponseFields(response ReviewResponse) ReviewResponse {
	if response.Malformed {
		return malformedResponse(response, response.MalformedReason)
	}
	if response.Severity != "" {
		severity, err := normalizeSeverity(response.Severity)
		if err != nil {
			return malformedResponse(response, err.Error())
		}
		response.Severity = severity
	}
	response.EvidenceRelation = EvidenceRelation(strings.ToUpper(strings.TrimSpace(string(response.EvidenceRelation))))
	response.ClaimStrength = ClaimStrength(strings.ToUpper(strings.TrimSpace(string(response.ClaimStrength))))
	status, validationReason := deriveAdjudication(response.EvidenceRelation, response.ClaimStrength)
	if validationReason != "" {
		return malformedResponse(response, validationReason)
	}
	response.Verdict = status
	if math.IsNaN(response.Confidence) || math.IsInf(response.Confidence, 0) || response.Confidence < 0 || response.Confidence > 1 {
		return malformedResponse(response, "confidence must be between 0 and 1")
	}
	if response.decodedJSON && status != finding.AdjudicationConfirmed && status != finding.AdjudicationDowngraded && (response.severityPresent || response.confidencePresent) {
		return malformedResponse(response, "severity and confidence must be omitted unless evidence_relation is SUPPORTS")
	}
	if response.decodedJSON && (status == finding.AdjudicationConfirmed || status == finding.AdjudicationDowngraded) && (!response.severityPresent || !response.confidencePresent) {
		return malformedResponse(response, "SUPPORTS requires severity and confidence")
	}
	if status == finding.AdjudicationUnsupported || status == finding.AdjudicationUncertain {
		response.Confidence = 0
	}
	if strings.TrimSpace(response.Reason) == "" {
		return malformedResponse(response, "required reason is missing or blank")
	}
	response.IsRealIssue = response.Verdict == finding.AdjudicationConfirmed || response.Verdict == finding.AdjudicationDowngraded
	return response
}

func malformedResponse(response ReviewResponse, validationReason string) ReviewResponse {
	response.Verdict = finding.AdjudicationUncertain
	response.IsRealIssue = false
	response.Severity = ""
	response.Confidence = 0
	response.Reason = malformedAdjudicationReason(validationReason)
	response.Recommendation = ""
	response.EvidenceIDs = nil
	response.Malformed = true
	response.MalformedReason = validationReason
	return response
}

func malformedAdjudicationReason(validationReason string) string {
	return "Provider adjudication malformed: " + strings.TrimSuffix(strings.TrimSpace(validationReason), ".") + "."
}

// deriveAdjudication is the single authoritative mapping from provider-owned
// semantic judgments to Lookup's canonical adjudication state.
func deriveAdjudication(relation EvidenceRelation, strength ClaimStrength) (finding.AdjudicationStatus, string) {
	relation = EvidenceRelation(strings.ToUpper(strings.TrimSpace(string(relation))))
	strength = ClaimStrength(strings.ToUpper(strings.TrimSpace(string(strength))))
	switch relation {
	case EvidenceSupports:
		switch strength {
		case ClaimStrengthUnchanged:
			return finding.AdjudicationConfirmed, ""
		case ClaimStrengthLower:
			return finding.AdjudicationDowngraded, ""
		default:
			return finding.AdjudicationUncertain, fmt.Sprintf("SUPPORTS requires claim_strength UNCHANGED or LOWER, got %q", strength)
		}
	case EvidenceContradicts:
		if strength != "" {
			return finding.AdjudicationUncertain, "claim_strength must be omitted when evidence_relation is CONTRADICTS"
		}
		return finding.AdjudicationUnsupported, ""
	case EvidenceInsufficient:
		if strength != "" {
			return finding.AdjudicationUncertain, "claim_strength must be omitted when evidence_relation is INSUFFICIENT"
		}
		return finding.AdjudicationUncertain, ""
	default:
		return finding.AdjudicationUncertain, fmt.Sprintf("unknown evidence_relation %q", relation)
	}
}

// NormalizeDecision is the authoritative boundary between provider data and
// core analysis state. The original static candidate remains the upper bound.
func NormalizeDecision(candidate finding.Finding, response ReviewResponse, provider, model string) (finding.ReviewDecision, error) {
	return normalizeDecision(candidate, response, provider, model)
}

func normalizeDecision(candidate finding.Finding, response ReviewResponse, provider, model string) (finding.ReviewDecision, error) {
	if response.Malformed {
		return malformedDecision(candidate, provider, model, response.MalformedReason), nil
	}
	status, validationReason := deriveAdjudication(response.EvidenceRelation, response.ClaimStrength)
	if validationReason != "" {
		return malformedDecision(candidate, provider, model, validationReason), nil
	}
	if strings.TrimSpace(response.Reason) == "" {
		return finding.ReviewDecision{}, fmt.Errorf("finding %q: adjudication requires reason", candidate.ID)
	}
	if math.IsNaN(response.Confidence) || math.IsInf(response.Confidence, 0) || response.Confidence < 0 || response.Confidence > 1 {
		return finding.ReviewDecision{}, fmt.Errorf("finding %q: confidence must be between 0 and 1", candidate.ID)
	}
	if response.EvidenceRelation == EvidenceSupports {
		lowered := response.Severity != "" && severityRank(response.Severity) < severityRank(candidate.Severity) || response.Confidence < candidate.Confidence
		if response.ClaimStrength == ClaimStrengthUnchanged && lowered {
			return malformedDecision(candidate, provider, model, "claim_strength UNCHANGED conflicts with lowered severity or confidence"), nil
		}
		if response.ClaimStrength == ClaimStrengthLower && !lowered {
			return malformedDecision(candidate, provider, model, "claim_strength LOWER requires lower severity or confidence"), nil
		}
	}
	severity := response.Severity
	if severity == "" {
		severity = candidate.Severity
	}
	if severityRank(severity) > severityRank(candidate.Severity) {
		severity = candidate.Severity
	}
	confidence := response.Confidence
	if confidence > candidate.Confidence {
		confidence = candidate.Confidence
	}
	return finding.ReviewDecision{Status: status, Source: finding.DecisionSourceAI, Severity: severity, Confidence: confidence, Reason: response.Reason, Recommendation: response.Recommendation, Provider: provider, Model: model, EvidenceIDs: append([]string(nil), response.EvidenceIDs...)}, nil
}

func malformedDecision(candidate finding.Finding, provider, model, validationReason string) finding.ReviewDecision {
	return finding.ReviewDecision{
		Status: finding.AdjudicationUncertain, Source: finding.DecisionSourceAI,
		Severity: candidate.Severity, Confidence: candidate.Confidence,
		Reason: malformedAdjudicationReason(validationReason), Provider: provider, Model: model,
	}
}

// NormalizeDecisionWithPacket validates that provider evidence references were
// supplied by Lookup. Final verdicts fail closed; non-final verdicts retain only
// valid references for audit.
func NormalizeDecisionWithPacket(packet reviewcontext.ReviewPacket, response ReviewResponse, provider, model string) (finding.ReviewDecision, error) {
	decision, err := normalizeDecision(packet.Candidate.Finding, response, provider, model)
	if err != nil {
		return finding.ReviewDecision{}, err
	}
	known := packet.EvidenceIDs()
	valid := make([]string, 0, len(response.EvidenceIDs))
	seen := map[string]bool{}
	invalid := false
	for _, id := range response.EvidenceIDs {
		if _, ok := known[id]; !ok {
			invalid = true
			continue
		}
		if !seen[id] {
			seen[id] = true
			valid = append(valid, id)
		}
	}
	if decision.Status == finding.AdjudicationConfirmed || decision.Status == finding.AdjudicationDowngraded || decision.Status == finding.AdjudicationUnsupported {
		if invalid || len(valid) == 0 {
			return malformedDecision(packet.Candidate.Finding, provider, model, "decisive adjudication requires valid supplied evidence IDs"), nil
		}
	}
	decision.EvidenceIDs = valid
	return decision, nil
}

func severityRank(value finding.Severity) int {
	switch value {
	case finding.SeverityCritical:
		return 5
	case finding.SeverityHigh:
		return 4
	case finding.SeverityMedium:
		return 3
	case finding.SeverityLow:
		return 2
	case finding.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func unwrapJSONFence(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", fmt.Errorf("provider response contains no review content")
	}
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed, nil
	}
	firstNewline := strings.IndexByte(trimmed, '\n')
	if firstNewline < 0 || !strings.HasSuffix(trimmed, "```") {
		return "", fmt.Errorf("decode structured review: malformed JSON fence")
	}
	header := strings.TrimSpace(trimmed[3:firstNewline])
	if header != "" && !strings.EqualFold(header, "json") {
		return "", fmt.Errorf("decode structured review: unsupported fence %q", header)
	}
	return strings.TrimSpace(trimmed[firstNewline+1 : len(trimmed)-3]), nil
}

func normalizeSeverity(value finding.Severity) (finding.Severity, error) {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "critical":
		return finding.SeverityCritical, nil
	case "high":
		return finding.SeverityHigh, nil
	case "medium":
		return finding.SeverityMedium, nil
	case "low":
		return finding.SeverityLow, nil
	case "info":
		return finding.SeverityInfo, nil
	default:
		return "", fmt.Errorf("unknown severity %q", value)
	}
}
