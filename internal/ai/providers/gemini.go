package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/poizdev/lookup/internal/ai"
)

type Gemini struct {
	client  *http.Client
	apiKey  string
	model   string
	baseURL string
}

func NewGemini(client *http.Client, apiKey, model, baseURL string) *Gemini {
	return &Gemini{client: client, apiKey: apiKey, model: model, baseURL: baseURL}
}

func (*Gemini) Name() string           { return "gemini" }
func (provider *Gemini) Model() string { return provider.model }

func (provider *Gemini) Review(ctx context.Context, request ai.ReviewRequest) (ai.ReviewResponse, error) {
	return singleReview(provider.ReviewBatch(ctx, []ai.ReviewRequest{request}))
}

func (provider *Gemini) ReviewBatch(ctx context.Context, requests []ai.ReviewRequest) ([]ai.ReviewResponse, error) {
	prompt, err := ai.BuildPrompt(requests)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]string{{"text": prompt.System + "\n\n" + prompt.User}},
		}},
		"generationConfig": map[string]any{
			"responseMimeType":   "application/json",
			"responseJsonSchema": ai.SemanticReviewSchema(),
		},
	}
	var envelope struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	endpoint := joinURL(provider.baseURL, "/v1beta/models/"+url.PathEscape(provider.model)+":generateContent")
	query := url.Values{"key": []string{provider.apiKey}}
	endpoint += "?" + query.Encode()
	if err := postJSON(ctx, provider.client, endpoint, nil, payload, &envelope, provider.apiKey); err != nil {
		return nil, err
	}
	if len(envelope.Candidates) == 0 || len(envelope.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("provider response contains no review content")
	}
	return ai.NormalizeResponses(envelope.Candidates[0].Content.Parts[0].Text, findingIDs(requests))
}
