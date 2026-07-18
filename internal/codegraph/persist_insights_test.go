package codegraph

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/learnings"
)

// stubStore is a minimal learningsStore stub for tests.
type stubStore struct {
	upserted   []learnings.Record
	existing   []learnings.Record
	upsertErr  error
	nearestErr error
}

func (s *stubStore) Upsert(_ context.Context, r learnings.Record) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted = append(s.upserted, r)
	return nil
}

func (s *stubStore) Nearest(_ context.Context, _, _ string, _ int) ([]learnings.Record, error) {
	if s.nearestErr != nil {
		return nil, s.nearestErr
	}
	return s.existing, nil
}

func TestPersistInsights_NilStore(t *testing.T) {
	n := PersistInsights(context.Background(), nil, "repo", TemplateInsightSurprises, [][]string{{"a", "b", "c", "d", "5", "r"}})
	if n != 0 {
		t.Fatalf("nil store: want 0, got %d", n)
	}
}

func TestPersistInsights_EmptyRows(t *testing.T) {
	s := &stubStore{}
	n := PersistInsights(context.Background(), s, "repo", TemplateInsightSurprises, nil)
	if n != 0 {
		t.Fatalf("empty rows: want 0, got %d", n)
	}
	if len(s.upserted) != 0 {
		t.Fatalf("expected no upserts, got %d", len(s.upserted))
	}
}

func TestPersistInsights_UnsupportedTemplate(t *testing.T) {
	s := &stubStore{}
	n := PersistInsights(context.Background(), s, "repo", "who_calls", [][]string{{"a"}})
	if n != 0 {
		t.Fatalf("unsupported template: want 0, got %d", n)
	}
}

// TestPersistInsights_Surprises_ScoreFilter verifies that only rows whose
// score meets the threshold produce records, and that each qualifying edge
// produces exactly two records (one per endpoint symbol).
func TestPersistInsights_Surprises_ScoreFilter(t *testing.T) {
	rows := [][]string{
		// score=3 → below threshold, should not persist
		{"FromA", "pkg/a.go", "ToA", "pkg/b.go", "3", "reason low"},
		// score=5 → at threshold, should persist FromB + ToB
		{"FromB", "pkg/b.go", "ToB", "pkg/c.go", "5", "reason high"},
	}
	s := &stubStore{}
	n := PersistInsights(context.Background(), s, "myrepo", TemplateInsightSurprises, rows)
	if n != 2 {
		t.Fatalf("want 2 records (FromB + ToB), got %d", n)
	}
	flags := make(map[string]string)
	for _, r := range s.upserted {
		flags[r.Symbol] = r.Flag
	}
	if flags["FromB"] != "hidden_dep" {
		t.Errorf("FromB: want flag hidden_dep, got %q", flags["FromB"])
	}
	if flags["ToB"] != "hidden_dep" {
		t.Errorf("ToB: want flag hidden_dep, got %q", flags["ToB"])
	}
	if _, present := flags["FromA"]; present {
		t.Error("FromA should not be persisted (score too low)")
	}
}

// TestPersistInsights_DeadCode verifies one record per row.
func TestPersistInsights_DeadCode(t *testing.T) {
	rows := [][]string{
		{`{"id":1,"label":"Symbol","properties":{"name":"OldFunc","kind":"function","file":"x.go"}}`},
		{`{"id":2,"label":"Symbol","properties":{"name":"UnusedType","kind":"type","file":"y.go"}}`},
		{`{"id":3,"label":"Symbol","properties":{"name":"DeadHelper","kind":"function","file":"z.go"}}`},
	}
	s := &stubStore{}
	n := PersistInsights(context.Background(), s, "repo", TemplateInsightDeadCode, rows)
	if n != 3 {
		t.Fatalf("want 3 dead_code records, got %d", n)
	}
	for _, r := range s.upserted {
		if r.Flag != "dead_code_candidate" {
			t.Errorf("symbol %q: want flag dead_code_candidate, got %q", r.Symbol, r.Flag)
		}
	}
}

// TestPersistInsights_Dedupe verifies that a symbol with an existing record
// matching the flag is skipped.
func TestPersistInsights_Dedupe(t *testing.T) {
	rows := [][]string{
		// Both FromC and ToC have score=5 but FromC already has hidden_dep.
		{"FromC", "pkg/c.go", "ToC", "pkg/d.go", "5", "already there"},
	}
	s := &stubStore{
		// Nearest returns a record with flag=hidden_dep for any symbol.
		existing: []learnings.Record{{Flag: "hidden_dep"}},
	}
	n := PersistInsights(context.Background(), s, "repo", TemplateInsightSurprises, rows)
	// Both symbols are deduped → 0.
	if n != 0 {
		t.Fatalf("dedupe: want 0, got %d", n)
	}
	if len(s.upserted) != 0 {
		t.Fatalf("dedupe: expected no upserts, got %d", len(s.upserted))
	}
}

func TestExtractNodeName_JSONNode(t *testing.T) {
	raw := `{"id":42,"label":"Symbol","properties":{"name":"FooBar","kind":"function"}}`
	got := extractNodeName(raw)
	if got != "FooBar" {
		t.Fatalf("want FooBar, got %q", got)
	}
}

func TestExtractNodeName_PlainName(t *testing.T) {
	got := extractNodeName("MySymbol")
	if got != "MySymbol" {
		t.Fatalf("want MySymbol, got %q", got)
	}
}

func TestExtractNodeName_EmptyOrBraces(t *testing.T) {
	if got := extractNodeName("{}"); got != "" {
		t.Fatalf("want empty for brace-only, got %q", got)
	}
	if got := extractNodeName(""); got != "" {
		t.Fatalf("want empty for blank, got %q", got)
	}
}
