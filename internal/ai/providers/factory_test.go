package providers

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/config"
)

func TestProviderRequestTimeoutDefaultsAndOverrides(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.AIConfig
		want time.Duration
	}{
		{name: "default cloud", cfg: config.AIConfig{Provider: "openai", OpenAI: config.OpenAIConfig{APIKey: "key"}}, want: 30 * time.Second},
		{name: "default Ollama", cfg: config.AIConfig{Provider: "ollama"}, want: 180 * time.Second},
		{name: "general cloud override", cfg: config.AIConfig{Provider: "openai", RequestTimeout: "45s", OpenAI: config.OpenAIConfig{APIKey: "key"}}, want: 45 * time.Second},
		{name: "general Ollama override", cfg: config.AIConfig{Provider: "ollama", RequestTimeout: "45s"}, want: 45 * time.Second},
		{name: "Ollama override precedence", cfg: config.AIConfig{Provider: "ollama", RequestTimeout: "45s", Ollama: config.OllamaConfig{RequestTimeout: "3m"}}, want: 3 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, err := New(test.cfg, &http.Client{})
			if err != nil {
				t.Fatal(err)
			}
			var got time.Duration
			switch typed := provider.(type) {
			case *OpenAI:
				got = typed.client.Timeout
			case *Ollama:
				got = typed.client.Timeout
			default:
				t.Fatalf("unexpected provider %T", provider)
			}
			if got != test.want {
				t.Fatalf("timeout = %v, want %v", got, test.want)
			}
		})
	}
}

func TestProviderRequestTimeoutPreservesExplicitInjectedClient(t *testing.T) {
	client := &http.Client{Timeout: 7 * time.Second}
	provider, err := New(config.AIConfig{Provider: "ollama", RequestTimeout: "45s", Ollama: config.OllamaConfig{RequestTimeout: "3m"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.(*Ollama).client.Timeout; got != 7*time.Second {
		t.Fatalf("timeout = %v, want injected 7s", got)
	}
}

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name     string
		config   config.AIConfig
		wantName string
		wantNil  bool
		wantErr  bool
		wantKind apperror.Kind
	}{
		{name: "no AI", config: config.AIConfig{NoAI: true}, wantNil: true},
		{name: "OpenAI case insensitive", config: config.AIConfig{Provider: " OpenAI ", Model: "model", OpenAI: config.OpenAIConfig{APIKey: "key"}}, wantName: "openai"},
		{name: "Anthropic", config: config.AIConfig{Provider: "anthropic", Model: "model", Anthropic: config.AnthropicConfig{APIKey: "key"}}, wantName: "anthropic"},
		{name: "Gemini", config: config.AIConfig{Provider: "gemini", Model: "model", Gemini: config.GeminiConfig{APIKey: "key"}}, wantName: "gemini"},
		{name: "Ollama without API key", config: config.AIConfig{Provider: "ollama", Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434", Model: "llama"}}, wantName: "ollama"},
		{name: "unknown", config: config.AIConfig{Provider: "other"}, wantErr: true, wantKind: apperror.KindProviderInitialization},
		{name: "OpenAI missing key", config: config.AIConfig{Provider: "openai"}, wantErr: true, wantKind: apperror.KindAuthentication},
		{name: "Anthropic missing key", config: config.AIConfig{Provider: "anthropic"}, wantErr: true, wantKind: apperror.KindAuthentication},
		{name: "Gemini missing key", config: config.AIConfig{Provider: "gemini"}, wantErr: true, wantKind: apperror.KindAuthentication},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, err := New(test.config, http.DefaultClient)
			if (err != nil) != test.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantNil {
				if provider != nil {
					t.Fatalf("New() = %#v, want nil", provider)
				}
				return
			}
			if test.wantErr {
				var typed *apperror.Error
				if !errors.As(err, &typed) || typed.Kind != test.wantKind {
					t.Fatalf("New() error = %#v, want kind %q", err, test.wantKind)
				}
				return
			}
			if provider == nil || provider.Name() != test.wantName {
				t.Fatalf("New().Name() = %v, want %q", provider, test.wantName)
			}
		})
	}
}
