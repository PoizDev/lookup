package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/poizdev/lookup/internal/ai"
)

type Anthropic struct {
	client  *http.Client
	apiKey  string
	model   string
	baseURL string
}

func NewAnthropic(client *http.Client, apiKey, model, baseURL string) *Anthropic {
	return &Anthropic{client: client, apiKey: apiKey, model: model, baseURL: baseURL}
}

func (*Anthropic) Name() string           { return "anthropic" }
func (provider *Anthropic) Model() string { return provider.model }

func (provider *Anthropic) Review(ctx context.Context, request ai.ReviewRequest) (ai.ReviewResponse, error) {
	return singleReview(provider.ReviewBatch(ctx, []ai.ReviewRequest{request}))
}

func (provider *Anthropic) ReviewBatch(ctx context.Context, requests []ai.ReviewRequest) ([]ai.ReviewResponse, error) {
	prompt, err := ai.BuildPrompt(requests)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model":      provider.model,
		"max_tokens": anthropicOutputTokens(len(requests)),
		"system":     prompt.System,
		"messages":   []map[string]string{{"role": "user", "content": prompt.User}},
	}
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	err = postJSON(ctx, provider.client, joinURL(provider.baseURL, "/v1/messages"), map[string]string{
		"x-api-key":         provider.apiKey,
		"anthropic-version": "2023-06-01",
	}, payload, &envelope, provider.apiKey)
	if err != nil {
		return nil, err
	}
	for _, block := range envelope.Content {
		if block.Type == "text" {
			return ai.NormalizeResponses(block.Text, findingIDs(requests))
		}
	}
	return nil, fmt.Errorf("provider response contains no review content")
}

func anthropicOutputTokens(findings int) int {
	tokens := findings * 384
	if tokens < 1024 {
		return 1024
	}
	if tokens > 8192 {
		return 8192
	}
	return tokens
}
