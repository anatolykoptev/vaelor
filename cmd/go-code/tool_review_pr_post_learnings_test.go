package main

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/policy"
	"github.com/anatolykoptev/go-code/internal/review"
)

// spyPersister implements learningsPersister and records every Upsert call.
type spyPersister struct {
	calls   []learnings.Record
	failAll bool
}

func (s *spyPersister) Upsert(_ context.Context, r learnings.Record) error {
	s.calls = append(s.calls, r)
	if s.failAll {
		return errors.New("boom")
	}
	return nil
}

func TestOutcomeFromEvent(t *testing.T) {
	cases := []struct {
		event string
		want  string
	}{
		{"APPROVE", "good"},
		{"approve", "good"},
		{"REQUEST_CHANGES", "bad"},
		{"request_changes", "bad"},
		{"COMMENT", "neutral"},
		{"", "neutral"},
		{"SOMETHING_ELSE", "neutral"},
	}
	for _, c := range cases {
		if got := outcomeFromEvent(c.event); got != c.want {
			t.Errorf("outcomeFromEvent(%q) = %q, want %q", c.event, got, c.want)
		}
	}
}

func TestPersistChangedSymbols_NilPersister_NoPanic(t *testing.T) {
	// Must not panic or error when persister is nil.
	syms := []review.ChangedSymbol{
		{Symbol: &parser.Symbol{Name: "Foo"}, ChangeType: review.ChangeModified},
	}
	persistChangedSymbols(context.Background(), nil, "owner/repo", "", "good", "/tmp", syms, nil)
}

func TestPersistChangedSymbols_CallsUpsertPerSymbol(t *testing.T) {
	sp := &spyPersister{}
	syms := []review.ChangedSymbol{
		{
			Symbol:     &parser.Symbol{Name: "Foo", File: "/repo/foo.go", StartLine: 1, EndLine: 10},
			ChangeType: review.ChangeModified,
		},
		{
			Symbol:     &parser.Symbol{Name: "Bar", File: "/repo/bar.go", StartLine: 1, EndLine: 5},
			ChangeType: review.ChangeAdded,
		},
	}
	persistChangedSymbols(
		context.Background(), sp,
		"owner/repo", "https://github.com/owner/repo/pull/42",
		"good", "/repo", syms, nil,
	)
	if len(sp.calls) != 2 {
		t.Fatalf("want 2 Upsert calls, got %d", len(sp.calls))
	}
	for i, want := range []string{"Foo", "Bar"} {
		if sp.calls[i].Symbol != want {
			t.Errorf("call[%d].Symbol = %q, want %q", i, sp.calls[i].Symbol, want)
		}
		if sp.calls[i].Repo != "owner/repo" {
			t.Errorf("call[%d].Repo = %q, want owner/repo", i, sp.calls[i].Repo)
		}
		if sp.calls[i].ReviewOutcome != "good" {
			t.Errorf("call[%d].ReviewOutcome = %q, want good", i, sp.calls[i].ReviewOutcome)
		}
		if sp.calls[i].PRURL != "https://github.com/owner/repo/pull/42" {
			t.Errorf("call[%d].PRURL wrong: %q", i, sp.calls[i].PRURL)
		}
	}
	// Without findings, Flag falls back to ChangeType; Note stays empty.
	if sp.calls[0].Flag != "modified" {
		t.Errorf("call[0].Flag = %q, want modified", sp.calls[0].Flag)
	}
	if sp.calls[1].Flag != "added" {
		t.Errorf("call[1].Flag = %q, want added", sp.calls[1].Flag)
	}
	if sp.calls[0].Note != "" {
		t.Errorf("call[0].Note = %q, want empty", sp.calls[0].Note)
	}
}

func TestPersistChangedSymbols_DerivesFlagAndNoteFromFinding(t *testing.T) {
	sp := &spyPersister{}
	syms := []review.ChangedSymbol{
		{
			Symbol:     &parser.Symbol{Name: "Foo", File: "/repo/foo.go", StartLine: 10, EndLine: 30},
			ChangeType: review.ChangeModified,
		},
	}
	// Finding on file foo.go at line 15 (inside Foo's range) should win over fallback.
	findings := []policy.Finding{
		{Path: "foo.go", Line: 15, Rule: "forbidden_import", Message: "use stdlib"},
	}
	persistChangedSymbols(
		context.Background(), sp,
		"owner/repo", "https://github.com/owner/repo/pull/42",
		"bad", "/repo", syms, findings,
	)
	if len(sp.calls) != 1 {
		t.Fatalf("want 1 Upsert call, got %d", len(sp.calls))
	}
	if sp.calls[0].Flag != "forbidden_import" {
		t.Errorf("Flag = %q, want forbidden_import", sp.calls[0].Flag)
	}
	if sp.calls[0].Note != "use stdlib" {
		t.Errorf("Note = %q, want 'use stdlib'", sp.calls[0].Note)
	}
}

func TestPersistChangedSymbols_FindingOutsideRange_UsesFallback(t *testing.T) {
	sp := &spyPersister{}
	syms := []review.ChangedSymbol{
		{
			Symbol:     &parser.Symbol{Name: "Foo", File: "/repo/foo.go", StartLine: 10, EndLine: 30},
			ChangeType: review.ChangeModified,
		},
	}
	// Finding on foo.go, but line 100 is outside Foo's [10,30] range.
	findings := []policy.Finding{
		{Path: "foo.go", Line: 100, Rule: "forbidden_import", Message: "use stdlib"},
	}
	persistChangedSymbols(
		context.Background(), sp,
		"owner/repo", "",
		"neutral", "/repo", syms, findings,
	)
	if len(sp.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(sp.calls))
	}
	if sp.calls[0].Flag != "modified" {
		t.Errorf("Flag = %q, want modified (fallback)", sp.calls[0].Flag)
	}
}

func TestPersistChangedSymbols_UpsertErrorDoesNotPanic(t *testing.T) {
	sp := &spyPersister{failAll: true}
	syms := []review.ChangedSymbol{
		{Symbol: &parser.Symbol{Name: "Foo", File: "/repo/foo.go"}, ChangeType: review.ChangeModified},
	}
	// Must not panic; errors are swallowed (logged via slog).
	persistChangedSymbols(context.Background(), sp, "r", "", "good", "/repo", syms, nil)
	if len(sp.calls) != 1 {
		t.Fatalf("want 1 call even on error, got %d", len(sp.calls))
	}
}
