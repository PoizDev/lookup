package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/logger"
	"go.uber.org/zap"
)

const (
	maxResponseBytes   = 2 << 20
	defaultHTTPTimeout = 30 * time.Second
	ollamaHTTPTimeout  = 180 * time.Second
)

var boundedHTTPClient = &http.Client{Timeout: defaultHTTPTimeout}

type retryPolicy struct {
	maxAttempts int
	maxElapsed  time.Duration
	baseDelay   time.Duration
	maxDelay    time.Duration
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time
	jitter      func(time.Duration) time.Duration
}

func defaultRetryPolicy() retryPolicy {
	return retryPolicy{maxAttempts: 3, maxElapsed: 30 * time.Second, baseDelay: 500 * time.Millisecond, maxDelay: 8 * time.Second, sleep: sleepContext, now: time.Now, jitter: func(delay time.Duration) time.Duration {
		return time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
	}}
}

// unavailableRetryPolicy uses a tighter budget for transient 5xx errors so the
// local CLI does not block for long on gateway or service outages.
func unavailableRetryPolicy() retryPolicy {
	return retryPolicy{maxAttempts: 2, maxElapsed: 15 * time.Second, baseDelay: 1 * time.Second, maxDelay: 4 * time.Second, sleep: sleepContext, now: time.Now, jitter: func(delay time.Duration) time.Duration {
		return time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
	}}
}

func httpClient(client *http.Client) *http.Client {
	return httpClientWithDefaultTimeout(client, defaultHTTPTimeout)
}

func httpClientWithDefaultTimeout(client *http.Client, timeout time.Duration) *http.Client {
	if client == nil {
		if timeout == defaultHTTPTimeout {
			return boundedHTTPClient
		}
		return &http.Client{Timeout: timeout}
	}
	if client.Timeout <= 0 {
		bounded := *client
		bounded.Timeout = timeout
		return &bounded
	}
	return client
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, payload any, result any, secrets ...string) error {
	return postJSONWithPolicy(ctx, client, endpoint, headers, payload, result, defaultRetryPolicy(), unavailableRetryPolicy(), secrets...)
}

// postJSONWithPolicy executes the request with separate retry policies for
// rate-limit (429) and transient server errors (5xx). Auth failures are never
// retried. Other 4xx are returned as KindInvalidRequest immediately.
func postJSONWithPolicy(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, payload any, result any, rateLimitPolicy retryPolicy, unavailPolicy retryPolicy, secrets ...string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode provider request: %w", err)
	}

	do := func() (*http.Response, error) {
		if !ai.ReserveProviderAttempt(ctx) {
			return nil, ai.ErrReviewBudgetExhausted
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if reqErr != nil {
			return nil, fmt.Errorf("create provider request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		return httpClient(client).Do(req)
	}

	rlStarted := rateLimitPolicy.now()
	var rlWaited time.Duration
	rlAttempts := 0

	uaStarted := unavailPolicy.now()
	var uaWaited time.Duration
	uaAttempts := 0

	for {
		resp, doErr := do()
		if doErr != nil {
			safe := &sanitizedError{message: sanitize(doErr.Error(), secrets), cause: doErr}
			if isTimeout(doErr) {
				return apperror.Wrap(apperror.KindNetworkTimeout, "AI provider request timed out", safe)
			}
			return safe
		}

		status := resp.StatusCode
		ai.ObserveProviderResponse(ctx, status)

		// ── Success ───────────────────────────────────────────────────────
		if status >= http.StatusOK && status < http.StatusMultipleChoices {
			decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
			decErr := decoder.Decode(result)
			_ = resp.Body.Close()
			if decErr != nil {
				ai.ObserveProviderStage(ctx, "response_envelope")
				return apperror.Wrap(apperror.KindMalformedResponse, "decode provider response", decErr)
			}
			return nil
		}

		drainAndClose(resp.Body)

		// ── Auth — never retry ────────────────────────────────────────────
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return apperror.New(apperror.KindAuthentication, fmt.Sprintf("AI provider authentication failed (HTTP %d)", status))
		}

		// ── Rate limit (429) ──────────────────────────────────────────────
		if status == http.StatusTooManyRequests {
			rlAttempts++
			if rlAttempts >= rateLimitPolicy.maxAttempts {
				return apperror.New(apperror.KindRateLimit, "AI provider rate limit exceeded after bounded retries")
			}
			delay := retryDelay(resp.Header.Get("Retry-After"), rlAttempts, rateLimitPolicy)
			if delay <= 0 || rateLimitPolicy.now().Sub(rlStarted)+rlWaited+delay > rateLimitPolicy.maxElapsed {
				return apperror.New(apperror.KindRateLimit, "AI provider rate limit retry budget exhausted")
			}
			logger.Warn("provider rate limited; retrying", zap.Duration("wait", delay), zap.Int("attempt", rlAttempts))
			if sleepErr := rateLimitPolicy.sleep(ctx, delay); sleepErr != nil {
				return sleepErr
			}
			rlWaited += delay
			continue
		}

		// ── Transient server errors (500/502/503/504) ─────────────────────
		if isUnavailableStatus(status) {
			uaAttempts++
			if uaAttempts >= unavailPolicy.maxAttempts {
				return apperror.New(apperror.KindUnavailable, fmt.Sprintf("AI provider temporarily unavailable after retries (HTTP %d)", status))
			}
			delay := retryDelay("", uaAttempts, unavailPolicy)
			if delay <= 0 || unavailPolicy.now().Sub(uaStarted)+uaWaited+delay > unavailPolicy.maxElapsed {
				return apperror.New(apperror.KindUnavailable, fmt.Sprintf("AI provider temporarily unavailable (HTTP %d)", status))
			}
			logger.Warn("provider unavailable; retrying", zap.Int("status", status), zap.Duration("wait", delay), zap.Int("attempt", uaAttempts))
			if sleepErr := unavailPolicy.sleep(ctx, delay); sleepErr != nil {
				return sleepErr
			}
			uaWaited += delay
			continue
		}

		// ── Other 4xx ────────────────────────────────────────────────────
		if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
			return apperror.New(apperror.KindInvalidRequest, fmt.Sprintf("AI provider rejected request (HTTP %d)", status))
		}
		return fmt.Errorf("provider returned HTTP %d", status)
	}
}

// isUnavailableStatus returns true for transient server-side errors worth retrying.
func isUnavailableStatus(status int) bool {
	return status == http.StatusInternalServerError ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

func retryDelay(retryAfter string, attempt int, policy retryPolicy) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(retryAfter); err == nil {
		if delay := when.Sub(policy.now()); delay > 0 {
			return delay
		}
	}
	delay := policy.baseDelay << (attempt - 1)
	if delay > policy.maxDelay {
		delay = policy.maxDelay
	}
	return policy.jitter(delay)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 32<<10))
	_ = body.Close()
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

type sanitizedError struct {
	message string
	cause   error
}

func (err *sanitizedError) Error() string { return "send provider request: " + err.message }
func (err *sanitizedError) Unwrap() error { return err.cause }

func sanitize(message string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return message
}

func findingIDs(requests []ai.ReviewRequest) []string {
	ids := make([]string, len(requests))
	for index := range requests {
		ids[index] = requests[index].Finding.ID
	}
	return ids
}

func singleReview(responses []ai.ReviewResponse, err error) (ai.ReviewResponse, error) {
	if err != nil {
		return ai.ReviewResponse{}, err
	}
	if len(responses) != 1 {
		return ai.ReviewResponse{}, fmt.Errorf("provider returned %d reviews for one finding", len(responses))
	}
	return responses[0], nil
}

func joinURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}
