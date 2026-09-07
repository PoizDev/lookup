package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/poizdev/lookup/internal/ai"
)

type OpenAI struct {
	client  *http.Client
	apiKey  string
	model   string
	baseURL string
}

func NewOpenAI(client *http.Client, apiKey, model, baseURL string) *OpenAI {
	return &OpenAI{client: client, apiKey: apiKey, model: model, baseURL: baseURL}
}

func (*OpenAI) Name() string           { return "openai" }
func (provider *OpenAI) Model() string { return provider.model }

func (provider *OpenAI) Review(ctx context.Context, request ai.ReviewRequest) (ai.ReviewResponse, error) {
	return singleReview(provider.ReviewBatch(ctx, []ai.ReviewRequest{request}))
}

func (provider *OpenAI) ReviewBatch(ctx context.Context, requests []ai.ReviewRequest) ([]ai.ReviewResponse, error) {
	prompt, err := ai.BuildPrompt(requests)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model": provider.model,
		"messages": []map[string]string{
			{"role": "system", "content": prompt.System},
			{"role": "user", "content": prompt.User},
		},
		"response_format": map[string]string{"type": "json_object"},
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err = postJSON(ctx, provider.client, joinURL(provider.baseURL, "/v1/chat/completions"), map[string]string{
		"Authorization": "Bearer " + provider.apiKey,
	}, payload, &envelope, provider.apiKey)
	if err != nil {
		return nil, err
	}
	if len(envelope.Choices) == 0 {
		return nil, fmt.Errorf("provider response contains no review content")
	}
	return ai.NormalizeResponses(envelope.Choices[0].Message.Content, findingIDs(requests))
}
