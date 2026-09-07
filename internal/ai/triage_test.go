package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

func supportedTestResponse(request ReviewRequest, reason string) ReviewResponse {
	evidenceIDs := []string(nil)
	if len(request.Packet.Evidence) > 0 {
		evidenceIDs = []string{request.Packet.Evidence[0].ID}
	}
	return ReviewResponse{ID: request.Finding.ID, EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: request.Finding.Severity, Confidence: request.Finding.Confidence, Reason: reason, Recommendation: "fix", EvidenceIDs: evidenceIDs}
}

type boundedProvider struct {
	mu                     sync.Mutex
	active, maximum, calls int
	httpAttempts           int
	attemptsPerCall        int
	failLarge              bool
	failAll                bool
	failErr                error
}

func TestTriageBuildsContextOnlyForEligibleCandidates(t *testing.T) {
	items := []finding.Finding{
		{ID: "eligible", Category: finding.CategoryArchitecture, Severity: finding.SeverityMedium, Confidence: .7, ReviewPolicy: finding.ReviewPolicyContextRequired},
		{ID: "static", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: 1, ReviewPolicy: finding.ReviewPolicyStaticAuthoritative},
	}
	built := 0
	provider := &boundedProvider{}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeSmart, Cache: NewAssessmentCache(""), BuildContext: func(item finding.Finding) reviewcontext.ReviewPacket { built++; return sufficientPacket(item) }}, nil)
	_, _ = reviewer.ReviewFindings(context.Background(), items, nil)
	if built != 1 {
		t.Fatalf("context builds = %d, want 1 eligible candidate", built)
	}
}

func TestInsufficientContextShortCircuitsToUncertain(t *testing.T) {
	provider := &boundedProvider{}
	item := finding.Finding{ID: "missing", Category: finding.CategoryArchitecture, Severity: finding.SeverityHigh, Confidence: .8, ReviewPolicy: finding.ReviewPolicyContextRequired}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, Cache: NewAssessmentCache(""), BuildContext: func(item finding.Finding) reviewcontext.ReviewPacket {
		packet := sufficientPacket(item)
		packet.Sufficiency = reviewcontext.SufficiencyInsufficient
		packet.MissingInformation = []string{"primary source unavailable"}
		return packet
	}}, nil)
	got, stats := reviewer.ReviewFindings(context.Background(), []finding.Finding{item}, nil)
	if provider.calls != 0 || stats.ContextInsufficient != 1 || got[0].Adjudication == nil || got[0].Adjudication.Status != finding.AdjudicationUncertain {
		t.Fatalf("calls=%d stats=%#v result=%#v", provider.calls, stats, got)
	}
}

func TestReviewModeOffDoesNotBuildContext(t *testing.T) {
	built := 0
	reviewer := NewTriageReviewer(&boundedProvider{}, TriageOptions{Mode: ReviewModeOff, BuildContext: func(item finding.Finding) reviewcontext.ReviewPacket { built++; return sufficientPacket(item) }}, nil)
	_, _ = reviewer.ReviewFindings(context.Background(), []finding.Finding{{ID: "f"}}, nil)
	if built != 0 {
		t.Fatalf("AI-off built %d contexts", built)
	}
}

func sufficientPacket(item finding.Finding) reviewcontext.ReviewPacket {
	return reviewcontext.ReviewPacket{Candidate: finding.PotentialFinding{Finding: item, Observation: item.Observation, Hypothesis: item.Hypothesis, ReviewPolicy: item.ReviewPolicy}, Evidence: []reviewcontext.EvidenceItem{{ID: "source.primary", Kind: reviewcontext.EvidencePrimary, Content: "source"}}, Sufficiency: reviewcontext.SufficiencySufficient, Metadata: reviewcontext.PacketMetadata{EstimatedInputTokens: 20, PacketBytes: 80, EvidenceCount: 1}}
}

func TestSyntheticScaleSmartTriageReducesRequestsAndCaches(t *testing.T) {
	items := make([]finding.Finding, 1756)
	for i := range items {
		items[i] = finding.Finding{ID: fmt.Sprintf("f-%04d", i), RuleID: "DIRECT", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .96, EvidenceStrength: "strong", Evidence: finding.Evidence{CodeSnippet: "small", Steps: []finding.EvidenceStep{{Kind: "source"}, {Kind: "propagation"}, {Kind: "sink"}}}}
		if i < 284 {
			items[i].RuleID = "CONTEXT"
			items[i].Confidence = .72
			items[i].EvidenceStrength = "moderate"
		}
	}
	provider := &boundedProvider{}
	cache := NewAssessmentCache(t.TempDir())
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeSmart, TokenBudget: 20000, OutputTokenBudget: 8192, Concurrency: 3, Model: "fixture", Cache: cache}, nil)
	_, first := reviewer.ReviewFindings(context.Background(), items, nil)
	_, second := reviewer.ReviewFindings(context.Background(), items, nil)
	oldRequests := (len(items) + 4) / 5
	t.Logf("static=%d smart=%d cached=%d provider=%d batches=%d old_fixed_five=%d", len(items), first.Selected, second.CacheHits, first.ProviderReviews, first.Batches, oldRequests)
	if first.Selected != 10 || first.ProviderReviews != 10 || first.Batches >= oldRequests {
		t.Fatalf("first=%#v old requests=%d", first, oldRequests)
	}
	// The scan-global SMART rule-family cap prevents one repetitive rule from monopolizing review.
	if first.Batches != 2 {
		t.Fatalf("expected 2 bounded batches for the diverse sample, got %d", first.Batches)
	}
	if second.CacheHits != 10 || second.ProviderReviews != 0 || second.Batches != 0 {
		t.Fatalf("second=%#v", second)
	}
}

func TestSyntheticScaleSmartTriageOutputAwareBatchCount(t *testing.T) {
	items := make([]finding.Finding, 284)
	for i := range items {
		items[i] = finding.Finding{ID: fmt.Sprintf("f-%04d", i), RuleID: "CTX", Confidence: .72, EvidenceStrength: "moderate"}
	}
	provider := &boundedProvider{}

	// Test with 8192 output budget (Anthropic / Gemini)
	rev8k := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 20000, OutputTokenBudget: 8192, Concurrency: 1}, nil)
	_, stats8k := rev8k.ReviewFindings(context.Background(), items, nil)
	if stats8k.Batches != 6 {
		t.Fatalf("global request budget: got %d batches, want 6", stats8k.Batches)
	}

	// Test with 4096 output budget (OpenAI)
	rev4k := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 20000, OutputTokenBudget: 4096, Concurrency: 1}, nil)
	_, stats4k := rev4k.ReviewFindings(context.Background(), items, nil)
	if stats4k.Batches != 6 {
		t.Fatalf("global request budget: got %d batches, want 6", stats4k.Batches)
	}
}

func BenchmarkSmartTriage1756(b *testing.B) {
	items := make([]finding.Finding, 1756)
	for i := range items {
		items[i] = finding.Finding{ID: fmt.Sprintf("b-%d", i), RuleID: "DIRECT", Confidence: .96, EvidenceStrength: "strong", Evidence: finding.Evidence{Steps: []finding.EvidenceStep{{Kind: "source"}, {Kind: "sink"}}}}
		if i < 284 {
			items[i].Confidence = .7
			items[i].EvidenceStrength = "moderate"
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewTriageReviewer(&boundedProvider{}, TriageOptions{Mode: ReviewModeSmart, TokenBudget: 20000, OutputTokenBudget: 8192, Concurrency: 3}, nil).ReviewFindings(context.Background(), items, nil)
	}
}

func (*boundedProvider) Name() string { return "fake" }
func (p *boundedProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := p.ReviewBatch(ctx, []ReviewRequest{request})
	if err != nil {
		return ReviewResponse{}, err
	}
	return responses[0], nil
}
func (p *boundedProvider) ReviewBatch(ctx context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	p.mu.Lock()
	p.active++
	p.calls++
	attempts := p.attemptsPerCall
	if attempts <= 0 {
		attempts = 1
	}
	p.httpAttempts += attempts
	if p.active > p.maximum {
		p.maximum = p.active
	}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.active--; p.mu.Unlock() }()
	if p.failErr != nil {
		return nil, p.failErr
	}
	if p.failAll || (p.failLarge && len(requests) > 2) {
		return nil, errors.New("malformed structured response")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	result := make([]ReviewResponse, len(requests))
	for i, request := range requests {
		result[i] = supportedTestResponse(request, "real")
	}
	return result, nil
}

func TestTriageReviewerCircuitBreakerBoundsProviderOutage(t *testing.T) {
	provider := &boundedProvider{failAll: true}
	var last ReviewProgress
	_, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 3, FailureThreshold: 3}, nil).ReviewFindings(context.Background(), triageFindings(20), func(progress ReviewProgress) { last = progress })
	if provider.calls >= 20 || stats.Failures != 20 {
		t.Fatalf("calls=%d stats=%#v", provider.calls, stats)
	}
	if last.Done != last.Total || last.Total != 20 {
		t.Fatalf("final progress = %#v", last)
	}
}

func TestConsecutiveFailureCircuitPreservesFinalTypedCauseAndAttemptAudit(t *testing.T) {
	root := apperror.New(apperror.KindMalformedResponse, "missing finding ID")
	provider := &boundedProvider{failErr: root}
	findings := triageFindings(3)
	plan := BuildReviewPlan(ReviewModeAll, findings, ReviewBudget{MaxReviewedFindings: 3, MaxProviderRequests: 6, MaxEstimatedInputTokens: 60000, MaxOutputTokens: 16000}, nil)
	plan.Batches = [][]ReviewRequest{
		{{Finding: findings[0]}}, {{Finding: findings[1]}}, {{Finding: findings[2]}},
	}
	plan.EstimatedProviderRequests = 3
	var warnings []error
	_, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, Concurrency: 1, FailureThreshold: 3, Plan: &plan}, func(err error) {
		warnings = append(warnings, err)
	}).ReviewFindings(context.Background(), findings, nil)
	if len(warnings) == 0 || !apperror.IsKind(warnings[len(warnings)-1], apperror.KindMalformedResponse) {
		t.Fatalf("final warning lost typed cause: %v", warnings)
	}
	if !strings.Contains(warnings[len(warnings)-1].Error(), "consecutive failure threshold reached") || !strings.Contains(warnings[len(warnings)-1].Error(), "missing finding ID") {
		t.Fatalf("final warning = %q", warnings[len(warnings)-1])
	}
	if len(stats.ProviderAttempts) != 3 {
		t.Fatalf("attempts = %#v", stats.ProviderAttempts)
	}
	last := stats.ProviderAttempts[2]
	if last.RequestIDs[0] != findings[2].ID || last.FailureKind != string(apperror.KindMalformedResponse) || last.ValidationStage != "response_schema" || !last.CountedTowardConsecutiveFailure {
		t.Fatalf("last attempt = %#v", last)
	}
}

func TestTriageReviewerPersistent429TripsCircuitImmediatelyAndZerosQueuedCalls(t *testing.T) {
	provider := &boundedProvider{failErr: apperror.New(apperror.KindRateLimit, "rate limit exceeded"), attemptsPerCall: 3}
	var warnings []error
	warnFn := func(err error) { warnings = append(warnings, err) }

	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 1}, warnFn)
	findings := triageFindings(20)
	got, stats := reviewer.ReviewFindings(context.Background(), findings, nil)

	// Concurrency=1: exactly 1 logical ReviewBatch call made.
	if provider.calls != 1 {
		t.Fatalf("logical provider calls = %d, want 1", provider.calls)
	}
	// 3 HTTP attempts occurred within that single logical batch call (initial + 2 retries).
	if provider.httpAttempts != 3 {
		t.Fatalf("http attempts = %d, want 3", provider.httpAttempts)
	}
	if stats.Failures != 20 || stats.ProviderReviews != 0 {
		t.Fatalf("failures = %d, reviews = %d", stats.Failures, stats.ProviderReviews)
	}
	if stats.StopReason != "rate_limit" {
		t.Fatalf("StopReason = %q, want rate_limit", stats.StopReason)
	}
	for i, item := range got {
		if item.AIReviewed {
			t.Fatalf("finding %d was reviewed after circuit opened: %#v", i, item)
		}
		if item.Severity != findings[i].Severity || item.Confidence != findings[i].Confidence {
			t.Fatalf("static fields modified: %#v", item)
		}
	}
	if len(warnings) == 0 {
		t.Fatal("expected user-visible warning on circuit break")
	}
}

func TestTriageReviewerPersistent503TripsCircuitImmediatelyAndZerosQueuedCalls(t *testing.T) {
	provider := &boundedProvider{failErr: apperror.New(apperror.KindUnavailable, "503 service unavailable"), attemptsPerCall: 2}
	var warnings []error
	warnFn := func(err error) { warnings = append(warnings, err) }

	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 1}, warnFn)
	findings := triageFindings(20)
	got, stats := reviewer.ReviewFindings(context.Background(), findings, nil)

	if provider.calls != 1 {
		t.Fatalf("logical provider calls = %d, want 1", provider.calls)
	}
	if provider.httpAttempts != 2 {
		t.Fatalf("http attempts = %d, want 2", provider.httpAttempts)
	}
	if stats.Failures != 20 || stats.ProviderReviews != 0 {
		t.Fatalf("failures = %d, reviews = %d", stats.Failures, stats.ProviderReviews)
	}
	if stats.StopReason != "provider_unavailable" {
		t.Fatalf("StopReason = %q, want provider_unavailable", stats.StopReason)
	}
	for _, item := range got {
		if item.AIReviewed {
			t.Fatalf("finding marked as AIReviewed: %#v", item)
		}
	}
}

func TestTriageReviewerAuthFailureTripsCircuitImmediatelyAndZerosQueuedCalls(t *testing.T) {
	provider := &boundedProvider{failErr: apperror.New(apperror.KindAuthentication, "401 unauthorized"), attemptsPerCall: 1}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 1}, nil)
	findings := triageFindings(20)
	got, stats := reviewer.ReviewFindings(context.Background(), findings, nil)

	if provider.calls != 1 {
		t.Fatalf("logical provider calls = %d, want 1", provider.calls)
	}
	if provider.httpAttempts != 1 {
		t.Fatalf("http attempts = %d, want 1 (auth is never retried)", provider.httpAttempts)
	}
	if stats.StopReason != "authentication" {
		t.Fatalf("StopReason = %q, want authentication", stats.StopReason)
	}
	if len(got) != 20 {
		t.Fatalf("finding count changed: %d", len(got))
	}
}

func TestTriageReviewerConcurrentWorkersCircuitBreakRaceSafety(t *testing.T) {
	provider := &boundedProvider{failErr: apperror.New(apperror.KindRateLimit, "rate limit exceeded"), attemptsPerCall: 3}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 3}, nil)
	findings := triageFindings(50)
	got, stats := reviewer.ReviewFindings(context.Background(), findings, nil)

	// Concurrency=3: at most 3 in-flight batches start.
	if provider.calls > 3 {
		t.Fatalf("logical provider calls = %d, want <= 3", provider.calls)
	}
	// At most 3 in-flight batches * 3 HTTP attempts = 9 HTTP attempts max.
	if provider.httpAttempts > 9 {
		t.Fatalf("http attempts = %d, want <= 9", provider.httpAttempts)
	}
	if stats.Failures != stats.Selected || stats.ProviderReviews != 0 {
		t.Fatalf("failures = %d, reviews = %d", stats.Failures, stats.ProviderReviews)
	}
	if len(got) != 50 {
		t.Fatalf("lost findings: len=%d", len(got))
	}
}

func TestSyntheticScale1756Persistent429DoesNotScaleProviderCalls(t *testing.T) {
	items := make([]finding.Finding, 1756)
	for i := range items {
		items[i] = finding.Finding{ID: fmt.Sprintf("f-%04d", i), RuleID: "CTX", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: .7, EvidenceStrength: "moderate"}
	}
	provider := &boundedProvider{failErr: apperror.New(apperror.KindRateLimit, "rate limit"), attemptsPerCall: 3}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeSmart, TokenBudget: 20000, OutputTokenBudget: 8192, Concurrency: 3}, nil)
	got, stats := reviewer.ReviewFindings(context.Background(), items, nil)

	// 1756 findings (284 smart-eligible -> 14 batches).
	// Logical ReviewBatch calls must be bounded by concurrency (<= 3), NOT 14 batches!
	if provider.calls > 3 {
		t.Fatalf("logical provider calls scaled with items: %d calls for 1756 findings (want <= 3)", provider.calls)
	}
	// Total HTTP attempts bounded by concurrency * 3 = 9 attempts max.
	if provider.httpAttempts > 9 {
		t.Fatalf("total http attempts = %d, want <= 9", provider.httpAttempts)
	}
	if len(got) != 1756 {
		t.Fatalf("returned findings count = %d, want 1756", len(got))
	}
	if stats.StopReason != "rate_limit" {
		t.Fatalf("StopReason = %q", stats.StopReason)
	}
}

func TestSyntheticScale1756Persistent503DoesNotScaleProviderCalls(t *testing.T) {
	items := make([]finding.Finding, 1756)
	for i := range items {
		items[i] = finding.Finding{ID: fmt.Sprintf("f-%04d", i), RuleID: "CTX", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: .7, EvidenceStrength: "moderate"}
	}
	provider := &boundedProvider{failErr: apperror.New(apperror.KindUnavailable, "service unavailable"), attemptsPerCall: 2}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeSmart, TokenBudget: 20000, OutputTokenBudget: 8192, Concurrency: 3}, nil)
	got, stats := reviewer.ReviewFindings(context.Background(), items, nil)

	// Logical ReviewBatch calls <= 3.
	if provider.calls > 3 {
		t.Fatalf("logical provider calls = %d, want <= 3", provider.calls)
	}
	// Total HTTP attempts <= 3 workers * 2 attempts = 6 attempts max.
	if provider.httpAttempts > 6 {
		t.Fatalf("total http attempts = %d, want <= 6", provider.httpAttempts)
	}
	if len(got) != 1756 {
		t.Fatalf("returned findings count = %d, want 1756", len(got))
	}
	if stats.StopReason != "provider_unavailable" {
		t.Fatalf("StopReason = %q", stats.StopReason)
	}
}

func TestTriageReviewerProgressCorrectnessOnCircuitTrip(t *testing.T) {
	provider := &boundedProvider{failErr: apperror.New(apperror.KindRateLimit, "rate limit")}
	var lastProgress ReviewProgress
	findings := triageFindings(30)
	NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 2}, nil).ReviewFindings(
		context.Background(),
		findings,
		func(p ReviewProgress) { lastProgress = p },
	)

	if lastProgress.Done != 30 || lastProgress.Total != 30 {
		t.Fatalf("final progress = %#v, want Done=30 Total=30", lastProgress)
	}
}

func TestTriageReviewerHandlesDuplicateFindingIDsByStableIndex(t *testing.T) {
	provider := &boundedProvider{}
	items := triageFindings(2)
	items[1].ID = items[0].ID
	got, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 1}, nil).ReviewFindings(context.Background(), items, nil)
	if stats.ProviderReviews != 2 || !got[0].AIReviewed || !got[1].AIReviewed {
		t.Fatalf("stats=%#v findings=%#v", stats, got)
	}
}

func triageFindings(count int) []finding.Finding {
	result := make([]finding.Finding, count)
	for i := range result {
		result[i] = finding.Finding{ID: string(rune('a' + i)), RuleID: "CTX", Category: finding.CategoryMaintainability, Severity: finding.SeverityMedium, Confidence: .7, EvidenceStrength: "moderate", Evidence: finding.Evidence{CodeSnippet: "small"}}
	}
	return result
}

func TestTriageReviewerBoundsConcurrencyAndPreservesOrder(t *testing.T) {
	provider := &boundedProvider{}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 2, Model: "m"}, nil)
	input := triageFindings(6)
	got, stats := reviewer.ReviewFindings(context.Background(), input, nil)
	if provider.maximum > 2 || provider.maximum < 2 {
		t.Fatalf("maximum concurrency = %d", provider.maximum)
	}
	for i := range input {
		if got[i].ID != input[i].ID || !got[i].AIReviewed {
			t.Fatalf("order/result at %d = %#v", i, got[i])
		}
	}
	if stats.Selected != 6 || stats.ProviderReviews != 6 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestTriageReviewerUsesCacheOnSecondRun(t *testing.T) {
	provider := &boundedProvider{}
	cache := NewAssessmentCache(t.TempDir())
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 1000, Concurrency: 1, Model: "m", Cache: cache}, nil)
	input := triageFindings(2)
	_, first := reviewer.ReviewFindings(context.Background(), input, nil)
	_, second := reviewer.ReviewFindings(context.Background(), input, nil)
	if first.ProviderReviews != 2 || second.CacheHits != 2 || second.ProviderReviews != 0 || provider.calls != 1 {
		t.Fatalf("first=%#v second=%#v calls=%d", first, second, provider.calls)
	}
}

type malformedMixedProvider struct{ calls int }

func (provider *malformedMixedProvider) Name() string { return "malformed-mixed" }
func (provider *malformedMixedProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := provider.ReviewBatch(ctx, []ReviewRequest{request})
	if len(responses) == 0 {
		return ReviewResponse{}, err
	}
	return responses[0], err
}
func (provider *malformedMixedProvider) ReviewBatch(_ context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	provider.calls++
	responses := make([]ReviewResponse, len(requests))
	for index, request := range requests {
		responses[index] = ReviewResponse{ID: request.Finding.ID, EvidenceRelation: EvidenceInsufficient, Reason: "not supported"}
	}
	for index, request := range requests {
		if request.Finding.ID == "b" {
			responses[index] = ReviewResponse{ID: request.Finding.ID, EvidenceRelation: EvidenceSupports, ClaimStrength: ClaimStrengthUnchanged, Severity: finding.SeverityMedium, Confidence: .7, Malformed: true, MalformedReason: "required reason is missing or blank"}
		}
	}
	return responses, nil
}

func TestTriageMalformedDecisionBecomesUncertainAndRemainingWorkContinues(t *testing.T) {
	provider := &malformedMixedProvider{}
	cache := NewAssessmentCache(t.TempDir())
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 10000, Concurrency: 1, Model: "m", Cache: cache}, nil)
	input := triageFindings(3)
	got, first := reviewer.ReviewFindings(context.Background(), input, nil)
	if first.ProviderReviews != 3 || first.Failures != 0 {
		t.Fatalf("stats = %#v", first)
	}
	for _, attempt := range first.ProviderAttempts {
		if attempt.FailureKind != "" || attempt.CountedTowardConsecutiveFailure {
			t.Fatalf("malformed per-finding adjudication counted as provider failure: %#v", attempt)
		}
	}
	if !got[0].AIReviewed || !got[1].AIReviewed || !got[2].AIReviewed || got[1].Adjudication.Status != finding.AdjudicationUncertain {
		t.Fatalf("findings = %#v", got)
	}
	if got[1].AIReview == nil || !got[1].AIReview.Malformed || got[1].AIReview.Reason == "" {
		t.Fatalf("malformed assessment = %#v", got[1].AIReview)
	}

	_, second := reviewer.ReviewFindings(context.Background(), input, nil)
	if second.CacheHits != 2 || second.ProviderReviews != 1 || provider.calls != 2 {
		t.Fatalf("malformed assessment was cached decisively: stats=%#v calls=%d", second, provider.calls)
	}
}

func TestTriageReviewerSplitsFailedLargeBatchOnce(t *testing.T) {
	provider := &boundedProvider{failLarge: true}
	reviewer := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 10000, Concurrency: 1}, nil)
	got, stats := reviewer.ReviewFindings(context.Background(), triageFindings(4), nil)
	if stats.Failures != 0 || stats.ProviderReviews != 4 || provider.calls != 3 {
		t.Fatalf("stats=%#v calls=%d", stats, provider.calls)
	}
	for _, item := range got {
		if !item.AIReviewed {
			t.Fatalf("partial recovery lost finding: %#v", got)
		}
	}
}

func TestTriageReviewerPropagatesCancellationWithoutDroppingFindings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &boundedProvider{}
	got, _ := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, Concurrency: 2}, nil).ReviewFindings(ctx, triageFindings(3), nil)
	if len(got) != 3 {
		t.Fatalf("findings = %#v", got)
	}
	for _, item := range got {
		if item.AIReviewed {
			t.Fatalf("canceled finding reviewed: %#v", item)
		}
	}
}

type cancellationBatchProvider struct {
	started chan struct{}
	calls   atomic.Int32
}

func (*cancellationBatchProvider) Name() string { return "cancellation" }
func (provider *cancellationBatchProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := provider.ReviewBatch(ctx, []ReviewRequest{request})
	if len(responses) == 0 {
		return ReviewResponse{}, err
	}
	return responses[0], err
}
func (provider *cancellationBatchProvider) ReviewBatch(ctx context.Context, _ []ReviewRequest) ([]ReviewResponse, error) {
	if provider.calls.Add(1) == 1 {
		close(provider.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestTriageCancellationStopsInFlightAndPendingBatchesPromptly(t *testing.T) {
	provider := &cancellationBatchProvider{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, OutputTokenBudget: 512, Concurrency: 1}, nil).ReviewFindings(ctx, triageFindings(20), nil)
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("review orchestration did not return promptly after cancellation")
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls = %d, pending batches started after cancellation", calls)
	}
}

func TestTriageCanceledContextNeverStartsPendingBatch(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		provider := &boundedProvider{}
		NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 150, OutputTokenBudget: 512, Concurrency: 1}, nil).ReviewFindings(ctx, triageFindings(20), nil)
		if provider.calls != 0 {
			t.Fatalf("iteration %d started %d provider calls after cancellation", iteration, provider.calls)
		}
	}
}

// contextInsufficientProvider returns KindContextInsufficient for every request,
// simulating an Ollama provider that rejects oversized requests before any HTTP call.
type contextInsufficientProvider struct{ calls int }

func (*contextInsufficientProvider) Name() string { return "ollama" }
func (p *contextInsufficientProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := p.ReviewBatch(ctx, []ReviewRequest{request})
	if len(responses) == 0 {
		return ReviewResponse{}, err
	}
	return responses[0], err
}
func (p *contextInsufficientProvider) ReviewBatch(_ context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	p.calls++
	return nil, apperror.New(apperror.KindContextInsufficient,
		"Ollama request exceeds executable context: 13514 > 4096")
}

// mixedProvider succeeds for small IDs and returns KindContextInsufficient for large ones.
type mixedSizingProvider struct {
	normalCalls      int
	insufficientSeen int
}

func (*mixedSizingProvider) Name() string { return "ollama" }
func (p *mixedSizingProvider) Review(ctx context.Context, request ReviewRequest) (ReviewResponse, error) {
	responses, err := p.ReviewBatch(ctx, []ReviewRequest{request})
	if len(responses) == 0 {
		return ReviewResponse{}, err
	}
	return responses[0], err
}
func (p *mixedSizingProvider) ReviewBatch(_ context.Context, requests []ReviewRequest) ([]ReviewResponse, error) {
	for _, r := range requests {
		if strings.HasPrefix(r.Finding.ID, "large-") {
			p.insufficientSeen++
			return nil, apperror.New(apperror.KindContextInsufficient,
				"Ollama request exceeds executable context: 13514 > 4096")
		}
	}
	p.normalCalls++
	result := make([]ReviewResponse, len(requests))
	for i, r := range requests {
		result[i] = supportedTestResponse(r, "real")
	}
	return result, nil
}

// TestContextInsufficientSingletonBecomesUncertainWithoutHTTPCall verifies Fix 2:
// an Ollama preflight rejection (KindContextInsufficient, no HTTP) results in
// AdjudicationUncertain for the affected finding, does not increment consecutive
// failures, and does not open the circuit breaker.
func TestContextInsufficientSingletonBecomesUncertainWithoutHTTPCall(t *testing.T) {
	provider := &contextInsufficientProvider{}
	items := triageFindings(1)
	got, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 10000, Concurrency: 1, FailureThreshold: 3}, nil).ReviewFindings(context.Background(), items, nil)

	// Provider was called (it performs the fit check) but no HTTP was sent.
	if provider.calls != 1 {
		t.Fatalf("provider.calls = %d, want 1", provider.calls)
	}
	// No provider attempt should be recorded (no HTTP call was made).
	if len(stats.ProviderAttempts) != 0 {
		t.Fatalf("ProviderAttempts = %v, want empty (no HTTP call)", stats.ProviderAttempts)
	}
	// The circuit must remain closed — no consecutive failure was counted.
	if stats.StopReason != "" {
		t.Fatalf("StopReason = %q, want empty (circuit must not open)", stats.StopReason)
	}
	// The finding must become UNCERTAIN, not a provider failure.
	if stats.Failures != 0 {
		t.Fatalf("Failures = %d, want 0 (not a provider failure)", stats.Failures)
	}
	if stats.ContextInsufficient != 1 {
		t.Fatalf("ContextInsufficient = %d, want 1", stats.ContextInsufficient)
	}
	if got[0].Adjudication == nil || got[0].Adjudication.Status != finding.AdjudicationUncertain {
		t.Fatalf("finding adjudication = %v, want AdjudicationUncertain", got[0].Adjudication)
	}
}

// TestMultipleContextInsufficientFindingsDoNotOpenCircuitBreaker verifies that
// N > FailureThreshold context-insufficient rejections never open the circuit.
func TestMultipleContextInsufficientFindingsDoNotOpenCircuitBreaker(t *testing.T) {
	provider := &contextInsufficientProvider{}
	items := triageFindings(10) // 10 > default FailureThreshold of 3
	plan := BuildReviewPlan(ReviewModeAll, items, ReviewBudget{MaxReviewedFindings: 10, MaxProviderRequests: 10, MaxEstimatedInputTokens: 60000, MaxOutputTokens: 16000}, nil)
	plan.Batches = make([][]ReviewRequest, len(items))
	for i := range items {
		plan.Batches[i] = []ReviewRequest{{Finding: items[i]}}
	}
	plan.EstimatedProviderRequests = len(items)

	got, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 60000, OutputTokenBudget: 16000, Concurrency: 1, FailureThreshold: 3, Budget: plan.Budget, Plan: &plan}, nil).ReviewFindings(context.Background(), items, nil)

	if provider.calls != 10 {
		t.Fatalf("provider.calls = %d, want 10 (all items attempted)", provider.calls)
	}
	if stats.StopReason != "" {
		t.Fatalf("circuit opened with StopReason=%q — must not open for context-insufficient rejections", stats.StopReason)
	}
	if stats.ContextInsufficient != 10 {
		t.Fatalf("ContextInsufficient = %d, want 10", stats.ContextInsufficient)
	}
	for i, f := range got {
		if f.Adjudication == nil || f.Adjudication.Status != finding.AdjudicationUncertain {
			t.Errorf("finding[%d] adjudication = %v, want AdjudicationUncertain", i, f.Adjudication)
		}
	}
}

// TestProviderFailureStillOpenCircuitUnderExistingThreshold verifies that real
// provider transport/schema failures still open the circuit at FailureThreshold,
// ensuring Fix 2 does not break existing circuit-breaker behavior.
func TestProviderFailureStillOpenCircuitUnderExistingThreshold(t *testing.T) {
	root := apperror.New(apperror.KindMalformedResponse, "malformed response")
	provider := &boundedProvider{failErr: root}
	items := triageFindings(10)
	plan := BuildReviewPlan(ReviewModeAll, items, ReviewBudget{MaxReviewedFindings: 10, MaxProviderRequests: 10, MaxEstimatedInputTokens: 60000, MaxOutputTokens: 16000}, nil)
	plan.Batches = make([][]ReviewRequest, len(items))
	for i := range items {
		plan.Batches[i] = []ReviewRequest{{Finding: items[i]}}
	}
	plan.EstimatedProviderRequests = len(items)

	_, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 10000, Concurrency: 1, FailureThreshold: 3, Plan: &plan}, nil).ReviewFindings(context.Background(), items, nil)

	// Provider must have been called only ~FailureThreshold times (circuit opens after 3).
	if provider.calls >= 10 {
		t.Fatalf("provider.calls = %d: circuit did not open", provider.calls)
	}
	if stats.StopReason == "" {
		t.Fatalf("StopReason empty — circuit should have opened after %d consecutive failures", 3)
	}
}

// TestContextInsufficientDoesNotStopRemainingValidBatches verifies that when
// some findings are context-insufficient and others are executable, the executable
// findings are still reviewed and context-insufficient ones become UNCERTAIN.
func TestContextInsufficientDoesNotStopRemainingValidBatches(t *testing.T) {
	provider := &mixedSizingProvider{}
	// Interleave large (context-insufficient) and small (normal) findings.
	items := []finding.Finding{
		{ID: "normal-0", RuleID: "R", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .8},
		{ID: "large-0", RuleID: "R", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .8},
		{ID: "normal-1", RuleID: "R", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .8},
		{ID: "large-1", RuleID: "R", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .8},
		{ID: "normal-2", RuleID: "R", Category: finding.CategorySecurity, Severity: finding.SeverityHigh, Confidence: .8},
	}
	plan := BuildReviewPlan(ReviewModeAll, items, ReviewBudget{MaxReviewedFindings: len(items), MaxProviderRequests: len(items), MaxEstimatedInputTokens: 60000, MaxOutputTokens: 16000}, nil)
	plan.Batches = make([][]ReviewRequest, len(items))
	for i := range items {
		plan.Batches[i] = []ReviewRequest{{Finding: items[i]}}
	}
	plan.EstimatedProviderRequests = len(items)

	got, stats := NewTriageReviewer(provider, TriageOptions{Mode: ReviewModeAll, TokenBudget: 10000, Concurrency: 1, FailureThreshold: 3, Plan: &plan}, nil).ReviewFindings(context.Background(), items, nil)

	// Circuit must remain open — no consecutive failures from context-insufficient.
	if stats.StopReason != "" {
		t.Fatalf("StopReason=%q — circuit opened unexpectedly", stats.StopReason)
	}
	// Normal findings must be reviewed.
	if stats.ProviderReviews != 3 {
		t.Fatalf("ProviderReviews = %d, want 3 (the three normal findings)", stats.ProviderReviews)
	}
	// Context-insufficient findings must be UNCERTAIN.
	if stats.ContextInsufficient != 2 {
		t.Fatalf("ContextInsufficient = %d, want 2", stats.ContextInsufficient)
	}
	// Verify per-finding disposition.
	byID := map[string]finding.Finding{}
	for _, f := range got {
		byID[f.ID] = f
	}
	for _, id := range []string{"normal-0", "normal-1", "normal-2"} {
		if !byID[id].AIReviewed {
			t.Errorf("normal finding %q not reviewed", id)
		}
	}
	for _, id := range []string{"large-0", "large-1"} {
		f := byID[id]
		if f.Adjudication == nil || f.Adjudication.Status != finding.AdjudicationUncertain {
			t.Errorf("large finding %q adjudication = %v, want AdjudicationUncertain", id, f.Adjudication)
		}
	}
}
