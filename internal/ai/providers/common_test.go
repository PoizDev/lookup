package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/apperror"
)

type policyRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn policyRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestPostJSONRetriesObeyScanAttemptBudget(t *testing.T) {
	attempts, reservations := 0, 0
	client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	policy := retryPolicy{maxAttempts: 3, maxElapsed: time.Second, baseDelay: time.Millisecond, maxDelay: time.Millisecond, sleep: func(context.Context, time.Duration) error { return nil }, now: time.Now, jitter: func(delay time.Duration) time.Duration { return delay }}
	ctx := ai.WithProviderAttemptBudget(context.Background(), func() bool { reservations++; return reservations <= 1 })
	err := postJSONWithPolicy(ctx, client, "https://example.invalid", nil, map[string]string{"x": "y"}, &map[string]any{}, policy, policy)
	if !errors.Is(err, ai.ErrReviewBudgetExhausted) || attempts != 1 || reservations != 2 {
		t.Fatalf("err=%v attempts=%d reservations=%d", err, attempts, reservations)
	}
}

func TestHTTPClientHasBoundedDefaultTimeout(t *testing.T) {
	if got := httpClient(nil).Timeout; got != 30*time.Second {
		t.Fatalf("default timeout = %v, want 30s", got)
	}
	if got := httpClient(&http.Client{}).Timeout; got != 30*time.Second {
		t.Fatalf("injected unbounded client timeout = %v, want 30s", got)
	}
}

func TestPostJSONCancelsInFlightRequestPromptly(t *testing.T) {
	started := make(chan struct{})
	client := &http.Client{Timeout: 30 * time.Second, Transport: policyRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- postJSON(ctx, client, "https://example.invalid", nil, map[string]string{"x": "y"}, &map[string]any{})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight request did not stop promptly")
	}
}

func TestPostJSONHonorsRetryAfterAndBoundsAttempts(t *testing.T) {
	attempts := 0
	waits := make([]time.Duration, 0, 2)
	client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"2"}},
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	})}
	policy := retryPolicy{
		maxAttempts: 3,
		maxElapsed:  10 * time.Second,
		baseDelay:   time.Millisecond,
		maxDelay:    5 * time.Second,
		sleep: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
		now:    time.Now,
		jitter: func(delay time.Duration) time.Duration { return delay },
	}

	err := postJSONWithPolicy(context.Background(), client, "https://example.invalid", nil, map[string]string{"x": "y"}, &map[string]any{}, policy, policy)
	if attempts != 3 || len(waits) != 2 || waits[0] != 2*time.Second {
		t.Fatalf("attempts=%d waits=%v, want 3 attempts and Retry-After waits", attempts, waits)
	}
	if !apperror.IsKind(err, apperror.KindRateLimit) {
		t.Fatalf("error = %#v, want rate limit error", err)
	}
}

func TestPostJSON429ThenSuccess(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"1"}},
				Body:       io.NopCloser(strings.NewReader("{}")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
		}, nil
	})}
	policy := retryPolicy{
		maxAttempts: 3,
		maxElapsed:  10 * time.Second,
		baseDelay:   time.Millisecond,
		maxDelay:    time.Second,
		sleep: func(_ context.Context, _ time.Duration) error {
			return nil
		},
		now:    time.Now,
		jitter: func(delay time.Duration) time.Duration { return delay },
	}

	var result map[string]any
	err := postJSONWithPolicy(context.Background(), client, "https://example.invalid", nil, map[string]string{"x": "y"}, &result, policy, policy)
	if err != nil {
		t.Fatalf("unexpected error on 429-then-success: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if result["status"] != "ok" {
		t.Fatalf("result = %#v, want status=ok", result)
	}
}

func TestPostJSON503PersistentFailsWithUnavailable(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("service unavailable")),
		}, nil
	})}
	policy := retryPolicy{
		maxAttempts: 2,
		maxElapsed:  5 * time.Second,
		baseDelay:   time.Millisecond,
		maxDelay:    time.Second,
		sleep: func(_ context.Context, _ time.Duration) error {
			return nil
		},
		now:    time.Now,
		jitter: func(delay time.Duration) time.Duration { return delay },
	}

	err := postJSONWithPolicy(context.Background(), client, "https://example.invalid", nil, map[string]string{"x": "y"}, &map[string]any{}, policy, policy)
	if !apperror.IsKind(err, apperror.KindUnavailable) {
		t.Fatalf("error = %#v, want KindUnavailable", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestPostJSON503ThenSuccess(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("gateway error")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":"recovered"}`)),
		}, nil
	})}
	policy := retryPolicy{
		maxAttempts: 3,
		maxElapsed:  5 * time.Second,
		baseDelay:   time.Millisecond,
		maxDelay:    time.Second,
		sleep: func(_ context.Context, _ time.Duration) error {
			return nil
		},
		now:    time.Now,
		jitter: func(delay time.Duration) time.Duration { return delay },
	}

	var result map[string]any
	err := postJSONWithPolicy(context.Background(), client, "https://example.invalid", nil, map[string]string{"x": "y"}, &result, policy, policy)
	if err != nil {
		t.Fatalf("unexpected error on 503-then-success: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if result["status"] != "recovered" {
		t.Fatalf("result = %#v, want status=recovered", result)
	}
}

func TestPostJSONRepeated502And504ClassifiedAsUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusInternalServerError} {
		attempts := 0
		client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("server error")),
			}, nil
		})}
		policy := retryPolicy{
			maxAttempts: 2,
			maxElapsed:  5 * time.Second,
			baseDelay:   time.Millisecond,
			maxDelay:    time.Second,
			sleep: func(_ context.Context, _ time.Duration) error {
				return nil
			},
			now:    time.Now,
			jitter: func(delay time.Duration) time.Duration { return delay },
		}

		err := postJSONWithPolicy(context.Background(), client, "https://example.invalid", nil, map[string]string{"x": "y"}, &map[string]any{}, policy, policy)
		if !apperror.IsKind(err, apperror.KindUnavailable) {
			t.Fatalf("status %d error = %#v, want KindUnavailable", status, err)
		}
		if attempts != 2 {
			t.Fatalf("status %d attempts = %d, want 2", status, attempts)
		}
	}
}

func TestPostJSONCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	policy := retryPolicy{
		maxAttempts: 3,
		maxElapsed:  time.Second,
		baseDelay:   time.Millisecond,
		maxDelay:    time.Second,
		sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
		now:    time.Now,
		jitter: func(delay time.Duration) time.Duration { return delay },
	}

	err := postJSONWithPolicy(ctx, client, "https://example.invalid", nil, struct{}{}, &map[string]any{}, policy, policy)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestPostJSONClassifiesAuthenticationAndTimeout(t *testing.T) {
	authAttempts := 0
	authClient := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		authAttempts++
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	err := postJSON(context.Background(), authClient, "https://example.invalid", nil, struct{}{}, &map[string]any{})
	if !apperror.IsKind(err, apperror.KindAuthentication) {
		t.Fatalf("authentication error = %#v", err)
	}
	if authAttempts != 1 {
		t.Fatalf("auth attempts = %d, want 1 (never retry auth)", authAttempts)
	}

	forbiddenAttempts := 0
	forbiddenClient := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		forbiddenAttempts++
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	err = postJSON(context.Background(), forbiddenClient, "https://example.invalid", nil, struct{}{}, &map[string]any{})
	if !apperror.IsKind(err, apperror.KindAuthentication) {
		t.Fatalf("forbidden error = %#v", err)
	}
	if forbiddenAttempts != 1 {
		t.Fatalf("forbidden attempts = %d, want 1 (never retry 403)", forbiddenAttempts)
	}

	timeoutClient := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	if err := postJSON(context.Background(), timeoutClient, "https://example.invalid", nil, struct{}{}, &map[string]any{}); !apperror.IsKind(err, apperror.KindNetworkTimeout) {
		t.Fatalf("timeout error = %#v", err)
	}
}

func TestPostJSONClassifiesMalformedResponseAndInvalidRequest(t *testing.T) {
	badJSONClient := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not-valid-json"))}, nil
	})}
	var res map[string]any
	err := postJSON(context.Background(), badJSONClient, "https://example.invalid", nil, struct{}{}, &res)
	if !apperror.IsKind(err, apperror.KindMalformedResponse) {
		t.Fatalf("bad json error = %#v, want KindMalformedResponse", err)
	}

	badReqClient := &http.Client{Transport: policyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"bad payload"}`))}, nil
	})}
	err = postJSON(context.Background(), badReqClient, "https://example.invalid", nil, struct{}{}, &res)
	if !apperror.IsKind(err, apperror.KindInvalidRequest) {
		t.Fatalf("bad req error = %#v, want KindInvalidRequest", err)
	}
}
