package setup

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ValidationErrorKind int

const (
	ValidationSuccess ValidationErrorKind = iota
	ValidationAuthFailure
	ValidationNetworkError
	ValidationRateLimit
)

type ValidationResult struct {
	Valid bool
	Error error
	Kind  ValidationErrorKind
}

func redactSecret(message, secret string) string {
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[REDACTED]")
}

var providerEndpoints = map[string]string{"openai": "https://api.openai.com/v1/models", "anthropic": "https://api.anthropic.com/v1/models", "gemini": "https://generativelanguage.googleapis.com/v1/models"}
var validationHTTPClient = &http.Client{Timeout: 10 * time.Second}

func ValidateAPIKey(ctx context.Context, provider, apiKey string) ValidationResult {
	endpoint, ok := providerEndpoints[provider]
	if !ok {
		return ValidationResult{Error: fmt.Errorf("unknown provider %q", provider), Kind: ValidationNetworkError}
	}
	return validateWithEndpoint(ctx, provider, apiKey, endpoint)
}
func validateWithEndpoint(ctx context.Context, provider, apiKey, endpoint string) ValidationResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := endpoint
	if !strings.Contains(endpoint, "/v1/") {
		url = strings.TrimRight(endpoint, "/") + "/v1/models"
	}
	if provider == "gemini" {
		separator := "?"
		if strings.Contains(url, "?") {
			separator = "&"
		}
		url += separator + "key=" + apiKey
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ValidationResult{Error: fmt.Errorf("create validation request: %s", redactSecret(err.Error(), apiKey)), Kind: ValidationNetworkError}
	}
	if provider == "openai" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if provider == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := validationHTTPClient.Do(req)
	if err != nil {
		safeMessage := redactSecret(err.Error(), apiKey)
		return ValidationResult{Error: fmt.Errorf("could not reach %s: %s", provider, safeMessage), Kind: ValidationNetworkError}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return ValidationResult{Valid: true, Kind: ValidationSuccess}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return ValidationResult{Error: fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode), Kind: ValidationAuthFailure}
	case resp.StatusCode == 429:
		return ValidationResult{Error: fmt.Errorf("rate limited (HTTP 429)"), Kind: ValidationRateLimit}
	default:
		return ValidationResult{Error: fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode), Kind: ValidationNetworkError}
	}
}
