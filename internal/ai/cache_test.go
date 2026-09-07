package ai

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
)

func TestAssessmentCacheHitAndNaturalInvalidation(t *testing.T) {
	cache := NewAssessmentCache(t.TempDir())
	request := batchRequest("f1", "source-v1")
	meta := CacheMetadata{Provider: "openai", Model: "m1", PromptSchema: PromptSchemaVersion, RuleVersion: "v1"}
	want := finding.AIAssessment{EvidenceRelation: string(EvidenceSupports), ClaimStrength: string(ClaimStrengthUnchanged), Verdict: finding.AIVerdictLikelyReal, Severity: finding.SeverityHigh, Confidence: .9, Reason: "reachable"}
	if _, ok := cache.Get(request, meta); ok {
		t.Fatal("first lookup hit")
	}
	cache.Put(request, meta, want)
	if got, ok := cache.Get(request, meta); !ok || got.Verdict != want.Verdict {
		t.Fatalf("cache get = %#v, %v", got, ok)
	}
	changed := request
	changed.CodeSnippet = "source-v2"
	if _, ok := cache.Get(changed, meta); ok {
		t.Fatal("source change did not invalidate")
	}
	meta.Model = "m2"
	if _, ok := cache.Get(request, meta); ok {
		t.Fatal("model change did not invalidate")
	}
	meta.Model = "m1"
	meta.PromptSchema = "next-schema"
	if _, ok := cache.Get(request, meta); ok {
		t.Fatal("prompt schema change did not invalidate")
	}
}

func TestPreviousSemanticReviewSchemaCannotHitCurrentCache(t *testing.T) {
	cache := NewAssessmentCache(t.TempDir())
	request := ReviewRequest{Finding: finding.Finding{ID: "semantic-contract", RuleID: "GO-COR-002"}}
	current := CacheMetadata{Provider: "fake", Model: "m", PromptSchema: PromptSchemaVersion, RuleVersion: "finding-v2"}
	previous := current
	previous.PromptSchema = "semantic-review-packet-v2"
	if cache.key(request, current) == cache.key(request, previous) {
		t.Fatal("previous semantic review schema still matches current cache identity")
	}
}

func TestAssessmentCacheIgnoresCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	cache := NewAssessmentCache(dir)
	request := batchRequest("f1", "source")
	meta := CacheMetadata{Provider: "fake", Model: "m", PromptSchema: PromptSchemaVersion}
	path := filepath.Join(dir, cache.key(request, meta)+".json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get(request, meta); ok {
		t.Fatal("corrupt cache entry was accepted")
	}
}

func TestAssessmentCacheIgnoresInvalidAssessment(t *testing.T) {
	dir := t.TempDir()
	cache := NewAssessmentCache(dir)
	request := batchRequest("f1", "source")
	meta := CacheMetadata{Provider: "fake", Model: "m", PromptSchema: PromptSchemaVersion}
	path := filepath.Join(dir, cache.key(request, meta)+".json")
	if err := os.WriteFile(path, []byte(`{"verdict":"impossible","confidence":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get(request, meta); ok {
		t.Fatal("invalid cache assessment was accepted")
	}
}

func TestValidAssessmentAcceptsDowngradedAndRejectsMalformedSeverity(t *testing.T) {
	valid := finding.AIAssessment{EvidenceRelation: string(EvidenceSupports), ClaimStrength: string(ClaimStrengthLower), Verdict: finding.AdjudicationDowngraded, Severity: finding.SeverityMedium, Confidence: .5, Reason: "lower impact"}
	if !validAssessment(valid) {
		t.Fatal("valid downgraded assessment rejected")
	}
	valid.Severity = "Extreme"
	if validAssessment(valid) {
		t.Fatal("assessment with malformed severity accepted")
	}
}
