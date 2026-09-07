package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ModelInfo struct {
	ID                         string
	DisplayName                string
	SupportedGenerationMethods []string
}

var modelHTTPClient = &http.Client{Timeout: 10 * time.Second}

func FetchModels(ctx context.Context, provider, apiKey string) ([]ModelInfo, error) {
	endpoint := providerEndpoints[provider]
	if provider == "ollama" {
		endpoint = defaultOllamaURL
	}
	if endpoint == "" {
		return nil, fmt.Errorf("model endpoint unavailable for %s", provider)
	}
	return fetchModelsFrom(ctx, provider, apiKey, endpoint)
}
func fetchModelsFrom(ctx context.Context, provider, apiKey, baseURL string) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := baseURL
	if provider == "ollama" {
		url = strings.TrimRight(baseURL, "/") + "/api/tags"
	} else if !strings.Contains(baseURL, "/v1/") {
		url = strings.TrimRight(baseURL, "/") + "/v1/models"
	}
	if provider == "gemini" {
		url += "?key=" + apiKey
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create model request: %s", redactSecret(err.Error(), apiKey))
	}
	if provider == "openai" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if provider == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := modelHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s models: %s", provider, redactSecret(err.Error(), apiKey))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s models: HTTP %d", provider, resp.StatusCode)
	}
	var candidates []ModelInfo
	if provider == "ollama" {
		var body struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return nil, fmt.Errorf("decode %s models", provider)
		}
		for _, m := range body.Models {
			candidates = append(candidates, ModelInfo{ID: m.Name, DisplayName: m.Name})
		}
	} else {
		var body struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Models []struct {
				Name                       string   `json:"name"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return nil, fmt.Errorf("decode %s models", provider)
		}
		for _, m := range body.Data {
			candidates = append(candidates, ModelInfo{ID: m.ID, DisplayName: m.ID})
		}
		for _, m := range body.Models {
			id := strings.TrimPrefix(m.Name, "models/")
			candidates = append(candidates, ModelInfo{ID: id, DisplayName: id, SupportedGenerationMethods: append([]string(nil), m.SupportedGenerationMethods...)})
		}
	}
	models := []ModelInfo{}
	for _, candidate := range candidates {
		if IsReviewCapableModel(provider, candidate) {
			models = append(models, candidate)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no compatible %s models returned", provider)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func IsReviewCapableModel(provider string, model ModelInfo) bool {
	id := strings.ToLower(strings.TrimSpace(model.ID))
	if id == "" {
		return false
	}
	switch provider {
	case "gemini":
		if !strings.HasPrefix(id, "gemini-") {
			return false
		}
		for _, blocked := range []string{"embedding", "imagen", "image", "aqa", "audio", "live"} {
			if strings.Contains(id, blocked) {
				return false
			}
		}
		if len(model.SupportedGenerationMethods) > 0 {
			for _, method := range model.SupportedGenerationMethods {
				if strings.EqualFold(method, "generateContent") {
					return true
				}
			}
			return false
		}
		return true
	case "openai":
		for _, blocked := range []string{"embedding", "image", "dall-e", "audio", "speech", "tts", "whisper", "transcri", "realtime", "moderation"} {
			if strings.Contains(id, blocked) {
				return false
			}
		}
		return strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "chatgpt-") || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4")
	case "anthropic":
		return strings.HasPrefix(id, "claude-")
	case "ollama":
		return true
	default:
		return false
	}
}
func fallbackModels(provider string) []ModelInfo {
	choices := map[string][]string{"openai": {"gpt-4o", "gpt-4o-mini"}, "anthropic": {"claude-sonnet-4-20250514", "claude-3-5-haiku-latest"}, "gemini": {"gemini-2.5-pro", "gemini-2.5-flash"}, "ollama": {"llama3.1", "codellama", "mistral"}}[provider]
	out := make([]ModelInfo, len(choices))
	for i, id := range choices {
		out[i] = ModelInfo{ID: id, DisplayName: id}
	}
	return out
}
