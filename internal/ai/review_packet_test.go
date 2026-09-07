package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

func packetRequest(content string) ReviewRequest {
	item := adjudicationCandidate()
	packet := reviewcontext.ReviewPacket{
		Candidate:   finding.PotentialFinding{Finding: item, Observation: "Observed assertion", Hypothesis: "May panic", ReviewPolicy: finding.ReviewPolicyContextRequired},
		Evidence:    []reviewcontext.EvidenceItem{{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: content}, {ID: "guard.1", Kind: reviewcontext.EvidenceGuard, Content: "if ok"}},
		Sufficiency: reviewcontext.SufficiencySufficient,
	}
	return ReviewRequest{Finding: item, Packet: packet}
}

func TestPromptRendersStructuredPacketAndRedactsExpandedEvidence(t *testing.T) {
	request := packetRequest(`token := "sk-proj-123456789abcdefghijklmnop"`)
	request.Packet.Candidate.Title = `token := "sk-proj-title123456789abcdefghijklmnop"`
	request.Packet.Candidate.Evidence.CodeSnippet = `token := "sk-proj-snippet123456789abcdefghijklmnop"`
	prompt, err := BuildPrompt([]ReviewRequest{request})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Observed assertion", "May panic", "source.primary", "context_sufficiency", "missing_information", "prefer INSUFFICIENT"} {
		if !strings.Contains(prompt.System+prompt.User, want) {
			t.Fatalf("prompt missing %q: %#v", want, prompt)
		}
	}
	if strings.Contains(prompt.User, "sk-proj-123456789abcdefghijklmnop") || strings.Contains(prompt.User, "sk-proj-title123456789abcdefghijklmnop") || strings.Contains(prompt.User, "sk-proj-snippet123456789abcdefghijklmnop") || !strings.Contains(prompt.User, "REDACTED_SECRET") {
		t.Fatalf("expanded packet was not redacted: %s", prompt.User)
	}
}

func TestCanonicalPacketOmitsDuplicateIndexesAndLegacyCandidateEvidence(t *testing.T) {
	request := packetRequest("primary")
	giant := strings.Repeat("duplicate", 10000)
	request.Packet.Provenance = []reviewcontext.EvidenceItem{{ID: "provenance.1", Content: giant}}
	request.Packet.Callers = []reviewcontext.EvidenceItem{{ID: "caller.1", Content: giant}}
	request.Packet.Candidate.Evidence.CodeSnippet = giant
	encoded, err := BuildPrompt([]ReviewRequest{request})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded.User, giant) || strings.Contains(encoded.User, "provenance.1") || strings.Contains(encoded.User, "caller.1") {
		t.Fatal("canonical packet serialized a duplicate or legacy evidence view")
	}
}

func TestNormalizeDecisionRejectsInventedEvidenceForFinalVerdict(t *testing.T) {
	request := packetRequest("assertion")
	response := ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityHigh, Confidence: .7, Reason: "supported", EvidenceIDs: []string{"invented.1"}}
	decision, err := NormalizeDecisionWithPacket(request.Packet, response, "fake", "m")
	if err != nil || decision.Status != finding.AdjudicationUncertain {
		t.Fatalf("decision = %#v, err = %v, want fail-closed UNCERTAIN", decision, err)
	}
}

func TestNormalizeDecisionRequiresValidCounterevidenceForContradiction(t *testing.T) {
	request := packetRequest("assertion")
	response := ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceContradicts, Reason: "guard falsifies claim", EvidenceIDs: []string{"guard.1"}}
	decision, err := NormalizeDecisionWithPacket(request.Packet, response, "fake", "m")
	if err != nil || decision.Status != finding.AdjudicationUnsupported || len(decision.EvidenceIDs) != 1 {
		t.Fatalf("decision = %#v, err = %v", decision, err)
	}
}

func TestNormalizeDecisionWithPacketDerivesSupportedAndInsufficientVerdicts(t *testing.T) {
	request := packetRequest("assertion")
	tests := []struct {
		name     string
		response ReviewResponse
		want     finding.AdjudicationStatus
	}{
		{"confirmed", ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityHigh, Confidence: .8, Reason: "supported", EvidenceIDs: []string{"source.primary"}}, finding.AdjudicationConfirmed},
		{"downgraded", ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthLower, Severity: finding.SeverityMedium, Confidence: .6, Reason: "weaker", EvidenceIDs: []string{"source.primary"}}, finding.AdjudicationDowngraded},
		{"uncertain", ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceInsufficient, Reason: "missing lifecycle context"}, finding.AdjudicationUncertain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := NormalizeDecisionWithPacket(request.Packet, test.response, "fake", "m")
			if err != nil || decision.Status != test.want {
				t.Fatalf("decision = %#v, err = %v, want %s", decision, err, test.want)
			}
		})
	}
}

func TestTriageMakesInvalidEvidenceUncertainWithoutDiscardingNeighbors(t *testing.T) {
	requests := []ReviewRequest{packetRequest("one"), packetRequest("two")}
	requests[0].Finding.ID, requests[0].Packet.Candidate.ID = "f-1", "f-1"
	requests[1].Finding.ID, requests[1].Packet.Candidate.ID = "f-2", "f-2"
	provider := &packetResponseProvider{responses: []ReviewResponse{
		{ID: "f-1", EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityHigh, Confidence: .7, Reason: "supported", EvidenceIDs: []string{"invented.1"}},
		{ID: "f-2", EvidenceRelation: EvidenceContradicts, Reason: "not supported", EvidenceIDs: []string{"guard.1"}},
	}}
	items := []finding.Finding{requests[0].Finding, requests[1].Finding}
	plan := ReviewPlan{SelectedIDs: []string{"f-1", "f-2"}, Selected: []PlannedCandidate{{CandidateID: "f-1", Request: requests[0]}, {CandidateID: "f-2", Request: requests[1]}}, Batches: [][]ReviewRequest{requests}}
	got, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, Cache: NewAssessmentCache(""), Plan: &plan}, nil).ReviewFindings(context.Background(), items, nil)
	if stats.ProviderReviews != 2 || got[0].Adjudication.Status != finding.AdjudicationUncertain || got[1].Adjudication.Status != finding.AdjudicationUnsupported {
		t.Fatalf("stats=%#v findings=%#v", stats, got)
	}
}

type packetResponseProvider struct{ responses []ReviewResponse }

func (*packetResponseProvider) Name() string { return "packet-responses" }
func (provider *packetResponseProvider) Review(context.Context, ReviewRequest) (ReviewResponse, error) {
	return provider.responses[0], nil
}
func (provider *packetResponseProvider) ReviewBatch(context.Context, []ReviewRequest) ([]ReviewResponse, error) {
	return append([]ReviewResponse(nil), provider.responses...), nil
}

func TestNormalizeDecisionRejectsInventedCounterevidenceReferences(t *testing.T) {
	request := packetRequest("assertion")
	response := ReviewResponse{ID: "f-1", EvidenceRelation: EvidenceContradicts, Reason: "not supported", EvidenceIDs: []string{"guard.1", "invented.1"}}
	decision, err := NormalizeDecisionWithPacket(request.Packet, response, "fake", "m")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != finding.AdjudicationUncertain || len(decision.EvidenceIDs) != 0 {
		t.Fatalf("decision = %#v, want fail-closed UNCERTAIN", decision)
	}
}

func TestCacheIdentityTracksCanonicalMaterialPacketContext(t *testing.T) {
	cache := NewAssessmentCache(t.TempDir())
	meta := CacheMetadata{Provider: "fake", Model: "m", PromptSchema: PromptSchemaVersion}
	first := packetRequest("same")
	identical := packetRequest("same")
	changed := packetRequest("materially changed")
	if cache.key(first, meta) != cache.key(identical, meta) {
		t.Fatal("identical packets have different cache keys")
	}
	if cache.key(first, meta) == cache.key(changed, meta) {
		t.Fatal("material context change reused cache key")
	}
}

func TestBatchMinimizationPreservesPrimaryPacketEvidence(t *testing.T) {
	request := packetRequest("direct source")
	request.Packet.Evidence = append(request.Packet.Evidence, reviewcontext.EvidenceItem{ID: "source.enclosing", Kind: reviewcontext.EvidenceEnclosing, Content: strings.Repeat("x", 8000)})
	minimized := minimizeToBudget(request, 512)
	found := false
	for _, item := range minimized.Packet.Evidence {
		if item.ID == "source.primary" {
			found = true
		}
	}
	if !found {
		t.Fatalf("primary evidence removed: %#v", minimized.Packet.Evidence)
	}
	if !minimized.Packet.Metadata.Truncated {
		t.Fatal("packet truncation was not marked")
	}
	if minimized.Packet.Metadata.EvidenceCount != len(minimized.Packet.Evidence) {
		t.Fatalf("stale evidence count: %#v", minimized.Packet.Metadata)
	}
	if EstimateBatchTokens([]ReviewRequest{minimized}) > 512 {
		t.Fatalf("minimized request still exceeds budget: %d", EstimateBatchTokens([]ReviewRequest{minimized}))
	}
}
