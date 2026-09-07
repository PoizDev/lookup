package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestBuildOllamaRequestUsesExecutableContextContract(t *testing.T) {
	requests := []ReviewRequest{{Finding: finding.Finding{ID: "f-1", RuleID: "R", Title: "title"}, CodeSnippet: "code"}}
	request, err := BuildOllamaRequest(requests, 6144)
	if err != nil {
		t.Fatal(err)
	}
	if request.NumCtx != 6144 || request.NumPredict != 448 {
		t.Fatalf("options = num_ctx:%d num_predict:%d", request.NumCtx, request.NumPredict)
	}
	if request.ChatTemplateAllowance != 128 {
		t.Fatalf("chat-template allowance = %d", request.ChatTemplateAllowance)
	}
	wantEstimate := EstimateGenericPromptTokens(request.Prompt.System + request.Prompt.User).Tokens
	if request.PromptEstimate != wantEstimate {
		t.Fatalf("prompt estimate = %d, want token estimate %d", request.PromptEstimate, wantEstimate)
	}
	payload, err := json.Marshal(request.Payload("model"))
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= request.PromptEstimate {
		t.Fatal("fixture must contain JSON transport overhead")
	}
	if request.ContextUse() != wantEstimate+448+128 {
		t.Fatalf("context use = %d", request.ContextUse())
	}
	if request.CountSource != TokenCountEstimated || request.EstimatorVersion != GenericTokenEstimatorVersion {
		t.Fatalf("sizing provenance = source:%q version:%q", request.CountSource, request.EstimatorVersion)
	}
}

func TestOllamaPayloadUsesSemanticReviewSchemaObject(t *testing.T) {
	request, err := BuildOllamaRequest([]ReviewRequest{{Finding: finding.Finding{ID: "f-1"}}}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	format, ok := request.Payload("model")["format"].(map[string]any)
	if !ok || format["type"] != "object" {
		t.Fatalf("format = %#v, want JSON Schema object", request.Payload("model")["format"])
	}
}

func TestBuildOllamaRequestUsesExactSizingWithoutDoubleTemplateAllowance(t *testing.T) {
	counter := &fakePromptTokenCounter{count: PromptTokenCount{Tokens: 3000, TemplateIncluded: true, Source: TokenCountExact}}
	request, err := BuildOllamaRequestWithSizing(context.Background(), []ReviewRequest{{Finding: finding.Finding{ID: "exact"}}}, 4096, PromptSizingStrategy{Counter: counter})
	if err != nil {
		t.Fatal(err)
	}
	if request.PromptEstimate != 3000 || request.ChatTemplateAllowance != 0 || request.ContextUse() != 3448 || request.CountSource != TokenCountExact {
		t.Fatalf("exact sizing contract = %#v", request)
	}
}

func TestBuildPromptIncludesCorrelatedFocusedContext(t *testing.T) {
	requests := []ReviewRequest{{
		Finding: finding.Finding{
			ID:       "finding-1",
			RuleID:   "SEC-001",
			Category: finding.CategorySecurity,
			Severity: finding.SeverityHigh,
			Title:    "Hardcoded secret",
		},
		CodeSnippet: "token := input",
		CallChain:   []string{"main", "connect"},
	}}

	prompt, err := BuildPrompt(requests)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{
		"finding-1", "SEC-001", "Hardcoded secret", "token := input",
		"connect", "evidence_relation", "claim_strength", "recommendation",
	} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt user content lacks %q: %s", want, prompt.User)
		}
	}
	if !strings.Contains(prompt.System, "verify") {
		t.Fatalf("system prompt does not constrain the model to verification: %s", prompt.System)
	}
	if !strings.Contains(prompt.User, `{"assessments":`) {
		t.Fatalf("prompt lacks canonical envelope: %s", prompt.User)
	}
}

func TestBuildPromptRequestsAuthoritativeBoundedAdjudication(t *testing.T) {
	prompt, err := BuildPrompt([]ReviewRequest{{Finding: finding.Finding{ID: "f-1", Severity: finding.SeverityHigh, Confidence: .8, Observation: "A type assertion exists.", Hypothesis: "It may panic."}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SUPPORTS", "CONTRADICTS", "INSUFFICIENT", "UNCHANGED", "LOWER", "increase severity or confidence"} {
		if !strings.Contains(prompt.System+prompt.User, want) {
			t.Fatalf("prompt missing %q: %#v", want, prompt)
		}
	}
	for _, forbidden := range []string{"non-authoritative assessment", "Never create, delete, reprioritize, or alter"} {
		if strings.Contains(prompt.System+prompt.User, forbidden) {
			t.Fatalf("prompt retains forbidden restriction %q", forbidden)
		}
	}
	if !strings.Contains(prompt.User, "severity must be one") || !strings.Contains(prompt.User, "severity STRING enum") || !strings.Contains(prompt.User, "confidence must be a numeric value from 0 through 1") {
		t.Fatalf("prompt does not distinguish string severity from numeric confidence: %s", prompt.User)
	}
}

func TestBuildPromptRequiresFalsificationAndDefinesNegativeVerdicts(t *testing.T) {
	prompt, err := BuildPrompt([]ReviewRequest{{Finding: finding.Finding{ID: "f-1", Severity: finding.SeverityHigh, Confidence: .8, Observation: "A call occurs.", Hypothesis: "The error may be ignored."}}})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(prompt.System + " " + prompt.User)
	for _, want := range []string{"falsif", "counterevidence", "existing error handling", "invariant", "bounded lifecycle", "supports", "contradicts", "insufficient", "cannot reliably establish", "positive supplied repository evidence"} {
		if !strings.Contains(text, want) {
			t.Fatalf("adversarial prompt missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "the observation is deterministic") {
		t.Fatalf("prompt anchors reviewer to analyzer premise: %s", text)
	}
}

func TestBuildPromptLimitsEachSnippetToOneHundredLines(t *testing.T) {
	prompt, err := BuildPrompt([]ReviewRequest{{
		Finding:     finding.Finding{ID: "finding-1"},
		CodeSnippet: strings.Repeat("safe line\n", 100) + "NEVER_SEND",
	}})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if strings.Contains(prompt.User, "NEVER_SEND") {
		t.Fatal("prompt leaked code beyond the 100-line limit")
	}
}

func TestBuildPromptRejectsEmptyBatch(t *testing.T) {
	if _, err := BuildPrompt(nil); err == nil {
		t.Fatal("BuildPrompt returned nil error for an empty batch")
	}
}

func TestBuildPromptUsesCanonicalContextWithoutDuplicateSnippet(t *testing.T) {
	request := ReviewRequest{Finding: finding.Finding{ID: "f1", RuleID: "SEC-1", Severity: finding.SeverityHigh, Confidence: .7, Title: "SQL", Evidence: finding.Evidence{CodeSnippet: "duplicate-code", CallChain: []string{"duplicate-chain"}}}, CodeSnippet: "duplicate-code", CallChain: []string{"duplicate-chain"}}
	prompt, err := BuildPrompt([]ReviewRequest{request})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(prompt.User, "duplicate-code") != 1 || strings.Count(prompt.User, "duplicate-chain") != 1 {
		t.Fatalf("canonical context duplicated data: %s", prompt.User)
	}
	if strings.Contains(prompt.User, "ai_reviewed") || strings.Contains(prompt.User, "recommendation\":\"") {
		t.Fatalf("prompt serialized unrelated finding fields: %s", prompt.User)
	}
}

func TestBuildPromptRedactsSecretsWithStablePlaceholders(t *testing.T) {
	secret := "sk-proj-1234567890abcdefghijkl"
	prompt, err := BuildPrompt([]ReviewRequest{{Finding: finding.Finding{ID: "f1", RuleID: "SEC-1", Title: "credential"}, CodeSnippet: "first := \"" + secret + "\"\nsecond := \"" + secret + "\"\nlabel := \"ordinary-string\""}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt.User, secret) || strings.Count(prompt.User, "<REDACTED_SECRET_1>") != 2 {
		t.Fatalf("secret redaction = %s", prompt.User)
	}
	if !strings.Contains(prompt.User, "ordinary-string") {
		t.Fatalf("ordinary string redacted: %s", prompt.User)
	}
}

func TestBuildPromptRedactsCredentialValueButPreservesCodeStructure(t *testing.T) {
	prompt, err := BuildPrompt([]ReviewRequest{{Finding: finding.Finding{ID: "f1"}, CodeSnippet: `password := "super-secret-value"`}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.User, `password := \"<REDACTED_SECRET_1>\"`) || strings.Contains(prompt.User, "super-secret-value") {
		t.Fatalf("redaction damaged structure: %s", prompt.User)
	}
}
