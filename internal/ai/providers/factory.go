package providers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/config"
)

const (
	openAIBaseURL    = "https://api.openai.com"
	anthropicBaseURL = "https://api.anthropic.com"
	geminiBaseURL    = "https://generativelanguage.googleapis.com"

	defaultOpenAIModel    = "gpt-4o-mini"
	defaultAnthropicModel = "claude-3-5-sonnet-latest"
	defaultGeminiModel    = "gemini-3.5-flash"
)

// New constructs the configured provider. It returns nil when AI is disabled.
func New(cfg config.AIConfig, client *http.Client) (ai.Provider, error) {
	if cfg.NoAI {
		return nil, nil
	}

	providerName := strings.ToLower(strings.TrimSpace(cfg.Provider))
	fallbackTimeout := defaultHTTPTimeout
	if providerName == "ollama" {
		fallbackTimeout = ollamaHTTPTimeout
	}
	requestTimeout, err := cfg.EffectiveRequestTimeout(providerName, fallbackTimeout)
	if err != nil {
		return nil, err
	}
	client = httpClientWithDefaultTimeout(client, requestTimeout)

	switch providerName {
	case "openai":
		if strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
			return nil, apperror.New(apperror.KindAuthentication, "OpenAI API key is required")
		}
		return NewOpenAI(client, cfg.OpenAI.APIKey, valueOrDefault(cfg.Model, defaultOpenAIModel), openAIBaseURL), nil
	case "anthropic":
		if strings.TrimSpace(cfg.Anthropic.APIKey) == "" {
			return nil, apperror.New(apperror.KindAuthentication, "Anthropic API key is required")
		}
		return NewAnthropic(client, cfg.Anthropic.APIKey, valueOrDefault(cfg.Model, defaultAnthropicModel), anthropicBaseURL), nil
	case "gemini":
		if strings.TrimSpace(cfg.Gemini.APIKey) == "" {
			return nil, apperror.New(apperror.KindAuthentication, "Gemini API key is required")
		}
		return NewGemini(client, cfg.Gemini.APIKey, valueOrDefault(cfg.Model, defaultGeminiModel), geminiBaseURL), nil
	case "ollama":
		baseURL := valueOrDefault(cfg.Ollama.BaseURL, config.DefaultOllamaBaseURL)
		model := cfg.Ollama.Model
		if strings.TrimSpace(model) == "" {
			model = valueOrDefault(cfg.Model, config.DefaultOllamaModel)
		}
		return NewOllama(client, model, baseURL), nil
	default:
		return nil, apperror.New(apperror.KindProviderInitialization, fmt.Sprintf("unsupported AI provider %q", cfg.Provider))
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
