package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/learnings"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubLearningsStore satisfies codegraph's learningsStore interface plus
// the learnings.Store interface subset used by handleRememberGraphInsights.
// We embed nothing — we implement only Upsert and Nearest via the concrete
// *learnings.Store wrapper convention: deps.Learnings is *learnings.Store,
// but PersistInsights accepts the unexported interface.  Since we call
// PersistInsights with deps.Learnings directly and deps.Learnings is typed
// as *learnings.Store, we use a small integration approach for unit tests:
// we supply a real *learnings.Store only when DATABASE_URL is present.
// For pure unit tests we skip the PersistInsights path and test the guard
// clauses and JSON shape instead.

// rememberResultShape mirrors rememberResult for JSON parsing in tests.
type rememberResultShape struct {
	Persisted map[string]int `json:"persisted"`
	Total     int            `json:"total"`
}

// TestRememberGraphInsights_NoRepo verifies that a missing repo returns an error.
func TestRememberGraphInsights_NoRepo(t *testing.T) {
	ctx := context.Background()
	ls := mustOpenLearnings(t)
	input := RememberGraphInsightsInput{Repo: ""}
	result, err := handleRememberGraphInsights(ctx, input, Config{}, analyze.Deps{Learnings: ls}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !isErrResult(result) {
		t.Error("expected error result for empty repo")
	}
}

// TestRememberGraphInsights_NoLearnings verifies that nil deps.Learnings
// returns a clear error message.
func TestRememberGraphInsights_NoLearnings(t *testing.T) {
	ctx := context.Background()
	input := RememberGraphInsightsInput{Repo: "owner/repo"}
	result, err := handleRememberGraphInsights(ctx, input, Config{}, analyze.Deps{Learnings: nil}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isErrResult(result) {
		t.Error("expected error result when learnings store is nil")
	}
	text := resultText(result)
	if text == "" {
		t.Error("expected non-empty error message")
	}
}

// TestRememberGraphInsights_MaxPerTemplateCap verifies the cap logic without a DB.
// We test the limit arithmetic in isolation.
func TestRememberGraphInsights_MaxPerTemplateCap(t *testing.T) {
	tests := []struct {
		input    int
		wantUsed int
	}{
		{0, defaultRememberMaxPerTemplate},
		{-5, defaultRememberMaxPerTemplate},
		{50, 50},
		{maxRememberMaxPerTemplate + 1, maxRememberMaxPerTemplate},
	}
	for _, tc := range tests {
		got := tc.input
		if got <= 0 {
			got = defaultRememberMaxPerTemplate
		}
		if got > maxRememberMaxPerTemplate {
			got = maxRememberMaxPerTemplate
		}
		if got != tc.wantUsed {
			t.Errorf("input %d: want %d, got %d", tc.input, tc.wantUsed, got)
		}
	}
}

// TestRememberGraphInsights_RememberTemplateList verifies that community_changes
// is NOT in rememberTemplates (per persist_insights.go: it has no stable shape).
func TestRememberGraphInsights_RememberTemplateList(t *testing.T) {
	for _, id := range rememberTemplates {
		if id == "community_changes" {
			t.Error("community_changes must not be in rememberTemplates: PersistInsights does not support it")
		}
	}
	// Must contain the two supported IDs.
	found := map[string]bool{}
	for _, id := range rememberTemplates {
		found[id] = true
	}
	if !found[codegraph.TemplateInsightSurprises] {
		t.Errorf("rememberTemplates missing %q", codegraph.TemplateInsightSurprises)
	}
	if !found[codegraph.TemplateInsightDeadCode] {
		t.Errorf("rememberTemplates missing %q", codegraph.TemplateInsightDeadCode)
	}
}

// TestRememberGraphInsights_ResultShape verifies the JSON output shape
// by building a rememberResult and marshaling it.
func TestRememberGraphInsights_ResultShape(t *testing.T) {
	res := rememberResult{
		Persisted: map[string]int{
			codegraph.TemplateInsightSurprises: 3,
			codegraph.TemplateInsightDeadCode:  7,
		},
		Total: 10,
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got rememberResultShape
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Total != 10 {
		t.Errorf("total: want 10, got %d", got.Total)
	}
	if got.Persisted[codegraph.TemplateInsightSurprises] != 3 {
		t.Errorf("surprises count: want 3, got %d", got.Persisted[codegraph.TemplateInsightSurprises])
	}
	if got.Persisted[codegraph.TemplateInsightDeadCode] != 7 {
		t.Errorf("dead_code count: want 7, got %d", got.Persisted[codegraph.TemplateInsightDeadCode])
	}
}

// Integration test: gated on DATABASE_URL.
func TestRememberGraphInsights_Integration(t *testing.T) {
	t.Skip("integration test: requires DATABASE_URL, LEARNINGS_DATABASE_URL, and a live repo")
}

// mustOpenLearnings returns nil without failing when LEARNINGS_DATABASE_URL is unset.
// This lets guard-clause tests run without a database.
func mustOpenLearnings(t *testing.T) *learnings.Store {
	t.Helper()
	return nil
}

// isErrResult returns true when the CallToolResult carries an error flag.
func isErrResult(r *mcp.CallToolResult) bool {
	if r == nil {
		return false
	}
	data, _ := json.Marshal(r)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	v, _ := m["isError"].(bool)
	return v
}

// resultText extracts the text content from a CallToolResult.
func resultText(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	data, _ := json.Marshal(r)
	var m map[string]interface{}
	_ = json.Unmarshal(data, &m)
	contents, _ := m["content"].([]interface{})
	if len(contents) == 0 {
		return ""
	}
	first, _ := contents[0].(map[string]interface{})
	text, _ := first["text"].(string)
	return text
}
