package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/reviewcontext"
)

const defaultInputTokenBudget = 20000

type TriageOptions struct {
	Mode              ReviewMode
	TokenBudget       int
	OutputTokenBudget int
	Concurrency       int
	Model             string
	Cache             *AssessmentCache
	FailureThreshold  int
	BuildContext      func(finding.Finding) reviewcontext.ReviewPacket
	Budget            ReviewBudget
	Plan              *ReviewPlan
}

type ReviewStats struct {
	Available           bool          `json:"available"`
	FindingsTotal       int           `json:"findings_total"`
	Selected            int           `json:"selected"`
	CacheHits           int           `json:"cache_hits"`
	ProviderReviews     int           `json:"provider_reviews"`
	Batches             int           `json:"batches"`
	Failures            int           `json:"failures"`
	Duration            time.Duration `json:"duration"`
	LikelyReal          int           `json:"likely_real"`
	Disputed            int           `json:"disputed"`
	Uncertain           int           `json:"uncertain"`
	Confirmed           int           `json:"confirmed"`
	Downgraded          int           `json:"downgraded"`
	Unsupported         int           `json:"unsupported"`
	PacketsBuilt        int           `json:"packets_built"`
	PacketInputTokens   int           `json:"packet_input_tokens"`
	PacketBytes         int           `json:"packet_bytes"`
	TruncatedPackets    int           `json:"truncated_packets"`
	ContextInsufficient int           `json:"context_insufficient"`
	// StopReason is set when the circuit breaker opens, e.g. "rate_limit", "authentication", "provider_unavailable".
	StopReason         string             `json:"stop_reason,omitempty"`
	ProviderFailureIDs []string           `json:"provider_failure_ids,omitempty"`
	BudgetSkippedIDs   []string           `json:"budget_skipped_ids,omitempty"`
	RuntimeSkipped     []SkippedCandidate `json:"runtime_skipped,omitempty"`
	ActualInputTokens  int                `json:"actual_estimated_input_tokens"`
	ActualOutputTokens int                `json:"actual_planned_output_tokens"`
	ProviderAttempts   []ProviderAttempt  `json:"provider_attempts,omitempty"`
	Plan               *ReviewPlan        `json:"-"`
}

type ReviewProgress struct{ Done, Total, Cached, Selected int }

type TriageReviewer struct {
	provider Provider
	options  TriageOptions
	warn     func(error)
	ledger   *budgetLedger
}

func NewTriageReviewer(provider Provider, options TriageOptions, warn func(error)) *TriageReviewer {
	if options.Mode == "" {
		options.Mode = ReviewModeSmart
	}
	if options.TokenBudget <= 0 {
		options.TokenBudget = defaultInputTokenBudget
	}
	if options.OutputTokenBudget <= 0 {
		if provider != nil {
			switch provider.Name() {
			case "anthropic", "gemini":
				options.OutputTokenBudget = 8192
			case "ollama":
				options.OutputTokenBudget = 2048
			default:
				options.OutputTokenBudget = 4096
			}
		} else {
			options.OutputTokenBudget = 4096
		}
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 3
	}
	if options.FailureThreshold <= 0 {
		options.FailureThreshold = 3
	}
	if provider != nil && provider.Name() == "ollama" && options.Concurrency > 1 {
		options.Concurrency = 1
	}
	if options.Budget == (ReviewBudget{}) {
		options.Budget = DefaultReviewBudget()
	}
	options.Budget = normalizeReviewBudget(options.Budget)
	return &TriageReviewer{provider: provider, options: options, warn: warn, ledger: newBudgetLedger(options.Budget)}
}

type indexedRequest struct {
	index   int
	request ReviewRequest
}
type batchJob struct{ items []indexedRequest }
type batchResult struct {
	assessments         map[int]finding.AIAssessment
	requests            map[int]ReviewRequest
	failures            int
	calls               int
	circuitBreak        bool
	circuitErr          error
	failed              []int
	budgetExhausted     bool
	contextInsufficient map[int]bool // local preflight: no HTTP sent, not a provider failure
	attempts            []ProviderAttempt
}

func (reviewer *TriageReviewer) ReviewFindings(ctx context.Context, input []finding.Finding, progress func(ReviewProgress)) ([]finding.Finding, ReviewStats) {
	started := time.Now()
	output := append([]finding.Finding(nil), input...)
	stats := ReviewStats{Available: reviewer.provider != nil, FindingsTotal: len(input)}
	if reviewer.provider == nil || reviewer.options.Mode == ReviewModeOff {
		stats.Duration = time.Since(started)
		return output, stats
	}
	plan := reviewer.options.Plan
	if plan == nil {
		built, err := BuildReviewPlanForProviderContext(ctx, reviewer.provider.Name(), reviewer.options.Mode, output, reviewer.options.Budget, reviewer.options.BuildContext)
		if err != nil {
			stats.Duration = time.Since(started)
			stats.Plan = &built
			stats.StopReason = "canceled"
			return output, stats
		}
		plan = &built
	}
	stats.Plan = plan
	reviewer.options.Plan = plan
	stats.Selected = len(plan.Selected)
	stats.PacketsBuilt = plan.ContextPacketsBuilt
	stats.TruncatedPackets = plan.TruncatedPackets
	stats.PacketInputTokens = plan.EstimatedInputTokens
	for _, skipped := range plan.Skipped {
		if skipped.Reason == SkipBudgetExhausted {
			stats.BudgetSkippedIDs = append(stats.BudgetSkippedIDs, skipped.CandidateID)
		}
		if skipped.Reason != SkipContextInsufficient {
			continue
		}
		for index := range output {
			if output[index].ID == skipped.CandidateID {
				decision := finding.ReviewDecision{Status: finding.AdjudicationUncertain, Source: finding.DecisionSourceStatic, Severity: output[index].Severity, Confidence: output[index].Confidence, Reason: "Semantic context is insufficient."}
				output[index].Adjudication = &decision
				stats.ContextInsufficient++
				stats.Uncertain++
			}
		}
	}
	meta := CacheMetadata{Provider: reviewer.provider.Name(), Model: reviewer.options.Model, PromptSchema: PromptSchemaVersion, RuleVersion: "finding-v2"}
	pending := make([]indexedRequest, 0)
	occurrences := map[string]int{}
	for _, selected := range plan.Selected {
		index := findingIndex(output, selected.CandidateID, occurrences[selected.CandidateID])
		if index < 0 {
			continue
		}
		occurrences[selected.CandidateID]++
		request := selected.Request
		stats.PacketBytes += request.Packet.Metadata.PacketBytes
		if assessment, ok := reviewer.options.Cache.Get(request, meta); ok {
			response := ReviewResponse{ID: output[index].ID, EvidenceRelation: EvidenceRelation(assessment.EvidenceRelation), ClaimStrength: ClaimStrength(assessment.ClaimStrength), Severity: assessment.Severity, Confidence: assessment.Confidence, Reason: assessment.Reason, Recommendation: assessment.Recommendation, EvidenceIDs: append([]string(nil), assessment.EvidenceIDs...)}
			decision, decisionErr := NormalizeDecision(output[index], response, assessment.Provider, assessment.Model)
			if request.Packet.Candidate.ID != "" {
				decision, decisionErr = NormalizeDecisionWithPacket(request.Packet, response, assessment.Provider, assessment.Model)
			}
			if decisionErr == nil {
				assessment = finding.AIAssessment{EvidenceRelation: assessment.EvidenceRelation, ClaimStrength: assessment.ClaimStrength, Verdict: decision.Status, IsRealIssue: decision.Status == finding.AdjudicationConfirmed || decision.Status == finding.AdjudicationDowngraded, Severity: decision.Severity, Confidence: decision.Confidence, Reason: decision.Reason, Recommendation: decision.Recommendation, Provider: decision.Provider, Model: decision.Model, EvidenceIDs: append([]string(nil), decision.EvidenceIDs...), Malformed: assessment.Malformed}
				output[index].AIReviewed = true
				output[index].AIReview = &assessment
				output[index].Adjudication = decisionFromAssessment(assessment)
				stats.CacheHits++
				countVerdict(&stats, assessment.Verdict)
				continue
			}
		}
		pending = append(pending, indexedRequest{index: index, request: request})
	}
	if progress != nil {
		progress(ReviewProgress{Total: len(pending), Cached: stats.CacheHits, Selected: stats.Selected})
	}
	if len(pending) == 0 {
		stats.Duration = time.Since(started)
		return output, stats
	}
	adaptive := plannedPendingBatches(plan.Batches, pending)
	jobs := make(chan batchJob)
	results := make(chan batchResult, len(adaptive))
	var workers sync.WaitGroup
	var circuitOpen atomic.Bool
	var consecutiveFailures atomic.Int32
	workerCount := min(reviewer.options.Concurrency, len(adaptive))
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				// Re-check circuit state immediately before calling provider (race-safe under concurrency).
				if circuitOpen.Load() {
					failed := make([]int, len(job.items))
					for i := range job.items {
						failed[i] = job.items[i].index
					}
					results <- batchResult{assessments: map[int]finding.AIAssessment{}, requests: map[int]ReviewRequest{}, failures: len(job.items), failed: failed}
					continue
				}
				result := reviewer.reviewWithSplit(ctx, job)
				if result.circuitBreak {
					if circuitOpen.CompareAndSwap(false, true) {
						reviewer.warning(circuitOpenMessage(result.circuitErr))
					}
				} else if result.failures > 0 && result.failures == len(job.items) {
					if len(result.attempts) > 0 {
						result.attempts[len(result.attempts)-1].CountedTowardConsecutiveFailure = true
					}
					if consecutiveFailures.Add(1) >= int32(reviewer.options.FailureThreshold) {
						if circuitOpen.CompareAndSwap(false, true) {
							result.circuitBreak = true
							result.circuitErr = fmt.Errorf("consecutive failure threshold reached: %w", result.circuitErr)
							reviewer.warning(circuitOpenMessage(result.circuitErr))
						}
					}
				} else if len(result.assessments) > 0 {
					consecutiveFailures.Store(0)
				}
				results <- result
			}
		}()
	}
	go func() {
		defer close(jobs)
		cursor := 0
		for _, batch := range adaptive {
			if ctx.Err() != nil {
				return
			}
			job := batchJob{items: make([]indexedRequest, len(batch))}
			for i, request := range batch {
				job.items[i] = indexedRequest{index: pending[cursor+i].index, request: request}
			}
			cursor += len(batch)
			select {
			case jobs <- job:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	done := 0
	for result := range results {
		stats.Batches += result.calls
		stats.Failures += result.failures
		stats.ProviderAttempts = append(stats.ProviderAttempts, result.attempts...)
		for _, index := range result.failed {
			if result.budgetExhausted {
				stats.BudgetSkippedIDs = append(stats.BudgetSkippedIDs, output[index].ID)
				stats.RuntimeSkipped = append(stats.RuntimeSkipped, SkippedCandidate{CandidateID: output[index].ID, Reason: SkipBudgetExhausted})
			} else if result.contextInsufficient != nil && result.contextInsufficient[index] {
				// Local preflight sizing rejection: apply the same UNCERTAIN disposition
				// as a plan-time CONTEXT_INSUFFICIENT without recording a provider failure.
				decision := finding.ReviewDecision{Status: finding.AdjudicationUncertain, Source: finding.DecisionSourceStatic, Severity: output[index].Severity, Confidence: output[index].Confidence, Reason: "Request exceeds executable provider context after safe minimization."}
				output[index].Adjudication = &decision
				stats.ContextInsufficient++
				stats.Uncertain++
				stats.RuntimeSkipped = append(stats.RuntimeSkipped, SkippedCandidate{CandidateID: output[index].ID, Reason: SkipContextInsufficient})
			} else {
				stats.ProviderFailureIDs = append(stats.ProviderFailureIDs, output[index].ID)
				stats.RuntimeSkipped = append(stats.RuntimeSkipped, SkippedCandidate{CandidateID: output[index].ID, Reason: SkipProviderFailure})
			}
		}
		if result.circuitBreak && stats.StopReason == "" {
			stats.StopReason = circuitStopReason(result.circuitErr)
		}
		completed := len(result.failed) + len(result.assessments)
		for index, assessment := range result.assessments {
			output[index].AIReviewed = true
			output[index].AIReview = &assessment
			output[index].Adjudication = decisionFromAssessment(assessment)
			stats.ProviderReviews++
			countVerdict(&stats, assessment.Verdict)
			reviewer.options.Cache.Put(result.requests[index], meta, assessment)
		}
		done += completed
		if progress != nil {
			progress(ReviewProgress{Done: done, Total: len(pending), Cached: stats.CacheHits, Selected: stats.Selected})
		}
	}
	stats.Duration = time.Since(started)
	stats.Batches, stats.ActualInputTokens, stats.ActualOutputTokens = reviewer.ledger.usage()
	return output, stats
}

func plannedPendingBatches(planned [][]ReviewRequest, pending []indexedRequest) [][]ReviewRequest {
	queues := make(map[string][]ReviewRequest)
	for _, item := range pending {
		queues[item.request.Finding.ID] = append(queues[item.request.Finding.ID], item.request)
	}
	result := make([][]ReviewRequest, 0, len(planned))
	for _, batch := range planned {
		filtered := make([]ReviewRequest, 0, len(batch))
		for _, request := range batch {
			queue := queues[request.Finding.ID]
			if len(queue) == 0 {
				continue
			}
			filtered = append(filtered, queue[0])
			queues[request.Finding.ID] = queue[1:]
		}
		if len(filtered) > 0 {
			result = append(result, filtered)
		}
	}
	return result
}

func (reviewer *TriageReviewer) reviewWithSplit(ctx context.Context, job batchJob) batchResult {
	result := batchResult{assessments: map[int]finding.AIAssessment{}, requests: map[int]ReviewRequest{}, contextInsufficient: map[int]bool{}, calls: 1}
	responses, attempt, err := reviewer.callBatch(ctx, job.items)
	if !errors.Is(err, errReviewBudgetExhausted) && !apperror.IsKind(err, apperror.KindContextInsufficient) {
		result.attempts = append(result.attempts, attempt)
	}
	result.circuitErr = err
	if err == nil {
		reviewer.mergeResponses(result.assessments, result.requests, job.items, responses)
		return result
	}
	if errors.Is(err, errReviewBudgetExhausted) {
		result.failures = len(job.items)
		result.budgetExhausted = true
		for _, item := range job.items {
			result.failed = append(result.failed, item.index)
		}
		return result
	}
	// Typed provider-wide fatal errors: do not split, open circuit immediately.
	if isCircuitBreakingError(err) {
		result.failures = len(job.items)
		for _, item := range job.items {
			result.failed = append(result.failed, item.index)
		}
		result.circuitBreak = true
		result.circuitErr = err
		return result
	}
	if len(job.items) <= 1 || ctx.Err() != nil {
		for _, item := range job.items {
			result.failed = append(result.failed, item.index)
		}
		if apperror.IsKind(err, apperror.KindContextInsufficient) {
			for _, item := range job.items {
				result.contextInsufficient[item.index] = true
			}
			return result
		}
		result.failures = len(job.items)
		reviewer.warning(fmt.Errorf("AI review: %w", err))
		return result
	}
	middle := len(job.items) / 2
	for _, half := range [][]indexedRequest{job.items[:middle], job.items[middle:]} {
		result.calls++
		responses, attempt, splitErr := reviewer.callBatch(ctx, half)
		if !errors.Is(splitErr, errReviewBudgetExhausted) && !apperror.IsKind(splitErr, apperror.KindContextInsufficient) {
			result.attempts = append(result.attempts, attempt)
		}
		result.circuitErr = splitErr
		if splitErr != nil {
			for _, item := range half {
				result.failed = append(result.failed, item.index)
			}
			if errors.Is(splitErr, errReviewBudgetExhausted) {
				result.budgetExhausted = true
			}
			if apperror.IsKind(splitErr, apperror.KindContextInsufficient) {
				for _, item := range half {
					result.contextInsufficient[item.index] = true
				}
				continue
			}
			result.failures += len(half)
			if isCircuitBreakingError(splitErr) {
				result.circuitBreak = true
				result.circuitErr = splitErr
				return result
			}
			reviewer.warning(fmt.Errorf("AI review: %w", splitErr))
			continue
		}
		reviewer.mergeResponses(result.assessments, result.requests, half, responses)
	}
	return result
}

func (reviewer *TriageReviewer) callBatch(ctx context.Context, items []indexedRequest) ([]ReviewResponse, ProviderAttempt, error) {
	requests := make([]ReviewRequest, len(items))
	for i := range items {
		requests[i] = items[i].request
	}
	inputTokens := EstimateBatchTokens(requests)
	outputTokens := EstimateBatchOutputTokens(len(requests))
	attempt := ProviderAttempt{EstimatedInputTokens: inputTokens, ReservedOutputTokens: outputTokens}
	for _, request := range requests {
		attempt.RequestIDs = append(attempt.RequestIDs, request.Finding.ID)
	}
	if reviewer.provider.Name() == "ollama" {
		attempt.ContextWindow = reviewer.options.Plan.ContextWindow
		if attempt.ContextWindow <= 0 {
			attempt.ContextWindow = OllamaBatchConstraints().ContextWindow
		}
		if contextProvider, ok := reviewer.provider.(ContextWindowProvider); ok {
			attempt.ContextWindow = contextProvider.ContextWindow()
		}
		strategy := reviewer.options.Plan.SizingStrategy
		contract, contractErr := BuildOllamaRequestWithSizing(ctx, requests, attempt.ContextWindow, strategy)
		if contractErr != nil {
			return nil, attempt, contractErr
		}
		inputTokens = contract.PromptEstimate
		attempt.EstimatedInputTokens = inputTokens
		attempt.ChatTemplateAllowance = contract.ChatTemplateAllowance
		attempt.PromptTokenSource = contract.CountSource
		attempt.TokenEstimatorVersion = contract.EstimatorVersion
		attempt.PredictedContextUse = contract.ContextUse()
		if !contract.Fits() {
			return nil, attempt, apperror.New(apperror.KindContextInsufficient, fmt.Sprintf("request exceeds executable provider context after sizing: %d > %d", contract.ContextUse(), contract.NumCtx))
		}
	}
	if !reviewer.ledger.reserve(inputTokens, outputTokens) {
		return nil, attempt, errReviewBudgetExhausted
	}
	first := true
	ctx = WithProviderAttemptBudget(ctx, func() bool {
		if first {
			first = false
			return true
		}
		return reviewer.ledger.reserve(inputTokens, outputTokens)
	})
	trace := &providerAttemptTrace{}
	started := time.Now()
	responses, err := (&Reviewer{provider: reviewer.provider}).reviewBatch(withProviderAttemptTrace(ctx, trace), requests)
	attempt.Duration = time.Since(started)
	attempt.ResponseReceived, attempt.HTTPStatus, attempt.ValidationStage, attempt.ActualPromptTokens = trace.snapshot()
	if attempt.ActualPromptTokens > 0 {
		attempt.PromptTokenError = attempt.ActualPromptTokens - attempt.EstimatedInputTokens
	}
	if err != nil {
		attempt.FailureKind = providerFailureKind(err)
		if attempt.ValidationStage == "" {
			if apperror.IsKind(err, apperror.KindMalformedResponse) {
				attempt.ValidationStage = "response_schema"
			} else {
				attempt.ValidationStage = "request"
			}
		}
	}
	return responses, attempt, err
}

func providerFailureKind(err error) string {
	for _, kind := range []apperror.Kind{apperror.KindAuthentication, apperror.KindRateLimit, apperror.KindUnavailable, apperror.KindMalformedResponse, apperror.KindInvalidRequest, apperror.KindNetworkTimeout} {
		if apperror.IsKind(err, kind) {
			return string(kind)
		}
	}
	if err != nil {
		return "provider_error"
	}
	return ""
}

var errReviewBudgetExhausted = ErrReviewBudgetExhausted

func findingIndex(items []finding.Finding, id string, occurrence int) int {
	for index := range items {
		if items[index].ID == id {
			if occurrence == 0 {
				return index
			}
			occurrence--
		}
	}
	return -1
}

func (reviewer *TriageReviewer) mergeResponses(target map[int]finding.AIAssessment, requests map[int]ReviewRequest, items []indexedRequest, responses []ReviewResponse) {
	for i, response := range responses {
		if response.Malformed {
			reviewer.warning(fmt.Errorf("finding %q: malformed provider adjudication: %s", items[i].request.Finding.ID, response.MalformedReason))
		}
		decision, err := NormalizeDecision(items[i].request.Finding, response, reviewer.provider.Name(), reviewer.options.Model)
		if items[i].request.Packet.Candidate.ID != "" {
			decision, err = NormalizeDecisionWithPacket(items[i].request.Packet, response, reviewer.provider.Name(), reviewer.options.Model)
		}
		if err != nil {
			reviewer.warning(err)
			decision = malformedDecision(items[i].request.Finding, reviewer.provider.Name(), reviewer.options.Model, err.Error())
			response.Malformed = true
			response.MalformedReason = err.Error()
		}
		semanticMalformed := response.Malformed || (decision.Status == finding.AdjudicationUncertain && response.EvidenceRelation != EvidenceInsufficient)
		assessment := finding.AIAssessment{EvidenceRelation: string(response.EvidenceRelation), ClaimStrength: string(response.ClaimStrength), Verdict: decision.Status, IsRealIssue: decision.Status == finding.AdjudicationConfirmed || decision.Status == finding.AdjudicationDowngraded, Severity: decision.Severity, Confidence: decision.Confidence, Reason: decision.Reason, Recommendation: decision.Recommendation, Provider: decision.Provider, Model: decision.Model, EvidenceIDs: append([]string(nil), decision.EvidenceIDs...), Malformed: semanticMalformed}
		target[items[i].index] = assessment
		requests[items[i].index] = items[i].request
	}
}

func decisionFromAssessment(assessment finding.AIAssessment) *finding.ReviewDecision {
	return &finding.ReviewDecision{Status: assessment.Verdict, Source: finding.DecisionSourceAI, Severity: assessment.Severity, Confidence: assessment.Confidence, Reason: assessment.Reason, Recommendation: assessment.Recommendation, Provider: assessment.Provider, Model: assessment.Model, EvidenceIDs: append([]string(nil), assessment.EvidenceIDs...)}
}

func (reviewer *TriageReviewer) warning(err error) {
	if reviewer.warn != nil {
		reviewer.warn(err)
	}
}

// isCircuitBreakingError returns true for errors that indicate a provider-wide
// outage and should prevent all queued batches from being sent.
func isCircuitBreakingError(err error) bool {
	for _, kind := range []apperror.Kind{
		apperror.KindAuthentication,
		apperror.KindRateLimit,
		apperror.KindUnavailable,
		apperror.KindNetworkTimeout,
	} {
		if apperror.IsKind(err, kind) {
			return true
		}
	}
	return false
}

// circuitStopReason returns the ReviewStats.StopReason string for the error kind.
func circuitStopReason(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case apperror.IsKind(err, apperror.KindAuthentication):
		return string(apperror.KindAuthentication)
	case apperror.IsKind(err, apperror.KindRateLimit):
		return string(apperror.KindRateLimit)
	case apperror.IsKind(err, apperror.KindUnavailable):
		return string(apperror.KindUnavailable)
	case apperror.IsKind(err, apperror.KindNetworkTimeout):
		return string(apperror.KindNetworkTimeout)
	default:
		return "provider_error"
	}
}

// circuitOpenMessage produces the user-visible warning shown when the circuit opens.
func circuitOpenMessage(err error) error {
	switch {
	case apperror.IsKind(err, apperror.KindAuthentication):
		return fmt.Errorf("AI Review unavailable · provider authentication failed\nStatic analysis results are unaffected.")
	case apperror.IsKind(err, apperror.KindRateLimit):
		return fmt.Errorf("AI Review stopped · provider rate limit reached\nStatic analysis results are unaffected.")
	case apperror.IsKind(err, apperror.KindUnavailable):
		return fmt.Errorf("AI Review stopped · provider temporarily unavailable\nStatic analysis results are unaffected.")
	case apperror.IsKind(err, apperror.KindNetworkTimeout):
		return fmt.Errorf("AI Review stopped · provider network timeout\nStatic analysis results are unaffected.")
	default:
		return fmt.Errorf("AI Review stopped · provider error: %w\nStatic analysis results are unaffected.", err)
	}
}

func countVerdict(stats *ReviewStats, verdict finding.AIVerdict) {
	switch verdict {
	case finding.AdjudicationConfirmed:
		stats.Confirmed++
		stats.LikelyReal++
	case finding.AdjudicationDowngraded:
		stats.Downgraded++
	case finding.AdjudicationUnsupported:
		stats.Unsupported++
		stats.Disputed++
	default:
		stats.Uncertain++
	}
}
