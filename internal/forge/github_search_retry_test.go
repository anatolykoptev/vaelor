package forge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

// TestSearchCode_Retries408ThenSucceeds guards #567: a GitHub Code Search
// HTTP 408 (Request Timeout) must be retried; when the second attempt
// succeeds, the caller gets the result, not the 408 error. The forge's
// default backoff sleeps ~1s between attempts — this test accepts that delay.
// Reverting 408 from isRetryableStatus REDS this test (the 408 is surfaced
// as an error instead of retried).
func TestSearchCode_Retries408ThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/code", func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// First attempt: GitHub times out on a complex query.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestTimeout)
			_, _ = w.Write([]byte(`{"message":"This query timed out. Try a simpler query, or try again later"}`))
			return
		}
		// Second attempt: success.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"items": []map[string]any{
				{
					"name":     "main.go",
					"path":     "cmd/main.go",
					"html_url": "https://github.com/foo/bar/blob/main/cmd/main.go",
					"repository": map[string]any{
						"full_name": "foo/bar",
					},
					"text_matches": []map[string]any{
						{"fragment": "func main() {"},
					},
				},
			},
		})
	})

	g := newTestGitHubForge(t, mux)
	result, err := g.SearchCode(context.Background(), "turn relay rotate OR failover", []string{"foo/bar"})
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if got := attempts.Load(); got < 2 {
		t.Fatalf("expected at least 2 attempts (408 then 200), got %d", got)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result after retry, got %d", len(result.Results))
	}
	if result.Results[0].Repo != "foo/bar" {
		t.Errorf("Repo = %q, want foo/bar", result.Results[0].Repo)
	}
}

// TestSearchCode_408PersistReturnsError verifies that when 408 persists across
// all retries, the call fails with a structured error that IsTransientAPIError
// recognizes (so the tool handler can append a simplify-query hint). The
// default backoff would sleep 1s+2s+4s across 3 retries — to keep the test
// fast, the handler is exercised via a fake forge returning a 408 error
// directly (see cmd/vaelor handler test for the hint).
func TestIsTransientAPIError_408And5xx(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		transient bool
	}{
		{"408", NewGitHubAPIError(http.StatusRequestTimeout, "github request: HTTP 408 — timed out"), true},
		{"500", NewGitHubAPIError(http.StatusInternalServerError, "github request: HTTP 500"), true},
		{"503", NewGitHubAPIError(http.StatusServiceUnavailable, "github request: HTTP 503"), true},
		{"400 not transient", NewGitHubAPIError(http.StatusBadRequest, "github request: HTTP 400"), false},
		{"422 not transient", NewGitHubAPIError(http.StatusUnprocessableEntity, "github request: HTTP 422"), false},
		{"404 not transient", NewGitHubAPIError(http.StatusNotFound, "github request: HTTP 404"), false},
		{"wrapped 408", errors.Join(NewGitHubAPIError(http.StatusRequestTimeout, "HTTP 408")), true},
		{"plain error not transient", errors.New("some other error"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTransientAPIError(tc.err)
			if got != tc.transient {
				t.Errorf("IsTransientAPIError(%v) = %v, want %v", tc.err, got, tc.transient)
			}
		})
	}
}

// TestIsRetryableStatus_408 is the unit guard for the 408 addition (#567).
func TestIsRetryableStatus_408(t *testing.T) {
	if !isRetryableStatus(http.StatusRequestTimeout) {
		t.Error("408 must be retryable")
	}
	if isRetryableStatus(http.StatusBadRequest) {
		t.Error("400 must NOT be retryable")
	}
	if isRetryableStatus(http.StatusUnprocessableEntity) {
		t.Error("422 must NOT be retryable")
	}
}
