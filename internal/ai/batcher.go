package ai

import (
	"context"
	"encoding/json"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

const (
	tokenOverheadPerFinding         = 64
	EstimatedOutputTokensPerFinding = 384
	DefaultOutputTokenBudget        = 4096
	ollamaFallbackContextWindow     = 4096
	ollamaFixedPromptOverhead       = 512
)

type BatchConstraints struct {
	InputBudget          int
	OutputBudget         int
	ContextWindow        int
	PromptOverheadTokens int
	SizingStrategy       PromptSizingStrategy
}

func OllamaBatchConstraints() BatchConstraints {
	return OllamaBatchConstraintsForContext(0)
}

func OllamaBatchConstraintsForContext(contextWindow int) BatchConstraints {
	if contextWindow <= 0 {
		contextWindow = ollamaFallbackContextWindow
	}
	return BatchConstraints{InputBudget: 8000, OutputBudget: 2048, ContextWindow: contextWindow, PromptOverheadTokens: OllamaChatTemplateAllowance}
}

func EstimateBatchTokens(requests []ReviewRequest) int {
	total := 32
	for _, request := range requests {
		data, _ := json.Marshal(canonicalizeForTransmission(request))
		total += tokenOverheadPerFinding + (len(data)+3)/4
	}
	return total
}

// EstimateBatchOutputTokens returns the estimated tokens needed for the JSON response envelope.
func EstimateBatchOutputTokens(count int) int {
	if count <= 0 {
		return 0
	}
	return 64 + count*EstimatedOutputTokensPerFinding
}

// AdaptiveBatches splits review requests so that neither input token budget nor
// provider output token budget is exceeded.
func AdaptiveBatches(requests []ReviewRequest, inputBudget int, outputBudgets ...int) [][]ReviewRequest {
	if inputBudget < 128 {
		inputBudget = 128
	}
	outputBudget := DefaultOutputTokenBudget
	if len(outputBudgets) > 0 && outputBudgets[0] > 0 {
		outputBudget = outputBudgets[0]
	}
	if outputBudget < 512 {
		outputBudget = 512
	}
	result, _ := AdaptiveBatchesWithConstraints(requests, BatchConstraints{InputBudget: inputBudget, OutputBudget: outputBudget})
	return result
}

func AdaptiveBatchesWithConstraints(requests []ReviewRequest, constraints BatchConstraints) ([][]ReviewRequest, []ReviewRequest) {
	return AdaptiveBatchesWithConstraintsContext(context.Background(), requests, constraints)
}

func AdaptiveBatchesWithConstraintsContext(ctx context.Context, requests []ReviewRequest, constraints BatchConstraints) ([][]ReviewRequest, []ReviewRequest) {
	inputBudget := constraints.InputBudget
	if inputBudget < 128 {
		inputBudget = 128
	}
	outputBudget := constraints.OutputBudget
	if outputBudget < 512 {
		outputBudget = 512
	}
	result := make([][]ReviewRequest, 0)
	rejected := make([]ReviewRequest, 0)
	current := make([]ReviewRequest, 0)
	for _, original := range requests {
		requestBudget := inputBudget
		if constraints.ContextWindow > 0 {
			requestBudget = min(requestBudget, constraints.ContextWindow-constraints.PromptOverheadTokens-EstimateBatchOutputTokens(1))
		}
		request := minimizeToBudget(original, requestBudget)
		if constraints.ContextWindow > 0 {
			request = minimizeOllamaToContextWithSizing(ctx, request, constraints.ContextWindow, constraints.SizingStrategy)
		}
		if original.Packet.Candidate.ID != "" && !request.Packet.Reviewable() {
			rejected = append(rejected, request)
			continue
		}
		if constraints.ContextWindow > 0 && !batchFitsConstraints(ctx, []ReviewRequest{request}, constraints, inputBudget, outputBudget) {
			rejected = append(rejected, request)
			continue
		}
		candidate := append(append([]ReviewRequest(nil), current...), request)
		inputExceeded := len(current) > 0 && estimateBatchInputContext(ctx, candidate, constraints) > inputBudget
		outputExceeded := len(current) > 0 && EstimateBatchOutputTokens(len(candidate)) > outputBudget
		contextExceeded := len(current) > 0 && constraints.ContextWindow > 0 && contextTokens(ctx, candidate, constraints) > constraints.ContextWindow
		if inputExceeded || outputExceeded || contextExceeded {
			result = append(result, current)
			current = nil
		}
		current = append(current, request)
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result, rejected
}

func batchFitsConstraints(ctx context.Context, batch []ReviewRequest, constraints BatchConstraints, inputBudget, outputBudget int) bool {
	contextFits := true
	if constraints.ContextWindow > 0 {
		contract, err := buildOllamaRequestWithContextPolicy(ctx, batch, constraints.ContextWindow, constraints.PromptOverheadTokens, constraints.SizingStrategy)
		contextFits = err == nil && contract.Fits()
	}
	return estimateBatchInputContext(ctx, batch, constraints) <= inputBudget &&
		EstimateBatchOutputTokens(len(batch)) <= outputBudget &&
		contextFits
}

func estimateBatchInput(batch []ReviewRequest, constraints BatchConstraints) int {
	return estimateBatchInputContext(context.Background(), batch, constraints)
}

func estimateBatchInputContext(ctx context.Context, batch []ReviewRequest, constraints BatchConstraints) int {
	if constraints.ContextWindow > 0 {
		request, err := buildOllamaRequestWithContextPolicy(ctx, batch, constraints.ContextWindow, constraints.PromptOverheadTokens, constraints.SizingStrategy)
		if err != nil {
			return 0
		}
		return request.PromptEstimate
	}
	return EstimateBatchTokens(batch)
}

func contextTokens(ctx context.Context, batch []ReviewRequest, constraints BatchConstraints) int {
	if contract, err := buildOllamaRequestWithContextPolicy(ctx, batch, constraints.ContextWindow, constraints.PromptOverheadTokens, constraints.SizingStrategy); err == nil {
		return contract.ContextUse()
	}
	return EstimateBatchTokens(batch) + EstimateBatchOutputTokens(len(batch)) + constraints.PromptOverheadTokens
}

func minimizeOllamaToContext(request ReviewRequest, contextWindow int) ReviewRequest {
	return minimizeOllamaToContextWithSizing(context.Background(), request, contextWindow, PromptSizingStrategy{})
}

func minimizeOllamaToContextWithSizing(ctx context.Context, request ReviewRequest, contextWindow int, strategy PromptSizingStrategy) ReviewRequest {
	return minimizeUntil(request, func(candidate ReviewRequest) bool {
		contract, err := buildOllamaRequestWithContextPolicy(ctx, []ReviewRequest{candidate}, contextWindow, OllamaChatTemplateAllowance, strategy)
		return err == nil && contract.Fits()
	})
}

func minimizeToBudget(request ReviewRequest, budget int) ReviewRequest {
	return minimizeUntil(request, func(candidate ReviewRequest) bool {
		return EstimateBatchTokens([]ReviewRequest{candidate}) <= budget
	})
}

func minimizeUntil(request ReviewRequest, fits func(ReviewRequest) bool) ReviewRequest {
	request = cloneReviewRequest(request)
	original := request.Packet
	packetAuthoritative := request.Packet.Candidate.ID != ""
	for !fits(request) && len(request.Packet.Evidence) > 1 {
		var removed bool
		request.Packet, removed = reviewcontext.RemoveLowestPriorityEvidence(request.Packet)
		if !removed {
			break
		}
	}
	for !fits(request) && len(request.Packet.Evidence) > 0 && len(request.Packet.Evidence[0].Content) > 32 {
		content := request.Packet.Evidence[0].Content
		request.Packet.Evidence[0].Content = content[:len(content)*3/4]
		request.Packet.Metadata.Truncated = true
	}
	for !fits(request) && len(request.Packet.Candidate.Hypothesis) > 32 {
		request.Packet.Candidate.Hypothesis = request.Packet.Candidate.Hypothesis[:len(request.Packet.Candidate.Hypothesis)*3/4]
		request.Packet.Metadata.Truncated = true
	}
	for !fits(request) && len(request.Packet.Candidate.Observation) > 32 {
		request.Packet.Candidate.Observation = request.Packet.Candidate.Observation[:len(request.Packet.Candidate.Observation)*3/4]
		request.Packet.Metadata.Truncated = true
	}
	for !fits(request) && len(request.Packet.Candidate.Title) > 16 {
		request.Packet.Candidate.Title = request.Packet.Candidate.Title[:len(request.Packet.Candidate.Title)*3/4]
		request.Packet.Metadata.Truncated = true
	}
	if !packetAuthoritative {
		for !fits(request) && len(request.CodeSnippet) > 16 {
			request.CodeSnippet = request.CodeSnippet[:len(request.CodeSnippet)*3/4]
		}
		for !fits(request) && len(request.Finding.Evidence.Steps) > 1 {
			request.Finding.Evidence.Steps = request.Finding.Evidence.Steps[:len(request.Finding.Evidence.Steps)-1]
		}
		for !fits(request) && len(request.CallChain) > 1 {
			request.CallChain = request.CallChain[:len(request.CallChain)-1]
		}
		if !fits(request) {
			request.CodeSnippet = ""
		}
		request.Finding.Title = truncateText(request.Finding.Title, 64)
		request.Finding.Reason = truncateText(request.Finding.Reason, 128)
		request.Finding.RuleID = truncateText(request.Finding.RuleID, 64)
		for i := range request.Finding.Evidence.Steps {
			step := &request.Finding.Evidence.Steps[i]
			step.File = truncateText(step.File, 96)
			step.Kind = truncateText(step.Kind, 24)
			step.Expression = truncateText(step.Expression, 64)
			step.Message = truncateText(step.Message, 96)
			step.RelatedSymbol = truncateText(step.RelatedSymbol, 64)
		}
		if !fits(request) {
			request.Finding.Evidence.Steps = nil
			request.CallChain = nil
			request.Finding.Evidence.CallChain = nil
			request.Finding.Location = nil
		}
		if !fits(request) {
			request.Finding.Reason = ""
			request.Finding.Title = ""
		}
	}
	request.Packet = reviewcontext.ReevaluateSufficiency(original, request.Packet)
	request.Packet = reviewcontext.RecalculateMetadata(request.Packet)
	return request
}

func cloneReviewRequest(request ReviewRequest) ReviewRequest {
	request.Packet.Evidence = append([]reviewcontext.EvidenceItem(nil), request.Packet.Evidence...)
	for index := range request.Packet.Evidence {
		if request.Packet.Evidence[index].Location != nil {
			location := *request.Packet.Evidence[index].Location
			request.Packet.Evidence[index].Location = &location
		}
	}
	request.Packet.MissingInformation = append([]string(nil), request.Packet.MissingInformation...)
	request.Finding.Evidence.Steps = append([]finding.EvidenceStep(nil), request.Finding.Evidence.Steps...)
	request.Finding.Evidence.CallChain = append([]string(nil), request.Finding.Evidence.CallChain...)
	request.CallChain = append([]string(nil), request.CallChain...)
	return request
}

func truncateText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum] + "…"
}
