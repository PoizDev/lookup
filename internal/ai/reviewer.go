package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
)

const maxStoredSnippetLines = 20

// Reviewer verifies deterministic findings while preserving them on AI failure.
type Reviewer struct {
	provider  Provider
	batchSize int
	warn      func(error)
}

// NewReviewer constructs an AI review orchestrator.
func NewReviewer(provider Provider, batchSize int, warn func(error)) *Reviewer {
	if batchSize <= 0 {
		batchSize = 1
	}
	return &Reviewer{provider: provider, batchSize: batchSize, warn: warn}
}

// NormalizePotential converts analyzer output into the durable finding model.
func NormalizePotential(potential language.PotentialFinding) finding.Finding {
	files := []string(nil)
	if potential.File != "" {
		files = []string{potential.File}
	}
	confidence := potential.Confidence
	if confidence == 0 && !potential.ConfidenceSpecified {
		confidence = 1
	}
	confidence = min(max(confidence, 0), 1)
	steps := make([]finding.EvidenceStep, len(potential.EvidenceSteps))
	for index, step := range potential.EvidenceSteps {
		steps[index] = finding.EvidenceStep{Kind: step.Kind, File: step.File, Line: step.Line, Column: step.Column, Expression: step.Expression, Message: step.Message, RelatedSymbol: step.RelatedSymbol}
	}
	var primaryLocation *finding.Location
	if potential.File != "" {
		primaryLocation = &finding.Location{File: potential.File, StartLine: potential.StartLine, EndLine: potential.EndLine}
	}
	observation := potential.Observation
	if observation == "" {
		observation = potential.Title
	}
	hypothesis := potential.Hypothesis
	if hypothesis == "" {
		hypothesis = potential.Description
	}
	policy := finding.ReviewPolicy(potential.ReviewPolicy)
	if policy == "" {
		policy = finding.ReviewPolicyContextRequired
	}
	decision := finding.ReviewDecision{Status: finding.AdjudicationNotReviewed}
	item := finding.Finding{
		ID:               findingID(potential),
		RuleID:           potential.RuleID,
		Category:         finding.Category(potential.Category),
		Severity:         finding.Severity(potential.Severity),
		Confidence:       confidence,
		EvidenceStrength: potential.EvidenceStrength,
		Title:            potential.Title,
		Reason:           potential.Description,
		Observation:      observation,
		Hypothesis:       hypothesis,
		ReviewPolicy:     policy,
		Adjudication:     &decision,
		Location:         primaryLocation,
		Evidence: finding.Evidence{
			AffectedFiles:   files,
			AffectedSymbols: append([]string(nil), potential.AffectedSymbols...),
			CodeSnippet:     limitLines(potential.CodeSnippet, maxStoredSnippetLines),
			CallChain:       append([]string(nil), potential.CallChain...),
			Steps:           steps,
		},
	}
	if policy == finding.ReviewPolicyStaticAuthoritative {
		adjudicated := finding.Adjudicate(finding.PotentialFinding{Finding: item, Observation: observation, Hypothesis: hypothesis, ReviewPolicy: policy}, decision)
		return adjudicated.Candidates[0].Finding
	}
	return item
}

// Review returns confirmed AI findings and deterministic fallbacks.
func (reviewer *Reviewer) Review(ctx context.Context, potentials []language.PotentialFinding) []finding.Finding {
	return reviewer.ReviewWithProgress(ctx, potentials, nil)
}

// ReviewWithProgress reports the number of attempted findings after each batch.
func (reviewer *Reviewer) ReviewWithProgress(
	ctx context.Context,
	potentials []language.PotentialFinding,
	progress func(done, total int),
) []finding.Finding {
	deterministic := make([]finding.Finding, len(potentials))
	requests := make([]ReviewRequest, len(potentials))
	for index, potential := range potentials {
		deterministic[index] = NormalizePotential(potential)
		requests[index] = ReviewRequest{
			Finding:     deterministic[index],
			CodeSnippet: limitLines(potential.CodeSnippet, maxPromptSnippetLines),
			CallChain:   append([]string(nil), potential.CallChain...),
		}
	}
	if reviewer.provider == nil || len(requests) == 0 {
		return deterministic
	}
	if progress != nil {
		progress(0, len(requests))
	}

	result := make([]finding.Finding, 0, len(deterministic))
	for start := 0; start < len(requests); start += reviewer.batchSize {
		if ctx.Err() != nil {
			return append(result, deterministic[start:]...)
		}
		end := min(start+reviewer.batchSize, len(requests))
		responses, err := reviewer.reviewBatch(ctx, requests[start:end])
		if err != nil {
			reviewer.warning(fmt.Errorf("AI review batch %d-%d: %w", start+1, end, err))
			result = append(result, deterministic[start:end]...)
			if progress != nil {
				progress(end, len(requests))
			}
			continue
		}
		for offset, response := range responses {
			normalized := deterministic[start+offset]
			decision, decisionErr := NormalizeDecision(normalized, response, reviewer.provider.Name(), "")
			if decisionErr != nil {
				reviewer.warning(decisionErr)
				result = append(result, normalized)
				continue
			}
			normalized.AIReviewed = true
			normalized.Adjudication = &decision
			normalized.AIReview = &finding.AIAssessment{
				EvidenceRelation: string(response.EvidenceRelation), ClaimStrength: string(response.ClaimStrength), Verdict: decision.Status, IsRealIssue: decision.Status == finding.AdjudicationConfirmed || decision.Status == finding.AdjudicationDowngraded, Severity: decision.Severity, Confidence: decision.Confidence,
				Reason: response.Reason, Recommendation: response.Recommendation,
			}
			result = append(result, normalized)
		}
		if progress != nil {
			progress(end, len(requests))
		}
	}
	return result
}

func (reviewer *Reviewer) reviewBatch(ctx context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	var (
		responses []ReviewResponse
		err       error
	)
	if provider, ok := reviewer.provider.(BatchProvider); ok {
		responses, err = provider.ReviewBatch(ctx, requests)
	} else {
		responses = make([]ReviewResponse, 0, len(requests))
		for _, request := range requests {
			response, reviewErr := reviewer.provider.Review(ctx, request)
			if reviewErr != nil {
				return nil, reviewErr
			}
			if response.ID == "" {
				response.ID = request.Finding.ID
			}
			responses = append(responses, response)
		}
	}
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(requests))
	for index := range requests {
		ids[index] = requests[index].Finding.ID
	}
	normalized, normalizeErr := normalizeParsedResponses(responses, ids)
	if normalizeErr != nil {
		ObserveProviderStage(ctx, "response_schema")
		return nil, apperror.Wrap(apperror.KindMalformedResponse, "validate provider adjudication schema", normalizeErr)
	}
	return normalized, nil
}

func (reviewer *Reviewer) warning(err error) {
	if reviewer.warn != nil {
		reviewer.warn(err)
	}
}

func findingID(potential language.PotentialFinding) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(potential.RuleID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(potential.File))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strconv.Itoa(potential.StartLine)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strconv.Itoa(potential.EndLine)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(potential.Title))
	for _, step := range potential.EvidenceSteps {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(step.Kind))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(step.File))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.Itoa(step.Line)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(step.Expression))
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}
