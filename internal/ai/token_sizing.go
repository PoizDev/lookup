package ai

import "context"

type TokenCountSource string

const (
	TokenCountExact     TokenCountSource = "exact"
	TokenCountEstimated TokenCountSource = "estimated"

	GenericTokenEstimatorVersion = "byte-classes-v1"
)

type PromptTokenCount struct {
	Tokens           int
	TemplateIncluded bool
	Source           TokenCountSource
	EstimatorVersion string
}

type PromptSizingStrategy struct {
	Counter PromptTokenCounter
}

func (strategy PromptSizingStrategy) CountPromptTokens(ctx context.Context, requests []ReviewRequest) (PromptTokenCount, error) {
	if strategy.Counter != nil {
		count, err := strategy.Counter.CountPromptTokens(ctx, requests)
		if err == nil && count.Tokens >= 0 {
			count.Source = TokenCountExact
			count.EstimatorVersion = ""
			return count, nil
		}
	}
	prompt, err := BuildPrompt(requests)
	if err != nil {
		return PromptTokenCount{}, err
	}
	return EstimateGenericPromptTokens(prompt.System + prompt.User), nil
}

// EstimateGenericPromptTokens applies a provider- and model-neutral byte-class
// bound. Ordinary text receives a 50% byte discount, while structural ASCII
// and non-ASCII byte-heavy inputs converge toward the raw-byte upper bound.
func EstimateGenericPromptTokens(prompt string) PromptTokenCount {
	data := []byte(prompt)
	structuralASCII, nonASCII := 0, 0
	for _, value := range data {
		switch {
		case value >= 128:
			nonASCII++
		case value >= 'a' && value <= 'z', value >= 'A' && value <= 'Z', value >= '0' && value <= '9':
		default:
			structuralASCII++
		}
	}
	halfBytes := (len(data) + 1) / 2
	structuralBound := 2*structuralASCII + nonASCII
	tokens := max(halfBytes, structuralBound)
	if tokens > len(data) {
		tokens = len(data)
	}
	return PromptTokenCount{Tokens: tokens, Source: TokenCountEstimated, EstimatorVersion: GenericTokenEstimatorVersion}
}

type ContextEstimate struct {
	PromptTokens         int
	ReservedOutputTokens int
	TemplateAllowance    int
	ContextWindow        int
	Source               TokenCountSource
	EstimatorVersion     string
}

func NewContextEstimate(count PromptTokenCount, reservedOutput, templateAllowance, contextWindow int) ContextEstimate {
	if count.TemplateIncluded {
		templateAllowance = 0
	}
	return ContextEstimate{
		PromptTokens: count.Tokens, ReservedOutputTokens: reservedOutput,
		TemplateAllowance: templateAllowance, ContextWindow: contextWindow,
		Source: count.Source, EstimatorVersion: count.EstimatorVersion,
	}
}

func (estimate ContextEstimate) PredictedUse() int {
	return estimate.PromptTokens + estimate.ReservedOutputTokens + estimate.TemplateAllowance
}

func (estimate ContextEstimate) Fits() bool {
	return estimate.ContextWindow > 0 && estimate.PredictedUse() <= estimate.ContextWindow
}
