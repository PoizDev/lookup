package setup

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeminiModelFetchErrorRedactsAPIKey(t *testing.T) {
	original := modelHTTPClient
	t.Cleanup(func() { modelHTTPClient = original })
	modelHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("failed URL " + request.URL.String())
	})}
	_, err := fetchModelsFrom(t.Context(), "gemini", "never-leak-model-key", "https://example.invalid/v1/models")
	if err == nil || strings.Contains(err.Error(), "never-leak-model-key") {
		t.Fatalf("model fetch error leaked API key: %v", err)
	}
}

func TestFetchModelsReturnsSortedLiveModels(t *testing.T) {
	original := modelHTTPClient
	t.Cleanup(func() { modelHTTPClient = original })
	modelHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"gpt-z"},{"id":"not-compatible"},{"id":"gpt-a"}]}`))}, nil
	})}
	models, err := fetchModelsFrom(t.Context(), "openai", "secret", "https://example.invalid/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "gpt-a" || models[1].ID != "gpt-z" {
		t.Fatalf("models=%v, want deterministic compatible list", models)
	}
}

func TestGeminiFetchKeepsOnlyGenerateContentReviewModels(t *testing.T) {
	original := modelHTTPClient
	t.Cleanup(func() { modelHTTPClient = original })
	modelHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `{"models":[` +
			`{"name":"models/gemini-embedding-001","supportedGenerationMethods":["embedContent"]},` +
			`{"name":"models/gemini-2.5-flash-image","supportedGenerationMethods":["generateContent"]},` +
			`{"name":"models/gemini-live-audio","supportedGenerationMethods":["generateContent"]},` +
			`{"name":"models/aqa","supportedGenerationMethods":["generateAnswer"]},` +
			`{"name":"models/gemini-future-text","supportedGenerationMethods":["generateContent"]},` +
			`{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent","countTokens"]}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	models, err := fetchModelsFrom(t.Context(), "gemini", "secret", "https://example.invalid/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gemini-2.5-pro", "gemini-future-text"}
	if len(models) != len(want) {
		t.Fatalf("Gemini models=%v, want %v", models, want)
	}
	for i := range want {
		if models[i].ID != want[i] {
			t.Fatalf("Gemini models=%v, want %v", models, want)
		}
	}
}

func TestOpenAIFetchExcludesNonReviewModelFamilies(t *testing.T) {
	original := modelHTTPClient
	t.Cleanup(func() { modelHTTPClient = original })
	modelHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"gpt-4o"},{"id":"gpt-image-1"},{"id":"text-embedding-3-large"},{"id":"gpt-4o-audio-preview"},{"id":"gpt-4o-speech-preview"},{"id":"gpt-4o-realtime-preview"},{"id":"tts-1"},{"id":"whisper-1"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	models, err := fetchModelsFrom(t.Context(), "openai", "secret", "https://example.invalid/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4o" {
		t.Fatalf("OpenAI models=%v, want only gpt-4o", models)
	}
}

func TestReviewCapableFilterAllowsFutureGeminiTextModelWithoutMetadata(t *testing.T) {
	if !IsReviewCapableModel("gemini", ModelInfo{ID: "gemini-4-future-text"}) {
		t.Fatal("future Gemini text model without metadata was rejected")
	}
}

func TestFallbackModelsCoverEveryProvider(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "gemini", "ollama"} {
		if got := fallbackModels(provider); len(got) == 0 {
			t.Errorf("no fallback models for %s", provider)
		}
	}
}

func TestSaveConfigUsesPrivatePermissions(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := SaveConfig(WizardConfig{Provider: "openai", APIKey: "secret", Model: "gpt-4o", OutputFmt: "markdown", SkipDirs: []string{"vendor"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(base, "lookup", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Join(base, "lookup"))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o, want 700", dirInfo.Mode().Perm())
	}
}

func TestSaveConfigPreservesProviderSpecificSecrets(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := SaveConfig(WizardConfig{Provider: "gemini", APIKey: "GEMINI_KEY", ProviderKeys: map[string]string{"openai": "OPENAI_KEY", "gemini": "GEMINI_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(base, "lookup", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, `api_key = "OPENAI_KEY"`) || !strings.Contains(text, `api_key = "GEMINI_KEY"`) {
		t.Fatalf("provider-specific keys were discarded:\n%s", text)
	}
}
