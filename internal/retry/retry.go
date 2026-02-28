// Package retry provides generic retry logic with exponential backoff.
//
// It is a leaf package with no internal dependencies, designed to be used
// by any package that makes fallible I/O calls (LLM client, GitHub client, etc.).
package retry

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Default retry constants.
const (
	DefaultMaxAttempts  = 3
	DefaultInitialDelay = 500 * time.Millisecond
	DefaultMaxDelay     = 5 * time.Second
)

// Options controls retry behavior.
// Zero values are replaced by the corresponding Default* constants.
type Options struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func (o *Options) applyDefaults() {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.InitialDelay <= 0 {
		o.InitialDelay = DefaultInitialDelay
	}
	if o.MaxDelay <= 0 {
		o.MaxDelay = DefaultMaxDelay
	}
}

// HTTPError is returned when an HTTP response has a retryable status code.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("retryable HTTP status %d", e.StatusCode)
}

// Do retries fn up to MaxAttempts times with exponential backoff.
// Respects context cancellation. Returns the last error if all attempts fail.
func Do[T any](ctx context.Context, opts Options, fn func() (T, error)) (T, error) {
	opts.applyDefaults()

	// Check context before any attempt.
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}

	delay := opts.InitialDelay
	var lastErr error

	for attempt := range opts.MaxAttempts {
		if attempt > 0 {
			// Wait for backoff delay or context cancellation.
			select {
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			case <-time.After(delay):
			}
			delay = min(delay*2, opts.MaxDelay)
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
	}

	var zero T
	return zero, lastErr
}

// isRetryableStatus reports whether the HTTP status code warrants a retry.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// HTTP retries an HTTP request function, treating 429 and 5xx as retryable.
// Returns the successful response, or the last error after exhausting attempts.
// The caller is responsible for closing the response body on success.
func HTTP(ctx context.Context, opts Options, fn func() (*http.Response, error)) (*http.Response, error) {
	return Do(ctx, opts, func() (*http.Response, error) {
		resp, err := fn()
		if err != nil {
			return nil, err
		}
		if isRetryableStatus(resp.StatusCode) {
			resp.Body.Close()
			return nil, &HTTPError{StatusCode: resp.StatusCode}
		}
		return resp, nil
	})
}
