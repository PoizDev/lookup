package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/ai/providers"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/language"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

var (
	newAIProvider      = providers.New
	newAssessmentCache = ai.DefaultAssessmentCache
)

func reviewFindings(ctx context.Context, cfg config.AIConfig, potentials []language.PotentialFinding, client *http.Client, warn func(error), progressFn func(done, total int)) []finding.Finding {
	items := normalizePotentials(potentials)
	got, _ := reviewNormalizedFindings(ctx, cfg, items, client, warn, func(progress ai.ReviewProgress) {
		if progressFn != nil {
			progressFn(progress.Done, progress.Total)
		}
	})
	return got
}

func reviewNormalizedFindings(ctx context.Context, cfg config.AIConfig, items []finding.Finding, client *http.Client, warn func(error), progressFn func(ai.ReviewProgress), contextBuilders ...func(finding.Finding) reviewcontext.ReviewPacket) ([]finding.Finding, ai.ReviewStats) {
	mode := ai.ReviewMode(cfg.ReviewMode)
	if mode == "" {
		mode = ai.ReviewModeSmart
	}
	if cfg.NoAI {
		mode = ai.ReviewModeOff
	}
	reviewBudget := ai.ReviewBudget{MaxReviewedFindings: cfg.ReviewBudget.MaxReviewedFindings, MaxProviderRequests: cfg.ReviewBudget.MaxProviderRequests, MaxEstimatedInputTokens: cfg.ReviewBudget.MaxEstimatedInputTokens, MaxOutputTokens: cfg.ReviewBudget.MaxOutputTokens}
	var contextBuilder func(finding.Finding) reviewcontext.ReviewPacket
	if len(contextBuilders) > 0 {
		contextBuilder = contextBuilders[0]
	}
	plan, planErr := ai.BuildReviewPlanForProviderContext(ctx, cfg.Provider, mode, items, reviewBudget, contextBuilder)
	if planErr != nil {
		return append([]finding.Finding(nil), items...), ai.ReviewStats{FindingsTotal: len(items), Plan: &plan, StopReason: "canceled"}
	}
	if mode == ai.ReviewModeOff || len(items) == 0 {
		return append([]finding.Finding(nil), items...), ai.ReviewStats{FindingsTotal: len(items), Plan: &plan}
	}
	provider, err := newAIProvider(cfg, client)
	if err != nil {
		if warn != nil {
			warn(fmt.Errorf("initialize AI provider: %w", err))
		}
		output := applyPlanContextFailures(items, plan)
		stats := ai.ReviewStats{FindingsTotal: len(items), Plan: &plan, Failures: len(plan.SelectedIDs), StopReason: "provider_initialization"}
		for _, id := range plan.SelectedIDs {
			stats.RuntimeSkipped = append(stats.RuntimeSkipped, ai.SkippedCandidate{CandidateID: id, Reason: ai.SkipProviderFailure})
		}
		return output, stats
	}
	if provider == nil {
		output := applyPlanContextFailures(items, plan)
		stats := ai.ReviewStats{FindingsTotal: len(items), Plan: &plan, Failures: len(plan.SelectedIDs), StopReason: "provider_unavailable"}
		for _, id := range plan.SelectedIDs {
			stats.RuntimeSkipped = append(stats.RuntimeSkipped, ai.SkippedCandidate{CandidateID: id, Reason: ai.SkipProviderFailure})
		}
		return output, stats
	}
	if contextProvider, ok := provider.(ai.ContextWindowProvider); ok {
		strategy := ai.PromptSizingStrategy{}
		if counter, available := provider.(ai.PromptTokenCounter); available {
			strategy.Counter = counter
		}
		plan, planErr = ai.BuildReviewPlanForProviderResolvedSizingContext(ctx, cfg.Provider, contextProvider.ContextWindow(), strategy, mode, items, reviewBudget, contextBuilder)
		if planErr != nil {
			return append([]finding.Finding(nil), items...), ai.ReviewStats{FindingsTotal: len(items), Plan: &plan, StopReason: "canceled"}
		}
	}
	budget, outputBudget, concurrency := 20000, 4096, 3
	switch provider.Name() {
	case "anthropic", "gemini":
		outputBudget = 8192
	case "openai":
		outputBudget = 4096
	case "ollama":
		budget, outputBudget, concurrency = 8000, 2048, 1
	}
	model := configuredModel(cfg, provider.Name())
	if metadata, ok := provider.(ai.ModelProvider); ok {
		model = metadata.Model()
	}
	options := ai.TriageOptions{Mode: mode, TokenBudget: budget, OutputTokenBudget: outputBudget, Concurrency: concurrency, Model: model, Cache: newAssessmentCache(), Budget: reviewBudget, Plan: &plan, BuildContext: contextBuilder}
	return ai.NewTriageReviewer(provider, options, warn).ReviewFindings(ctx, items, progressFn)
}

func applyPlanContextFailures(items []finding.Finding, plan ai.ReviewPlan) []finding.Finding {
	output := append([]finding.Finding(nil), items...)
	for _, skipped := range plan.Skipped {
		if skipped.Reason != ai.SkipContextInsufficient {
			continue
		}
		for index := range output {
			if output[index].ID == skipped.CandidateID {
				output[index].Adjudication = &finding.ReviewDecision{Status: finding.AdjudicationUncertain, Source: finding.DecisionSourceStatic, Severity: output[index].Severity, Confidence: output[index].Confidence, Reason: "Semantic context is insufficient."}
			}
		}
	}
	return output
}

func normalizePotentials(potentials []language.PotentialFinding) []finding.Finding {
	result := make([]finding.Finding, len(potentials))
	for index, potential := range potentials {
		result[index] = ai.NormalizePotential(potential)
	}
	return result
}

func finalizeFindings(items []finding.Finding) finding.AnalysisResult {
	result := finding.AnalysisResult{}
	for _, item := range items {
		decision := finding.ReviewDecision{Status: finding.AdjudicationNotReviewed}
		if item.Adjudication != nil {
			decision = *item.Adjudication
		}
		adjudicated := finding.Adjudicate(finding.PotentialFinding{
			Finding: item, Observation: item.Observation, Hypothesis: item.Hypothesis, ReviewPolicy: item.ReviewPolicy,
		}, decision)
		result.Candidates = append(result.Candidates, adjudicated.Candidates...)
		result.FinalFindings = append(result.FinalFindings, adjudicated.FinalFindings...)
	}
	return result
}
func configuredModel(cfg config.AIConfig, provider string) string {
	if provider == "ollama" && cfg.Ollama.Model != "" {
		return cfg.Ollama.Model
	}
	return cfg.Model
}
