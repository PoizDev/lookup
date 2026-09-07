package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/ai/providers"
	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/reporter"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

func TestOllamaFailureAuditRecordsHTTPAndSchemaWithoutContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"message":{"content":"not-json-secret-marker"}}`)
	}))
	defer server.Close()
	originalFactory, originalCache := newAIProvider, newAssessmentCache
	t.Cleanup(func() { newAIProvider, newAssessmentCache = originalFactory, originalCache })
	newAssessmentCache = func() *ai.AssessmentCache { return ai.NewAssessmentCache(t.TempDir()) }
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) {
		return providers.NewOllama(server.Client(), "model", server.URL), nil
	}
	item := finding.Finding{ID: "audit-id", RuleID: "RULE", Severity: finding.SeverityHigh, Confidence: .8, ReviewPolicy: finding.ReviewPolicyContextRequired}
	build := func(item finding.Finding) reviewcontext.ReviewPacket {
		return reviewcontext.RecalculateMetadata(reviewcontext.ReviewPacket{Candidate: finding.PotentialFinding{Finding: item}, Sufficiency: reviewcontext.SufficiencySufficient})
	}
	_, stats := reviewNormalizedFindings(context.Background(), config.AIConfig{Provider: "ollama", ReviewMode: "all"}, []finding.Finding{item}, nil, nil, nil, build)
	if len(stats.ProviderAttempts) != 1 {
		t.Fatalf("attempts = %#v", stats.ProviderAttempts)
	}
	attempt := stats.ProviderAttempts[0]
	if !attempt.ResponseReceived || attempt.HTTPStatus != 200 || attempt.ValidationStage != "response_schema" || attempt.FailureKind != string(apperror.KindMalformedResponse) {
		t.Fatalf("attempt = %#v", attempt)
	}
	encoded := fmt.Sprintf("%#v", attempt)
	if strings.Contains(encoded, "not-json-secret-marker") {
		t.Fatalf("attempt audit retained response content: %s", encoded)
	}
}

type cliReviewProvider struct{}

func (cliReviewProvider) Name() string { return "fake" }
func (cliReviewProvider) Review(_ context.Context, request ai.ReviewRequest) (ai.ReviewResponse, error) {
	evidenceIDs := []string(nil)
	if len(request.Packet.Evidence) > 0 {
		evidenceIDs = []string{request.Packet.Evidence[0].ID}
	}
	return ai.ReviewResponse{
		ID: request.Finding.ID, EvidenceRelation: ai.EvidenceSupports, ClaimStrength: ai.ClaimStrengthUnchanged, Severity: finding.SeverityCritical,
		Confidence: request.Finding.Confidence, Reason: "confirmed", Recommendation: "fix it", EvidenceIDs: evidenceIDs,
	}, nil
}

func TestReviewNormalizedFindingsPlansOllamaWithinRequestBudget(t *testing.T) {
	originalFactory := newAIProvider
	t.Cleanup(func() { newAIProvider = originalFactory })
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) {
		return nil, errors.New("diagnostic stop after planning")
	}
	items := make([]finding.Finding, 7)
	for index := range items {
		items[index] = finding.Finding{
			ID: fmt.Sprintf("candidate-%d", index), RuleID: fmt.Sprintf("RULE-%d", index),
			Category: finding.CategoryMaintainability, Severity: finding.SeverityHigh,
			Confidence: .9, ReviewPolicy: finding.ReviewPolicyContextRequired,
			Location: &finding.Location{File: fmt.Sprintf("%d.go", index)},
		}
	}
	build := func(item finding.Finding) reviewcontext.ReviewPacket {
		packet := reviewcontext.ReviewPacket{
			Candidate: finding.PotentialFinding{Finding: item, ReviewPolicy: item.ReviewPolicy},
			Evidence: []reviewcontext.EvidenceItem{
				{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "direct source"},
				{ID: "source.enclosing", Kind: reviewcontext.EvidenceEnclosing, Content: strings.Repeat("evidence ", 1200)},
			},
			Sufficiency: reviewcontext.SufficiencySufficient,
		}
		return reviewcontext.RecalculateMetadata(packet)
	}
	_, stats := reviewNormalizedFindings(context.Background(), config.AIConfig{Provider: "ollama", ReviewMode: "all"}, items, nil, nil, nil, build)
	if stats.Plan == nil || len(stats.Plan.Selected) == 0 || stats.Plan.EstimatedProviderRequests == 0 || stats.Plan.EstimatedProviderRequests > 6 {
		t.Fatalf("Ollama plan = %#v, want a non-empty plan within the request budget", stats.Plan)
	}
	for _, candidate := range stats.Plan.Selected {
		if candidate.PredictedContextUse > candidate.ContextWindow {
			t.Fatalf("candidate %q predicted use = %d, context window = %d", candidate.CandidateID, candidate.PredictedContextUse, candidate.ContextWindow)
		}
	}
}

func TestReviewNormalizedFindingsCancelsDuringPlanning(t *testing.T) {
	originalFactory := newAIProvider
	t.Cleanup(func() { newAIProvider = originalFactory })
	providerConstructed := false
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) {
		providerConstructed = true
		return cliReviewProvider{}, nil
	}
	items := make([]finding.Finding, 20)
	for index := range items {
		items[index] = finding.Finding{ID: fmt.Sprintf("candidate-%d", index), RuleID: fmt.Sprintf("RULE-%d", index), ReviewPolicy: finding.ReviewPolicyContextRequired}
	}
	ctx, cancel := context.WithCancel(context.Background())
	built := 0
	got, stats := reviewNormalizedFindings(ctx, config.AIConfig{Provider: "ollama", ReviewMode: "all"}, items, nil, nil, nil, func(item finding.Finding) reviewcontext.ReviewPacket {
		built++
		cancel()
		return reviewcontext.ReviewPacket{Candidate: finding.PotentialFinding{Finding: item}, Sufficiency: reviewcontext.SufficiencySufficient}
	})
	if built != 1 || providerConstructed || len(got) != len(items) || stats.StopReason != "canceled" {
		t.Fatalf("built=%d providerConstructed=%v findings=%d stats=%#v", built, providerConstructed, len(got), stats)
	}
}

func cliPotential() []language.PotentialFinding {
	return []language.PotentialFinding{{RuleID: "SEC-001", Category: "Security", Severity: "High", Title: "secret"}}
}

func TestReviewFindingsNoAISkipsProviderAndReturnsDeterministic(t *testing.T) {
	originalFactory := newAIProvider
	t.Cleanup(func() { newAIProvider = originalFactory })
	called := false
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) {
		called = true
		return nil, errors.New("must not be called")
	}

	got := reviewFindings(context.Background(), config.AIConfig{NoAI: true}, cliPotential(), nil, nil, nil)
	if called || len(got) != 1 || got[0].AIReviewed {
		t.Fatalf("factory called = %v, findings = %#v", called, got)
	}
}

func TestReviewFindingsProviderConstructionFailureFallsBack(t *testing.T) {
	originalFactory := newAIProvider
	t.Cleanup(func() { newAIProvider = originalFactory })
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) {
		return nil, errors.New("missing key")
	}
	var warning error

	got := reviewFindings(context.Background(), config.AIConfig{Provider: "openai"}, cliPotential(), nil, func(err error) { warning = err }, nil)
	if len(got) != 1 || got[0].AIReviewed || warning == nil {
		t.Fatalf("findings = %#v, warning = %v", got, warning)
	}
}

func TestReviewFindingsRunsProviderAndReportsProgress(t *testing.T) {
	originalFactory := newAIProvider
	originalCache := newAssessmentCache
	t.Cleanup(func() { newAIProvider = originalFactory; newAssessmentCache = originalCache })
	newAssessmentCache = func() *ai.AssessmentCache { return ai.NewAssessmentCache(t.TempDir()) }
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) { return cliReviewProvider{}, nil }
	var progress [][2]int

	got := reviewFindings(context.Background(), config.AIConfig{Provider: "fake"}, cliPotential(), nil, nil, func(done, total int) {
		progress = append(progress, [2]int{done, total})
	})
	if len(got) != 1 || !got[0].AIReviewed || got[0].Severity != finding.SeverityHigh || got[0].AIReview == nil || got[0].AIReview.Severity != finding.SeverityHigh || got[0].Adjudication == nil || got[0].Adjudication.Status != finding.AdjudicationConfirmed {
		t.Fatalf("findings = %#v", got)
	}
	if len(progress) != 2 || progress[0] != [2]int{0, 1} || progress[1] != [2]int{1, 1} {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestReviewNormalizedFindingsOnlySendsPreFilteredFindings(t *testing.T) {
	originalFactory := newAIProvider
	originalCache := newAssessmentCache
	t.Cleanup(func() { newAIProvider = originalFactory; newAssessmentCache = originalCache })
	provider := &collectingCLIProvider{}
	newAIProvider = func(config.AIConfig, *http.Client) (ai.Provider, error) { return provider, nil }
	newAssessmentCache = func() *ai.AssessmentCache { return ai.NewAssessmentCache(t.TempDir()) }
	all := normalizePotentials([]language.PotentialFinding{
		{RuleID: "HIGH", Category: "Security", Severity: "High", Title: "high"},
		{RuleID: "LOW", Category: "Security", Severity: "Low", Title: "low"},
	})
	filtered, err := reporter.Filter(all, "high", "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reviewNormalizedFindings(context.Background(), config.AIConfig{Provider: "fake", ReviewMode: "all"}, filtered, nil, nil, nil)
	if len(got) != 1 || len(provider.ids) != 1 || provider.ids[0] != filtered[0].ID {
		t.Fatalf("reviewed=%#v provider ids=%#v", got, provider.ids)
	}
}

type collectingCLIProvider struct{ ids []string }

func (*collectingCLIProvider) Name() string { return "fake" }
func (p *collectingCLIProvider) Review(_ context.Context, request ai.ReviewRequest) (ai.ReviewResponse, error) {
	p.ids = append(p.ids, request.Finding.ID)
	return ai.ReviewResponse{ID: request.Finding.ID, EvidenceRelation: ai.EvidenceSupports, ClaimStrength: ai.ClaimStrengthUnchanged, Severity: request.Finding.Severity, Confidence: request.Finding.Confidence, Reason: "real", Recommendation: "fix"}, nil
}
