// Package ai defines provider-neutral AI review contracts.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

var ErrReviewBudgetExhausted = errors.New("scan-global AI review budget exhausted")

type ProviderAttempt struct {
	RequestIDs                      []string         `json:"request_ids"`
	EstimatedInputTokens            int              `json:"estimated_input_tokens"`
	ReservedOutputTokens            int              `json:"reserved_output_tokens"`
	ContextWindow                   int              `json:"context_window,omitempty"`
	ChatTemplateAllowance           int              `json:"chat_template_allowance,omitempty"`
	PromptTokenSource               TokenCountSource `json:"prompt_token_source,omitempty"`
	TokenEstimatorVersion           string           `json:"token_estimator_version,omitempty"`
	PredictedContextUse             int              `json:"predicted_context_use,omitempty"`
	ActualPromptTokens              int              `json:"actual_prompt_tokens,omitempty"`
	PromptTokenError                int              `json:"prompt_token_error,omitempty"`
	Duration                        time.Duration    `json:"duration"`
	ResponseReceived                bool             `json:"response_received"`
	HTTPStatus                      int              `json:"http_status,omitempty"`
	FailureKind                     string           `json:"failure_kind,omitempty"`
	ValidationStage                 string           `json:"validation_stage,omitempty"`
	CountedTowardConsecutiveFailure bool             `json:"counted_toward_consecutive_failure"`
}

type providerAttemptTrace struct {
	mu           sync.Mutex
	received     bool
	status       int
	stage        string
	promptTokens int
}

type providerAttemptTraceKey struct{}

func withProviderAttemptTrace(ctx context.Context, trace *providerAttemptTrace) context.Context {
	return context.WithValue(ctx, providerAttemptTraceKey{}, trace)
}

func ObserveProviderResponse(ctx context.Context, status int) {
	if trace, ok := ctx.Value(providerAttemptTraceKey{}).(*providerAttemptTrace); ok {
		trace.mu.Lock()
		trace.received, trace.status = true, status
		trace.mu.Unlock()
	}
}

func ObserveProviderStage(ctx context.Context, stage string) {
	if trace, ok := ctx.Value(providerAttemptTraceKey{}).(*providerAttemptTrace); ok {
		trace.mu.Lock()
		trace.stage = stage
		trace.mu.Unlock()
	}
}

func ObserveProviderPromptTokens(ctx context.Context, tokens int) {
	if trace, ok := ctx.Value(providerAttemptTraceKey{}).(*providerAttemptTrace); ok {
		trace.mu.Lock()
		trace.promptTokens = tokens
		trace.mu.Unlock()
	}
}

func (trace *providerAttemptTrace) snapshot() (bool, int, string, int) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.received, trace.status, trace.stage, trace.promptTokens
}

// Provider reviews deterministic findings using an AI service.
type Provider interface {
	Name() string
	Review(context.Context, ReviewRequest) (ReviewResponse, error)
}

// ModelProvider exposes the effective model for cache attribution and reports.
type ModelProvider interface{ Model() string }

// ContextWindowProvider exposes the effective model context used to construct
// requests. Planners use the same resolved value so execution cannot drift.
type ContextWindowProvider interface{ ContextWindow() int }

// PromptTokenCounter is an optional provider capability. Implementations must
// count the exact logical request representation that the provider will send.
type PromptTokenCounter interface {
	CountPromptTokens(context.Context, []ReviewRequest) (PromptTokenCount, error)
}

// BatchProvider is an optional capability for providers that can review
// multiple findings in one API call.
type BatchProvider interface {
	Provider
	ReviewBatch(context.Context, []ReviewRequest) ([]ReviewResponse, error)
}

type providerAttemptKey struct{}

// WithProviderAttemptBudget installs a hook called immediately before every
// provider HTTP attempt, including transport retries.
func WithProviderAttemptBudget(ctx context.Context, reserve func() bool) context.Context {
	return context.WithValue(ctx, providerAttemptKey{}, reserve)
}

func ReserveProviderAttempt(ctx context.Context) bool {
	reserve, ok := ctx.Value(providerAttemptKey{}).(func() bool)
	return !ok || reserve()
}

// ReviewRequest contains the focused context sent for review.
type ReviewRequest struct {
	Finding     finding.Finding            `json:"finding"`
	Packet      reviewcontext.ReviewPacket `json:"packet"`
	CodeSnippet string                     `json:"code_snippet"`
	CallChain   []string                   `json:"call_chain"`
}

// EvidenceRelation is the provider's structured judgment about how supplied
// repository evidence relates to the analyzer hypothesis.
type EvidenceRelation string

const (
	EvidenceSupports     EvidenceRelation = "SUPPORTS"
	EvidenceContradicts  EvidenceRelation = "CONTRADICTS"
	EvidenceInsufficient EvidenceRelation = "INSUFFICIENT"
)

// ClaimStrength describes whether a supported candidate remains valid at its
// proposed impact or only in a weaker form. It is meaningful only for SUPPORTS.
type ClaimStrength string

const (
	ClaimStrengthUnchanged ClaimStrength = "UNCHANGED"
	ClaimStrengthLower     ClaimStrength = "LOWER"
)

// ReviewResponse is the normalized result returned by every provider.
type ReviewResponse struct {
	ID               string                     `json:"id,omitempty"`
	EvidenceRelation EvidenceRelation           `json:"evidence_relation,omitempty"`
	ClaimStrength    ClaimStrength              `json:"claim_strength,omitempty"`
	Verdict          finding.AdjudicationStatus `json:"-"`
	IsRealIssue      bool                       `json:"-"`
	Severity         finding.Severity           `json:"severity"`
	Confidence       float64                    `json:"confidence"`
	Reason           string                     `json:"reason"`
	Recommendation   string                     `json:"recommendation"`
	EvidenceIDs      []string                   `json:"evidence_ids,omitempty"`
	// Malformed is set only by Lookup after validating provider output. It is
	// never accepted from or emitted to a provider.
	Malformed         bool   `json:"-"`
	MalformedReason   string `json:"-"`
	decodedJSON       bool
	severityPresent   bool
	confidencePresent bool
}

func (response *ReviewResponse) UnmarshalJSON(data []byte) error {
	type wire ReviewResponse
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*response = ReviewResponse(decoded)
	response.decodedJSON = true
	_, response.severityPresent = fields["severity"]
	_, response.confidencePresent = fields["confidence"]
	return nil
}
