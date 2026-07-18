package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestEnrichTestedSetViaOxCodes_UsesFunctionBodiesScope asserts that the ox-codes
// scoped search sent by enrichTestedSetViaOxCodes uses the valid "function_bodies"
// scope, not the invalid "function" scope that ox-codes rejects with 400.
//
// Regression guard for go-code issue #419: delta.go called /search/scoped with
// Scope: "function", so every enrichment call was rejected and silently ignored.
// The body-search backstop for review_delta's untested-symbol detection was dead.
//
// RED guarantee: revert the delta.go fix to Scope: "function", and this test fails
// because the captured request scope is "function" instead of "function_bodies".
func TestEnrichTestedSetViaOxCodes_UsesFunctionBodiesScope(t *testing.T) {
	t.Parallel()

	var gotScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/scoped" {
			t.Errorf("expected /search/scoped, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		defer func() { _ = r.Body.Close() }()

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if s, ok := body["scope"].(string); ok {
			gotScope = s
		}

		resp := oxcodes.SearchResponse{
			Matches:      []oxcodes.SearchMatch{},
			TotalMatches: 0,
			Truncated:    false,
			DurationMS:   0,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := oxcodes.NewClient(srv.URL)
	changed := []ChangedSymbol{
		{
			Symbol:     &parser.Symbol{Name: "EnsureGraph", Language: "go", File: "/repo/service.go"},
			ChangeType: ChangeModified,
		},
	}
	testedSet := map[string]bool{}
	enrichTestedSetViaOxCodes(context.Background(), client, "/repo", changed, testedSet)

	if gotScope == "" {
		t.Fatal("no /search/scoped request was received")
	}
	if gotScope != "function_bodies" {
		t.Errorf("expected scope %q, got %q", "function_bodies", gotScope)
	}
}
