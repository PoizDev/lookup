package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/apperror"
)

type Ollama struct {
	client        *http.Client
	model         string
	baseURL       string
	contextWindow int
}

func NewOllama(client *http.Client, model, baseURL string) *Ollama {
	return NewOllamaWithContext(client, model, baseURL, 0)
}

func NewOllamaWithContext(client *http.Client, model, baseURL string, contextWindow int) *Ollama {
	if contextWindow <= 0 {
		contextWindow = ai.OllamaBatchConstraints().ContextWindow
	}
	return &Ollama{client: httpClientWithDefaultTimeout(client, ollamaHTTPTimeout), model: model, baseURL: baseURL, contextWindow: contextWindow}
}

func (*Ollama) Name() string                { return "ollama" }
func (provider *Ollama) Model() string      { return provider.model }
func (provider *Ollama) ContextWindow() int { return provider.contextWindow }

func (provider *Ollama) Review(ctx context.Context, request ai.ReviewRequest) (ai.ReviewResponse, error) {
	return singleReview(provider.ReviewBatch(ctx, []ai.ReviewRequest{request}))
}

func (provider *Ollama) ReviewBatch(ctx context.Context, requests []ai.ReviewRequest) ([]ai.ReviewResponse, error) {
	contract, err := ai.BuildOllamaRequest(requests, provider.contextWindow)
	if err != nil {
		return nil, err
	}
	if !contract.Fits() {
		return nil, apperror.New(apperror.KindContextInsufficient,
			fmt.Sprintf("Ollama request exceeds executable context: %d > %d", contract.ContextUse(), contract.NumCtx))
	}
	payload := contract.Payload(provider.model)
	var envelope struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
	}
	if err := postJSON(ctx, provider.client, joinURL(provider.baseURL, "/api/chat"), nil, payload, &envelope); err != nil {
		return nil, err
	}
	ai.ObserveProviderPromptTokens(ctx, envelope.PromptEvalCount)
	responses, err := ai.NormalizeResponses(envelope.Message.Content, findingIDs(requests))
	if err != nil {
		ai.ObserveProviderStage(ctx, "response_schema")
		return nil, apperror.Wrap(apperror.KindMalformedResponse, "validate Ollama adjudication response", err)
	}
	return responses, nil
}
