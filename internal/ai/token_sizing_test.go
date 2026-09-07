package ai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestGenericPromptEstimatorIsDeterministicVersionedAndObservable(t *testing.T) {
	input := "func compact(v string) bool { return v != \"\" && strings.TrimSpace(v) != \"\" }"
	first := EstimateGenericPromptTokens(input)
	second := EstimateGenericPromptTokens(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic estimates: %#v != %#v", first, second)
	}
	if first.Source != TokenCountEstimated || first.EstimatorVersion == "" || first.Tokens <= 0 || first.TemplateIncluded {
		t.Fatalf("estimate lacks normalized provenance: %#v", first)
	}
	if first.Tokens >= len([]byte(input)) {
		t.Fatalf("ordinary source estimate=%d is not meaningfully below raw bytes=%d", first.Tokens, len([]byte(input)))
	}
}

func TestGenericPromptEstimatorCoversCrossTokenizerCalibrationCorpus(t *testing.T) {
	type fixture struct {
		name  string
		text  string
		qwen  int
		gemma int
		nomic int
	}
	fixtures := []fixture{
		{"go", "package main\nimport \"crypto/sha256\"\nfunc digest(input []byte) [32]byte { return sha256.Sum256(input) }", 35, 41, 39},
		{"python", "def normalize(values: list[str]) -> list[str]:\n    return [value.strip().lower() for value in values if value]", 26, 32, 40},
		{"rust", "pub fn checked_sum(values: &[u64]) -> Option<u64> {\n    values.iter().try_fold(0, |sum, value| sum.checked_add(*value))\n}", 39, 49, 54},
		{"csharp", "public async Task<User?> FindAsync(Guid id, CancellationToken ct) {\n    return await db.Users.SingleOrDefaultAsync(x => x.Id == id, ct);\n}", 31, 42, 54},
		{"canonical_json", `{"finding_id":"abc123","rule_id":"GO-SEC-002","observation":"A legacy digest is used.","hypothesis":"The digest may protect security-sensitive material.","context_sufficiency":"INCOMPLETE","missing_information":["callee context unavailable"],"evidence":[{"id":"source.primary","kind":"primary","content":"sum := md5.Sum(secret)"}]}`, 78, 84, 129},
		{"english", "A deterministic context planner must reserve enough space for the model response while retaining the evidence required to adjudicate the finding.", 23, 24, 28},
		{"punctuation", strings.Repeat("{}[]():,.;_+-=*/\\\\|&!?<>@#$%^~\n", 40), 680, 761, 1202},
		{"long_identifiers", strings.Repeat("veryLongGeneratedIdentifier_0123456789abcdef0123456789abcdef ", 30), 811, 812, 872},
		{"compact", strings.Repeat(`{"a":[1,2,3],"b":false,"c":"0123456789abcdef"};fn(x){return(x??0)+1}`, 25), 950, 976, 1327},
		{"unicode", strings.Repeat("Merhaba dünya — güvenli bağlam ölçümü. 你好世界。こんにちは世界。Привет мир. مرحبا بالعالم. ", 20), 621, 501, 1022},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			got := EstimateGenericPromptTokens(fixture.text).Tokens
			for family, actual := range map[string]int{"qwen": fixture.qwen, "gemma": fixture.gemma, "nomic": fixture.nomic} {
				if got < actual {
					t.Fatalf("estimate=%d undercounts %s actual=%d", got, family, actual)
				}
			}
		})
	}
}

func TestGenericPromptEstimatorCoversObservedReviewPromptShapes(t *testing.T) {
	// These aggregate shapes and actual counts came from the six Qwen requests
	// that exposed the generic /4 estimator defect. Production code has no model branch.
	fixtures := []struct{ bytes, alnum, structural, nonASCII, actual int }{
		{13220, 10041, 3179, 0, 3884},
		{13261, 9979, 3282, 0, 3980},
		{13384, 10244, 3140, 0, 4061},
		{13919, 10389, 3530, 0, 4233},
		{13433, 10008, 3425, 0, 4076},
		{13853, 10383, 3470, 0, 4129},
	}
	for index, fixture := range fixtures {
		text := strings.Repeat("a", fixture.alnum) + strings.Repeat(" ", fixture.structural) + strings.Repeat("é", fixture.nonASCII/2)
		if len([]byte(text)) != fixture.bytes {
			t.Fatalf("fixture %d bytes=%d, want %d", index, len([]byte(text)), fixture.bytes)
		}
		got := EstimateGenericPromptTokens(text)
		if got.Tokens+128 < fixture.actual {
			t.Fatalf("fixture %d estimate+allowance=%d undercounts actual=%d", index, got.Tokens+128, fixture.actual)
		}
		if (index == 3 || index == 5) && NewContextEstimate(got, 448, 128, 4096).Fits() {
			t.Fatalf("fixture %d previously overflowing shape passed local context policy: %#v", index, got)
		}
	}
}

type fakePromptTokenCounter struct {
	count    PromptTokenCount
	err      error
	received []ReviewRequest
}

func (counter *fakePromptTokenCounter) CountPromptTokens(_ context.Context, requests []ReviewRequest) (PromptTokenCount, error) {
	counter.received = append([]ReviewRequest(nil), requests...)
	return counter.count, counter.err
}

func TestPromptSizingPrefersExactCounterAndCountsLogicalRequest(t *testing.T) {
	requests := []ReviewRequest{batchRequest("exact", "source")}
	counter := &fakePromptTokenCounter{count: PromptTokenCount{Tokens: 321, TemplateIncluded: true, Source: TokenCountExact}}
	got, err := (PromptSizingStrategy{Counter: counter}).CountPromptTokens(context.Background(), requests)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tokens != 321 || got.Source != TokenCountExact || !reflect.DeepEqual(counter.received, requests) {
		t.Fatalf("exact count not preferred or logical request drifted: got=%#v received=%#v", got, counter.received)
	}
	estimate := NewContextEstimate(got, 448, 128, 4096)
	if estimate.TemplateAllowance != 0 {
		t.Fatalf("template-included exact count was charged allowance twice: %#v", estimate)
	}
}

func TestPromptSizingFallsBackWhenExactCounterFails(t *testing.T) {
	requests := []ReviewRequest{batchRequest("fallback", "source")}
	counter := &fakePromptTokenCounter{err: errors.New("counter unavailable")}
	got, err := (PromptSizingStrategy{Counter: counter}).CountPromptTokens(context.Background(), requests)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != TokenCountEstimated || got.EstimatorVersion != GenericTokenEstimatorVersion || got.Tokens <= 0 {
		t.Fatalf("counter failure did not use generic fallback: %#v", got)
	}
}

func TestContextEstimateUsesSingleAuthoritativeEquation(t *testing.T) {
	fit := NewContextEstimate(PromptTokenCount{Tokens: 3520, Source: TokenCountEstimated}, 448, 128, 4096)
	if fit.PredictedUse() != 4096 || !fit.Fits() {
		t.Fatalf("boundary estimate should fit: %#v", fit)
	}
	overflow := fit
	overflow.PromptTokens++
	if overflow.Fits() {
		t.Fatalf("overflow estimate should not fit: %#v", overflow)
	}
}
