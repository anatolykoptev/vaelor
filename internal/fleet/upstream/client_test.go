package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// buildTestCompareResponse builds a valid GitHub compare API response.
func buildTestCompareResponse(status string, nCommits int) githubCompareResp {
	commits := make([]githubCommitEntry, nCommits)
	for i := 0; i < nCommits; i++ {
		commits[i] = githubCommitEntry{
			SHA: fmt.Sprintf("abc%03d", i),
			Commit: githubCommitInner{
				Message: fmt.Sprintf("fix: issue %d\nsome body", i),
				Author:  githubAuthor{Name: "Author", Date: "2024-01-01T00:00:00Z"},
			},
		}
	}
	return githubCompareResp{
		Status:  status,
		HTMLURL: "https://github.com/owner/repo/compare/v1.0...v1.1",
		Commits: commits,
	}
}

// TestCompare_HappyPath_VPrefix: bare tags requested, server responds 404 for bare
// form but 200 for v-prefix form → Resolved=true.
func TestCompare_HappyPath_VPrefix(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Paths look like /repos/owner/repo/compare/BASE...HEAD
		parts := strings.Split(r.URL.Path, "/compare/")
		if len(parts) != 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		compareSpec := parts[1]
		// If neither side has v-prefix, return 404.
		if !strings.Contains(compareSpec, "v1.0") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := buildTestCompareResponse("ahead", 3)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cl.Resolved {
		t.Errorf("expected Resolved=true, got Reason=%q", cl.Reason)
	}
	if cl.Status != "ahead" {
		t.Errorf("Status=%q; want ahead", cl.Status)
	}
	if len(cl.Commits) != 3 {
		t.Errorf("Commits len=%d; want 3", len(cl.Commits))
	}
}

// TestCompare_AllTagAttemptsFail: all three tag forms (bare, v, release-) get 404.
func TestCompare_AllTagAttemptsFail(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if cl.Resolved {
		t.Error("expected Resolved=false when all tag forms fail")
	}
	if !strings.Contains(cl.Reason, "no matching tags") {
		t.Errorf("Reason=%q; want to contain 'no matching tags'", cl.Reason)
	}
}

// TestCompare_422_NoCommonAncestor: 422 → Resolved=false, Reason mentions ancestor.
func TestCompare_422_NoCommonAncestor(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if cl.Resolved {
		t.Error("expected Resolved=false for 422")
	}
	if !strings.Contains(cl.Reason, "ancestor") {
		t.Errorf("Reason=%q; want to contain 'ancestor'", cl.Reason)
	}
}

// TestCompare_403_RateLimit: 403 + X-RateLimit-Remaining: 0 → Resolved=false, Reason mentions rate limit.
func TestCompare_403_RateLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if cl.Resolved {
		t.Error("expected Resolved=false for rate-limited 403")
	}
	if !strings.Contains(cl.Reason, "rate limit") {
		t.Errorf("Reason=%q; want to contain 'rate limit'", cl.Reason)
	}
}

// TestCompare_ContextDeadline: context times out → Resolved=false, Reason mentions timeout.
func TestCompare_ContextDeadline(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cl, err := client.Compare(ctx, "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	if cl.Resolved {
		t.Error("expected Resolved=false on context deadline")
	}
	reason := strings.ToLower(cl.Reason)
	if !strings.Contains(reason, "context") && !strings.Contains(reason, "deadline") &&
		!strings.Contains(reason, "timeout") && !strings.Contains(reason, "canceled") {
		t.Errorf("Reason=%q; want to mention context/deadline/timeout/canceled", cl.Reason)
	}
}

// TestCompare_CommitCap: upstream returns 100 commits → capped at 20, Truncated=true.
func TestCompare_CommitCap(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := buildTestCompareResponse("ahead", 100)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cl.Resolved {
		t.Errorf("expected Resolved=true, got Reason=%q", cl.Reason)
	}
	if len(cl.Commits) > 20 {
		t.Errorf("Commits len=%d; want ≤20", len(cl.Commits))
	}
	if !cl.Truncated {
		t.Error("expected Truncated=true when upstream has >20 commits")
	}
}

// TestCompare_BodyCap: huge response body → no panic, returns something.
func TestCompare_BodyCap(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ahead","commits":[`))
		// Write many entries to exceed 16 KiB.
		entry := `{"sha":"x","commit":{"message":"m","author":{"name":"a","date":"2024-01-01T00:00:00Z"}}},`
		for i := 0; i < 300; i++ {
			w.Write([]byte(entry))
		}
		w.Write([]byte(`]}`))
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	// Must not panic.
	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected non-nil error: %v", err)
	}
	// Either resolved with truncated commits or not resolved (invalid JSON at limit).
	_ = cl
}

// TestCompare_CommitSubjectExtraction: only first line used as Subject.
func TestCompare_CommitSubjectExtraction(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := githubCompareResp{
			Status:  "ahead",
			HTMLURL: "https://github.com/owner/repo/compare/v1.0...v1.1",
			Commits: []githubCommitEntry{
				{
					SHA: "abc123",
					Commit: githubCommitInner{
						Message: "feat: add feature\n\nLong body here.",
						Author:  githubAuthor{Name: "Dev", Date: "2024-05-01T12:00:00Z"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cl.Commits) != 1 {
		t.Fatalf("Commits len=%d; want 1", len(cl.Commits))
	}
	if cl.Commits[0].Subject != "feat: add feature" {
		t.Errorf("Subject=%q; want first line only", cl.Commits[0].Subject)
	}
	if cl.Commits[0].Author != "Dev" {
		t.Errorf("Author=%q; want Dev", cl.Commits[0].Author)
	}
}

// TestCompare_URLPopulated: Resolved=true → URL is populated from html_url.
func TestCompare_URLPopulated(t *testing.T) {
	t.Parallel()
	const compareURL = "https://github.com/owner/repo/compare/v1.0...v1.1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := githubCompareResp{
			Status:  "ahead",
			HTMLURL: compareURL,
			Commits: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	cl, err := client.Compare(context.Background(), "owner/repo", "1.0", "1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cl.URL != compareURL {
		t.Errorf("URL=%q; want %q", cl.URL, compareURL)
	}
}
