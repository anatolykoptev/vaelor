package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// maxRateLimitWait is the maximum time to block for a rate-limit reset.
	maxRateLimitWait = 30 * time.Second

	// ghErrorBodyLimit caps how much of an error body we read.
	ghErrorBodyLimit = 1024
)

// RetryConfig controls the exponential backoff for transient failures.
type RetryConfig struct {
	MaxRetries  int
	InitialWait time.Duration
	MaxWait     time.Duration
	Multiplier  float64
}

// defaultRetryConfig returns the standard retry policy for GitHub requests.
func defaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  3,
		InitialWait: 1 * time.Second,
		MaxWait:     30 * time.Second,
		Multiplier:  2.0,
	}
}

// doGitHubRequest executes the request with retry/backoff and rate-limit
// handling. It consumes the response body on success and returns a response
// whose Body can be read by the caller. The body is closed on error paths.
func (g *GitHubForge) doGitHubRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	cfg := defaultRetryConfig()
	backoff := cfg.InitialWait
	baseReq := req

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req := baseReq.WithContext(attemptCtx)
		resp, err := g.http.Do(req)
		if err != nil {
			cancel()
			if attempt == cfg.MaxRetries || !isRetryableError(err) {
				return nil, err
			}
			if err := waitOrDone(ctx, backoff); err != nil {
				return nil, err
			}
			backoff = nextBackoff(backoff, cfg)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			cancel()
			if err != nil {
				return nil, err
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return resp, nil
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			wait, ok := githubRateLimitWait(resp)
			if ok {
				_ = resp.Body.Close()
				if wait > maxRateLimitWait {
					cancel()
					return nil, githubRateLimitError(resp)
				}
				if wait < 0 {
					wait = 0
				}
				cancel()
				if wait > 0 {
					if err := waitOrDone(ctx, wait); err != nil {
						return nil, err
					}
				}
				continue
			}
			// 403 without rate-limit headers is a hard failure.
			if resp.StatusCode == http.StatusForbidden {
				cancel()
				return nil, newGitHubAPIError(resp, "github request")
			}
			// 429 without rate-limit headers falls through to generic retry.
		}

		if isRetryableStatus(resp.StatusCode) {
			if attempt == cfg.MaxRetries {
				cancel()
				return nil, newGitHubAPIError(resp, "github request")
			}
			_ = resp.Body.Close()
			cancel()
			if err := waitOrDone(ctx, backoff); err != nil {
				return nil, err
			}
			backoff = nextBackoff(backoff, cfg)
			continue
		}

		cancel()
		return nil, newGitHubAPIError(resp, "github request")
	}

	return nil, fmt.Errorf("github request: max retries exceeded")
}

// githubRateLimitWait extracts the suggested wait from a rate-limited response.
// It returns (wait, true) if Retry-After or X-RateLimit-Reset are present.
func githubRateLimitWait(resp *http.Response) (time.Duration, bool) {
	if h := resp.Header.Get("Retry-After"); h != "" {
		if wait, ok := parseRetryAfter(h); ok {
			return wait, true
		}
	}
	if h := resp.Header.Get("X-RateLimit-Reset"); h != "" {
		ts, err := strconv.ParseInt(h, 10, 64)
		if err == nil {
			return time.Until(time.Unix(ts, 0)), true
		}
	}
	return 0, false
}

// githubRateLimitError builds a descriptive error for rate-limit responses.
func githubRateLimitError(resp *http.Response) error {
	var details []string
	if wait, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok && wait > 0 {
		details = append(details, fmt.Sprintf("retry after %s", wait))
	}
	if h := resp.Header.Get("X-RateLimit-Reset"); h != "" {
		ts, err := strconv.ParseInt(h, 10, 64)
		if err == nil {
			details = append(details, fmt.Sprintf("rate limit resets at %s", time.Unix(ts, 0).Format(time.RFC3339)))
		}
	}
	msg := "rate limit exceeded"
	if len(details) > 0 {
		msg += " (" + strings.Join(details, ", ") + ")"
	}
	return fmt.Errorf("github request: HTTP %d — %s", resp.StatusCode, msg)
}

// githubAPIError is a structured GitHub API error.
type githubAPIError struct {
	statusCode int
	msg        string
}

func (e *githubAPIError) Error() string { return e.msg }

// NewGitHubAPIError constructs a structured GitHub API error with the given
// status code and message. Exported so callers (and tests) can build a value
// that IsTransientAPIError recognizes — e.g. a fake forge simulating a 408
// timeout (issue #567).
func NewGitHubAPIError(statusCode int, msg string) error {
	return &githubAPIError{statusCode: statusCode, msg: msg}
}

// newGitHubAPIError reads the response body and builds a descriptive error.
func newGitHubAPIError(resp *http.Response, context string) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, ghErrorBodyLimit))
	var parsed struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Message != "" {
		detail := parsed.Message
		if len(parsed.Errors) > 0 && parsed.Errors[0].Message != "" {
			detail += ": " + parsed.Errors[0].Message
		}
		return &githubAPIError{
			statusCode: resp.StatusCode,
			msg:        fmt.Sprintf("%s: HTTP %d — %s", context, resp.StatusCode, detail),
		}
	}
	return &githubAPIError{
		statusCode: resp.StatusCode,
		msg:        fmt.Sprintf("%s: HTTP %d", context, resp.StatusCode),
	}
}

// waitOrDone sleeps for d or until ctx is cancelled.
func waitOrDone(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nextBackoff computes the next exponential backoff, capped at cfg.MaxWait.
func nextBackoff(current time.Duration, cfg RetryConfig) time.Duration {
	next := time.Duration(float64(current) * cfg.Multiplier)
	if next > cfg.MaxWait {
		return cfg.MaxWait
	}
	return next
}

// isRetryableStatus reports whether a non-2xx HTTP status should be retried.
func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, // 408 — GitHub Code Search times out on complex queries (issue #567)
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// IsTransientAPIError reports whether err is a GitHub API error whose status
// is transient and worth surfacing with a retry/simplify hint to the caller
// (408 Request Timeout or 5xx). Exported so tool handlers (github_code_search)
// can append a query-simplification hint on these failures (issue #567).
// 4xx other than 408 are NOT transient.
func IsTransientAPIError(err error) bool {
	var apiErr *githubAPIError
	if errors.As(err, &apiErr) {
		return apiErr.statusCode == http.StatusRequestTimeout || apiErr.statusCode >= 500
	}
	return false
}

// isRetryableError reports whether a network/transport error is worth retrying.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return false
}

// parseRetryAfter parses a Retry-After header value. It supports delta
// seconds and HTTP-date formats.
func parseRetryAfter(h string) (time.Duration, bool) {
	if h == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n >= 0 {
		return time.Duration(n) * time.Second, true
	}
	if t, err := http.ParseTime(strings.TrimSpace(h)); err == nil {
		wait := time.Until(t)
		if wait < 0 {
			wait = 0
		}
		return wait, true
	}
	return 0, false
}
