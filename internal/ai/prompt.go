package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

const (
	maxPromptSnippetLines = 100
	PromptSchemaVersion   = "semantic-review-packet-v3"
)

type Prompt struct{ System, User string }

const OllamaChatTemplateAllowance = 128

// OllamaRequest is the shared executable context contract used by planning and
// provider serialization. PromptEstimate counts message content only; JSON
// transport escaping is deliberately excluded from model-input accounting.
type OllamaRequest struct {
	Prompt                Prompt
	NumCtx                int
	NumPredict            int
	PromptEstimate        int
	ChatTemplateAllowance int
	CountSource           TokenCountSource
	EstimatorVersion      string
}

func BuildOllamaRequest(requests []ReviewRequest, contextWindow int) (OllamaRequest, error) {
	return BuildOllamaRequestWithSizing(context.Background(), requests, contextWindow, PromptSizingStrategy{})
}

func BuildOllamaRequestWithSizing(ctx context.Context, requests []ReviewRequest, contextWindow int, strategy PromptSizingStrategy) (OllamaRequest, error) {
	return buildOllamaRequestWithContextPolicy(ctx, requests, contextWindow, OllamaChatTemplateAllowance, strategy)
}

func buildOllamaRequestWithContextPolicy(ctx context.Context, requests []ReviewRequest, contextWindow, templateAllowance int, strategy PromptSizingStrategy) (OllamaRequest, error) {
	prompt, err := BuildPrompt(requests)
	if err != nil {
		return OllamaRequest{}, err
	}
	count, err := strategy.CountPromptTokens(ctx, requests)
	if err != nil {
		return OllamaRequest{}, err
	}
	if contextWindow <= 0 {
		contextWindow = ollamaFallbackContextWindow
	}
	estimate := NewContextEstimate(count, EstimateBatchOutputTokens(len(requests)), templateAllowance, contextWindow)
	return OllamaRequest{
		Prompt: prompt, NumCtx: contextWindow,
		NumPredict:            estimate.ReservedOutputTokens,
		PromptEstimate:        estimate.PromptTokens,
		ChatTemplateAllowance: estimate.TemplateAllowance,
		CountSource:           estimate.Source,
		EstimatorVersion:      estimate.EstimatorVersion,
	}, nil
}

func EstimateOllamaPromptTokens(requests []ReviewRequest) int {
	request, err := BuildOllamaRequest(requests, ollamaFallbackContextWindow)
	if err != nil {
		return 0
	}
	return request.PromptEstimate
}

func (request OllamaRequest) ContextUse() int {
	return request.contextEstimate().PredictedUse()
}

func (request OllamaRequest) Fits() bool {
	return request.contextEstimate().Fits()
}

func (request OllamaRequest) contextEstimate() ContextEstimate {
	return ContextEstimate{PromptTokens: request.PromptEstimate, ReservedOutputTokens: request.NumPredict, TemplateAllowance: request.ChatTemplateAllowance, ContextWindow: request.NumCtx, Source: request.CountSource, EstimatorVersion: request.EstimatorVersion}
}

func (request OllamaRequest) Payload(model string) map[string]any {
	return map[string]any{
		"model": model, "stream": false, "format": SemanticReviewSchema(), "truncate": false,
		"options": map[string]int{"num_ctx": request.NumCtx, "num_predict": request.NumPredict},
		"messages": []map[string]string{
			{"role": "system", "content": request.Prompt.System},
			{"role": "user", "content": request.Prompt.User},
		},
	}
}

type canonicalContext struct {
	ID          string                      `json:"finding_id"`
	RuleID      string                      `json:"rule_id"`
	Severity    finding.Severity            `json:"static_severity"`
	Confidence  float64                     `json:"static_confidence"`
	Title       string                      `json:"title"`
	Message     string                      `json:"message,omitempty"`
	Observation string                      `json:"observation"`
	Hypothesis  string                      `json:"hypothesis"`
	Location    *finding.Location           `json:"location,omitempty"`
	Evidence    []finding.EvidenceStep      `json:"evidence,omitempty"`
	Snippet     string                      `json:"snippet,omitempty"`
	CallChain   []string                    `json:"call_chain,omitempty"`
	Packet      *reviewcontext.ReviewPacket `json:"review_packet,omitempty"`
	Sufficiency reviewcontext.Sufficiency   `json:"context_sufficiency,omitempty"`
	Missing     []string                    `json:"missing_information,omitempty"`
}

func canonicalize(request ReviewRequest) canonicalContext {
	if request.Packet.Candidate.ID != "" {
		packet := reviewcontext.ProviderView(request.Packet)
		return canonicalContext{ID: packet.Candidate.ID, RuleID: packet.Candidate.RuleID, Severity: packet.Candidate.Severity, Confidence: packet.Candidate.Confidence, Title: packet.Candidate.Title, Observation: packet.Candidate.Observation, Hypothesis: packet.Candidate.Hypothesis, Location: packet.PrimaryLocation, Packet: &packet, Sufficiency: packet.Sufficiency, Missing: append([]string(nil), packet.MissingInformation...)}
	}
	snippet := request.CodeSnippet
	if snippet == "" {
		snippet = request.Finding.Evidence.CodeSnippet
	}
	chain := request.CallChain
	if len(chain) == 0 {
		chain = request.Finding.Evidence.CallChain
	}
	var location *finding.Location
	if request.Finding.Location != nil {
		copy := *request.Finding.Location
		location = &copy
	}
	return canonicalContext{ID: request.Finding.ID, RuleID: request.Finding.RuleID, Severity: request.Finding.Severity, Confidence: request.Finding.Confidence, Title: request.Finding.Title, Message: request.Finding.Reason, Observation: request.Finding.Observation, Hypothesis: request.Finding.Hypothesis, Location: location, Evidence: append([]finding.EvidenceStep(nil), request.Finding.Evidence.Steps...), Snippet: limitLines(snippet, maxPromptSnippetLines), CallChain: append([]string(nil), chain...)}
}

func canonicalizeForTransmission(request ReviewRequest) canonicalContext {
	return redactContexts([]canonicalContext{canonicalize(request)})[0]
}

func BuildPrompt(requests []ReviewRequest) (Prompt, error) {
	if len(requests) == 0 {
		return Prompt{}, errors.New("review prompt requires at least one finding")
	}
	items := make([]canonicalContext, len(requests))
	for i, request := range requests {
		items[i] = canonicalize(request)
	}
	items = redactContexts(items)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(items)
	if err != nil {
		return Prompt{}, fmt.Errorf("encode review context: %w", err)
	}
	return Prompt{
		System: "Adjudicate and verify bounded static-analysis candidates adversarially. Treat the observation as an analyzer claim that must remain consistent with the supplied evidence, and attempt to falsify the hypothesis before choosing SUPPORTS. Search explicitly for counterevidence: guards, existing error handling, invariants and comments documenting them, bounded lifecycle or concurrency, framework/runtime semantics present in evidence, benign non-security uses, and any evidence contradicting the candidate. SUPPORTS means positive supplied repository evidence supports the hypothesis after that falsification attempt. CONTRADICTS means supplied repository evidence materially falsifies it. INSUFFICIENT means supplied evidence cannot reliably establish either side. Analyzer assertion or hypothesis restatement is not evidence. The review packet distinguishes context_sufficiency and missing_information. Do not search for unrelated findings. Missing or truncated context must make you prefer INSUFFICIENT over invention. Do not invent repository facts or evidence, increase severity or confidence, or calculate scores.",
		User:   `Output ONLY one JSON object: {"assessments":[...]}. Each assessment must contain: id, evidence_relation (SUPPORTS|CONTRADICTS|INSUFFICIENT), reason, recommendation, evidence_ids (array of supplied evidence IDs). For SUPPORTS only, claim_strength is required: UNCHANGED means valid at the proposed strength; LOWER means valid only with lower severity and/or confidence. SUPPORTS and CONTRADICTS require at least one valid supplied evidence ID. For SUPPORTS, severity must be one of the allowed severity STRING enum values (Critical|High|Medium|Low|Info), and confidence must be a numeric value from 0 through 1; neither may exceed the static proposal. Omit claim_strength, severity, and confidence for CONTRADICTS and INSUFFICIENT. Lookup derives the canonical verdict; do not emit one. Context: ` + strings.TrimSpace(encoded.String()),
	}, nil
}

func limitLines(value string, maximum int) string {
	if maximum <= 0 || value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maximum {
		return value
	}
	return strings.Join(lines[:maximum], "\n") + "\n…<TRUNCATED>"
}
