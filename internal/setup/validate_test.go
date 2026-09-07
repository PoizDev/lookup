package setup

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type validationRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn validationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestValidationClassifiesProviderResponses(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   ValidationErrorKind
		valid  bool
	}{{200, ValidationSuccess, true}, {401, ValidationAuthFailure, false}, {403, ValidationAuthFailure, false}, {429, ValidationRateLimit, false}, {503, ValidationNetworkError, false}} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			original := validationHTTPClient
			t.Cleanup(func() { validationHTTPClient = original })
			validationHTTPClient = &http.Client{Transport: validationRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			result := validateWithEndpoint(t.Context(), "openai", "secret", "https://example.invalid/v1/models")
			if result.Valid != test.valid || result.Kind != test.kind {
				t.Fatalf("status %d result=%+v", test.status, result)
			}
		})
	}
}

func TestGeminiValidationTransportErrorRedactsAPIKey(t *testing.T) {
	original := validationHTTPClient
	t.Cleanup(func() { validationHTTPClient = original })
	validationHTTPClient = &http.Client{Transport: validationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("failed URL " + request.URL.String())
	})}
	result := validateWithEndpoint(t.Context(), "gemini", "never-leak-this-key", "https://example.invalid/v1/models")
	if result.Error == nil || strings.Contains(result.Error.Error(), "never-leak-this-key") {
		t.Fatalf("validation error leaked API key: %v", result.Error)
	}
}
