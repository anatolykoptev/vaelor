package impact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

// TestFindHiddenCallers_EmptyLanguage_SkipsScopedSearch asserts that FindHiddenCallers
// does NOT call /search/scoped when language is empty. Sending an empty language to
// ox-codes /search/scoped returns 400 — the defensive skip prevents the spurious error.
//
// Regression guard for investigation 2026-06-12 (ITEM 3): the missing omitempty tag
// on ScopedSearchInput.Language caused a 400 on every impact_analysis call without
// an explicit language. Combined with the omitempty fix, this guard ensures go-code
// never attempts a scoped search it knows will be rejected.
//
// RED guarantee: remove the `if language != ""` guard from FindHiddenCallers and
// the test fails because the fake server receives a scoped request with
// "language":"" and returns 400.
func TestFindHiddenCallers_EmptyLanguage_SkipsScopedSearch(t *testing.T) {
	scopedCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/scoped" {
			scopedCalled = true
			http.Error(w, `unsupported language: ""`, http.StatusBadRequest)
			return
		}
		// /search (string literal search) is allowed — return empty result.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"matches":[],"total_matches":0,"truncated":false,"duration_ms":0}`))
	}))
	defer srv.Close()

	client := oxcodes.NewClient(srv.URL)
	// Empty language — the common path when impact_analysis has no language input.
	callers := FindHiddenCallers(context.Background(), client, "/some/repo", "MyFunc", "")

	if scopedCalled {
		t.Error("FindHiddenCallers must not call /search/scoped when language is empty")
	}
	// String literal search still runs — result may be nil or empty, both fine.
	_ = callers
}

// TestFindHiddenCallers_WithLanguage_CallsScopedSearch asserts that when language is
// set, /search/scoped IS called (the fix must not suppress the scoped search for
// callers that provide a language).
func TestFindHiddenCallers_WithLanguage_CallsScopedSearch(t *testing.T) {
	scopedCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search/scoped" {
			scopedCalled = true
		}
		_, _ = w.Write([]byte(`{"matches":[],"total_matches":0,"truncated":false,"duration_ms":0}`))
	}))
	defer srv.Close()

	client := oxcodes.NewClient(srv.URL)
	FindHiddenCallers(context.Background(), client, "/some/repo", "MyFunc", "go")

	if !scopedCalled {
		t.Error("FindHiddenCallers must call /search/scoped when language is non-empty")
	}
}
