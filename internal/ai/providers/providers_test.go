package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/finding"
)

const structuredReview = `{"id":"finding-1","evidence_relation":"SUPPORTS","claim_strength":"UNCHANGED","severity":"Critical","confidence":0.96,"reason":"reachable secret","recommendation":"remove it","evidence_ids":["source.primary"]}`

const structuredBatchReview = `[{"id":"finding-1","evidence_relation":"SUPPORTS","claim_strength":"UNCHANGED","severity":"Critical","confidence":0.96,"reason":"reachable secret","recommendation":"remove it","evidence_ids":["source.primary"]},{"id":"finding-2","evidence_relation":"INSUFFICIENT","reason":"safe","recommendation":"none","evidence_ids":[]}]`

func reviewRequest() ai.ReviewRequest {
	return ai.ReviewRequest{
		Finding: finding.Finding{
			ID:       "finding-1",
			RuleID:   "SEC-001",
			Category: finding.CategorySecurity,
			Severity: finding.SeverityHigh,
			Title:    "Hardcoded secret",
			Reason:   "A literal secret was detected.",
			Evidence: finding.Evidence{AffectedFiles: []string{"main.go"}},
		},
		CodeSnippet: `token := "secret"`,
		CallChain:   []string{"main", "connect"},
	}
}

func batchReviewRequests() []ai.ReviewRequest {
	first := reviewRequest()
	second := reviewRequest()
	second.Finding.ID = "finding-2"
	second.Finding.RuleID = "COR-001"
	second.Finding.Title = "Unhandled error"
	second.CodeSnippet = "_ = doWork()"
	return []ai.ReviewRequest{first, second}
}

func assertReview(t *testing.T, got ai.ReviewResponse) {
	t.Helper()
	if !got.IsRealIssue || got.Severity != finding.SeverityCritical || got.Confidence != 0.96 {
		t.Fatalf("review = %#v, want real Critical issue with 0.96 confidence", got)
	}
	if got.Reason != "reachable secret" || got.Recommendation != "remove it" {
		t.Fatalf("review = %#v, want normalized reason and recommendation", got)
	}
}

func decodeBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	defer request.Body.Close()
	data, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if !strings.Contains(string(data), "Hardcoded secret") || !strings.Contains(string(data), "token") || !strings.Contains(string(data), "secret") || !strings.Contains(string(data), "connect") {
		t.Fatalf("request body lacks focused finding context: %s", data)
	}
	return body
}

func assertSemanticReviewSchema(t *testing.T, value any) {
	t.Helper()
	schema, ok := value.(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("semantic schema = %#v, want top-level object", value)
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "assessments" {
		t.Fatalf("top-level required = %#v", schema["required"])
	}
	properties := schema["properties"].(map[string]any)
	items := properties["assessments"].(map[string]any)["items"].(map[string]any)
	variants, ok := items["anyOf"].([]any)
	if !ok || len(variants) != 3 {
		t.Fatalf("assessment variants = %#v", items)
	}
	wantRelations := map[string]bool{"SUPPORTS": true, "CONTRADICTS": true, "INSUFFICIENT": true}
	for _, raw := range variants {
		variant := raw.(map[string]any)
		props := variant["properties"].(map[string]any)
		relationEnum := props["evidence_relation"].(map[string]any)["enum"].([]any)
		if len(relationEnum) != 1 || !wantRelations[relationEnum[0].(string)] {
			t.Fatalf("relation enum = %#v", relationEnum)
		}
		relation := relationEnum[0].(string)
		delete(wantRelations, relation)
		if relation == "SUPPORTS" {
			strength := props["claim_strength"].(map[string]any)["enum"].([]any)
			if len(strength) != 2 || strength[0] != "UNCHANGED" || strength[1] != "LOWER" {
				t.Fatalf("claim strength enum = %#v", strength)
			}
			severity := props["severity"].(map[string]any)
			if severity["type"] != "string" {
				t.Fatalf("severity schema = %#v", severity)
			}
			severityEnum := severity["enum"].([]any)
			wantSeverity := []any{string(finding.SeverityCritical), string(finding.SeverityHigh), string(finding.SeverityMedium), string(finding.SeverityLow), string(finding.SeverityInfo)}
			if len(severityEnum) != len(wantSeverity) {
				t.Fatalf("severity enum = %#v", severityEnum)
			}
			for index := range wantSeverity {
				if severityEnum[index] != wantSeverity[index] {
					t.Fatalf("severity enum = %#v, want %#v", severityEnum, wantSeverity)
				}
			}
			confidence := props["confidence"].(map[string]any)
			if confidence["type"] != "number" || confidence["minimum"] != float64(0) || confidence["maximum"] != float64(1) {
				t.Fatalf("confidence schema = %#v", confidence)
			}
		} else if _, exists := props["severity"]; exists {
			t.Fatalf("%s permits SUPPORTS-only fields: %#v", relation, props)
		}
	}
	if len(wantRelations) != 0 {
		t.Fatalf("missing relation variants: %#v", wantRelations)
	}
}

func TestOpenAIReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer openai-secret" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		body := decodeBody(t, request)
		if body["model"] != "gpt-test" {
			t.Fatalf("model = %v, want gpt-test", body["model"])
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"choices":[{"message":{"content":`+quoteJSON(structuredReview)+`}}]}`)
	}))
	defer server.Close()

	provider := NewOpenAI(server.Client(), "openai-secret", "gpt-test", server.URL)
	got, err := provider.Review(context.Background(), reviewRequest())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if provider.Name() != "openai" {
		t.Fatalf("Name = %q, want openai", provider.Name())
	}
	assertReview(t, got)
}

func TestAnthropicReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", request.URL.Path)
		}
		if request.Header.Get("x-api-key") != "anthropic-secret" {
			t.Fatalf("x-api-key = %q", request.Header.Get("x-api-key"))
		}
		if request.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("anthropic-version = %q", request.Header.Get("anthropic-version"))
		}
		body := decodeBody(t, request)
		if body["model"] != "claude-test" {
			t.Fatalf("model = %v, want claude-test", body["model"])
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"content":[{"type":"text","text":`+quoteJSON(structuredReview)+`}]}`)
	}))
	defer server.Close()

	provider := NewAnthropic(server.Client(), "anthropic-secret", "claude-test", server.URL)
	got, err := provider.Review(context.Background(), reviewRequest())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	assertReview(t, got)
}

func TestGeminiReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1beta/models/gemini-test:generateContent" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("key") != "gemini-secret" {
			t.Fatalf("key query parameter = %q", request.URL.Query().Get("key"))
		}
		decodeBody(t, request)
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"candidates":[{"content":{"parts":[{"text":`+quoteJSON(structuredReview)+`}]}}]}`)
	}))
	defer server.Close()

	provider := NewGemini(server.Client(), "gemini-secret", "gemini-test", server.URL)
	got, err := provider.Review(context.Background(), reviewRequest())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	assertReview(t, got)
}

func TestOllamaReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/chat" {
			t.Fatalf("path = %q, want /api/chat", request.URL.Path)
		}
		body := decodeBody(t, request)
		if body["model"] != "llama-test" || body["stream"] != false {
			t.Fatalf("body = %#v, want llama-test with stream=false", body)
		}
		options, ok := body["options"].(map[string]any)
		if !ok || options["num_predict"] != float64(448) || options["num_ctx"] != float64(4096) {
			t.Fatalf("Ollama options = %#v", body["options"])
		}
		if body["truncate"] != false {
			t.Fatalf("truncate = %#v, want false", body["truncate"])
		}
		assertSemanticReviewSchema(t, body["format"])
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"message":{"content":`+quoteJSON(structuredReview)+`}}`)
	}))
	defer server.Close()

	provider := NewOllama(server.Client(), "llama-test", server.URL)
	got, err := provider.Review(context.Background(), reviewRequest())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	assertReview(t, got)
}

func TestOpenAIReviewBatchUsesOneRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		body := decodeBody(t, request)
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), "finding-1") || !strings.Contains(string(encoded), "finding-2") {
			t.Fatalf("batch payload lacks finding IDs: %s", encoded)
		}
		_, _ = io.WriteString(response, `{"choices":[{"message":{"content":`+quoteJSON(structuredBatchReview)+`}}]}`)
	}))
	defer server.Close()

	got, err := NewOpenAI(server.Client(), "key", "model", server.URL).ReviewBatch(context.Background(), batchReviewRequests())
	if err != nil || requests != 1 || len(got) != 2 || got[1].ID != "finding-2" {
		t.Fatalf("requests = %d, responses = %#v, error = %v", requests, got, err)
	}
}

func TestAnthropicReviewBatchUsesOneRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		decodeBody(t, request)
		_, _ = io.WriteString(response, `{"content":[{"type":"text","text":`+quoteJSON(structuredBatchReview)+`}]}`)
	}))
	defer server.Close()

	got, err := NewAnthropic(server.Client(), "key", "model", server.URL).ReviewBatch(context.Background(), batchReviewRequests())
	if err != nil || requests != 1 || len(got) != 2 {
		t.Fatalf("requests = %d, responses = %#v, error = %v", requests, got, err)
	}
}

func TestGeminiReviewBatchUsesOneRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		body := decodeBody(t, request)
		generation := body["generationConfig"].(map[string]any)
		schema, ok := generation["responseJsonSchema"]
		if !ok {
			t.Fatalf("generationConfig does not contain responseJsonSchema: %#v", generation)
		}
		if _, legacy := generation["responseSchema"]; legacy {
			t.Fatalf("generationConfig must not use legacy responseSchema: %#v", generation)
		}
		assertSemanticReviewSchema(t, schema)
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), `"assessments"`) || !strings.Contains(string(encoded), `"evidence_relation"`) || strings.Contains(string(encoded), `"verdict"`) {
			t.Fatalf("Gemini schema does not match canonical assessment envelope: %s", encoded)
		}
		_, _ = io.WriteString(response, `{"candidates":[{"content":{"parts":[{"text":`+quoteJSON(structuredBatchReview)+`}]}}]}`)
	}))
	defer server.Close()

	got, err := NewGemini(server.Client(), "key", "model", server.URL).ReviewBatch(context.Background(), batchReviewRequests())
	if err != nil || requests != 1 || len(got) != 2 {
		t.Fatalf("requests = %d, responses = %#v, error = %v", requests, got, err)
	}
}

func TestAnthropicOutputBudgetScalesForLargeBatch(t *testing.T) {
	if got := anthropicOutputTokens(20); got <= 1024 {
		t.Fatalf("output tokens = %d", got)
	}
	if got := anthropicOutputTokens(1000); got > 8192 {
		t.Fatalf("output tokens exceeds cap: %d", got)
	}
}

func TestOllamaReviewBatchUsesOneRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		decodeBody(t, request)
		_, _ = io.WriteString(response, `{"message":{"content":`+quoteJSON(structuredBatchReview)+`}}`)
	}))
	defer server.Close()

	got, err := NewOllama(server.Client(), "model", server.URL).ReviewBatch(context.Background(), batchReviewRequests())
	if err != nil || requests != 1 || len(got) != 2 {
		t.Fatalf("requests = %d, responses = %#v, error = %v", requests, got, err)
	}
}

func TestOllamaUsesExtendedLocalInferenceTimeout(t *testing.T) {
	provider := NewOllama(&http.Client{}, "model", "http://localhost:11434")
	if got := provider.client.Timeout; got != 180*time.Second {
		t.Fatalf("Ollama timeout = %v, want 180s", got)
	}
}

func TestOpenAIRejectsHTTPErrorWithoutLeakingCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(response, `{"error":"invalid credential"}`)
	}))
	defer server.Close()

	_, err := NewOpenAI(server.Client(), "never-leak-this", "gpt-test", server.URL).Review(context.Background(), reviewRequest())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want HTTP 401 error", err)
	}
	if strings.Contains(err.Error(), "never-leak-this") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestOpenAIRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"choices":[{"message":{"content":"not-json"}}]}`)
	}))
	defer server.Close()

	if _, err := NewOpenAI(server.Client(), "key", "model", server.URL).Review(context.Background(), reviewRequest()); err == nil {
		t.Fatal("Review returned nil error for malformed structured response")
	}
}

func TestReviewPropagatesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewOllama(http.DefaultClient, "model", "http://127.0.0.1:1").Review(ctx, reviewRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestGeminiTransportErrorDoesNotLeakCredential(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection failed")
	})}

	_, err := NewGemini(client, "never-leak-gemini-key", "model", "https://example.invalid").Review(context.Background(), reviewRequest())
	if err == nil {
		t.Fatal("Review returned nil error for transport failure")
	}
	if strings.Contains(err.Error(), "never-leak-gemini-key") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func quoteJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
