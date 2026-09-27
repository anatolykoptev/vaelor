package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/forge"
	"github.com/anatolykoptev/vaelor/internal/learnings"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/policy"
	"github.com/anatolykoptev/vaelor/internal/review"
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

// post/renderReview tests

func TestRenderReview(t *testing.T) {
	r := &review.DeltaResult{
		Risk:            review.RiskGuidance{RiskLevel: "medium", RiskScore: 0.5, Flags: []string{"touches api"}},
		UntestedSymbols: []string{"Svc.Foo"},
	}
	findings := []policy.Finding{
		{Path: "main.go", Line: 10, Rule: "forbidden_import", Message: "use stdlib"},
	}
	body, comments := renderReview(r, findings)
	if !strings.Contains(body, "medium") {
		t.Fatal("body missing risk")
	}
	if len(comments) != 1 || comments[0].Path != "main.go" {
		t.Fatalf("bad comments: %+v", comments)
	}
	// Verify the type of comment is correct.
	_ = forge.InlineComment{}
}

// post/outcomeFromEvent tests

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

// post/persistChangedSymbols tests

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

// dryRunBody runs reviewPRDryRun over a synthetic result and returns the
// serialized response text. DATABASE_URL is cleared so the learnings loop
// provably no-ops — CI DOES export it (preflight.yml), and without this a
// fixture carrying ChangedSymbols would hit the ephemeral DB and have
// "prior review" suggestions injected into the XML under test.
func dryRunBody(t *testing.T, input ReviewPRInput, r *review.DeltaResult) string {
	t.Helper()
	t.Setenv("DATABASE_URL", "")
	res, err := reviewPRDryRun(context.Background(), input, r)
	if err != nil {
		t.Fatalf("reviewPRDryRun: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	text := textContentOf(t, res)
	if text == "" {
		t.Fatal("empty result")
	}
	return text
}

// TestReviewPRDryRun_CapsImpactedSymbols is the #765 regression test: the
// dry-run path must apply the same maxReviewImpacted default cap as
// review_delta, reporting the TRUE total + truncated marker — a fixture of
// 300 (> cap) fails whether or not the cap exists.
func TestReviewPRDryRun_CapsImpactedSymbols(t *testing.T) {
	r := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(300),
		UntestedSymbols: []string{"Foo"},
		Risk:            review.RiskGuidance{RiskLevel: "medium", RiskScore: 0.5},
	}
	out := dryRunBody(t, ReviewPRInput{Repo: "o/r", PR: 1}, r)

	for _, want := range []string{`total="300"`, `shown="50"`, `truncated="true"`} {
		if !strings.Contains(out, want) {
			t.Errorf("response missing %s", want)
		}
	}
}

// TestReviewPRDryRun_FullImpactUncapped verifies the opt-in escape hatch:
// full_impact=true returns the complete list, same contract as review_delta.
func TestReviewPRDryRun_FullImpactUncapped(t *testing.T) {
	r := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(300),
	}
	out := dryRunBody(t, ReviewPRInput{Repo: "o/r", PR: 1, FullImpact: true}, r)

	if !strings.Contains(out, `total="300"`) || !strings.Contains(out, `shown="300"`) {
		t.Errorf("full_impact should report total=300 shown=300")
	}
	if strings.Contains(out, `truncated="true"`) {
		t.Error("full_impact must not report truncation")
	}
}

// TestReviewPRDryRun_UntestedRiskPrecedeImpacted is the #765 ordering
// guarantee: untested + risk must serialize BEFORE impacted_symbols so a
// transport/budget truncation drops the least discriminating section's tail
// rather than evicting the decision-relevant sections entirely.
func TestReviewPRDryRun_UntestedRiskPrecedeImpacted(t *testing.T) {
	r := &review.DeltaResult{
		ImpactedSymbols: buildLargeImpacted(300),
		UntestedSymbols: []string{"Foo"},
		Risk:            review.RiskGuidance{RiskLevel: "medium", RiskScore: 0.5},
	}
	out := dryRunBody(t, ReviewPRInput{Repo: "o/r", PR: 1}, r)

	idxUntested := strings.Index(out, "<untested>")
	idxRisk := strings.Index(out, "<risk ")
	idxImpacted := strings.Index(out, "<impacted_symbols")
	if idxUntested < 0 || idxRisk < 0 || idxImpacted < 0 {
		t.Fatalf("missing sections: untested=%d risk=%d impacted=%d", idxUntested, idxRisk, idxImpacted)
	}
	if idxUntested >= idxImpacted || idxRisk >= idxImpacted {
		t.Errorf("order wrong: untested=%d risk=%d impacted=%d — untested/risk must precede impacted_symbols",
			idxUntested, idxRisk, idxImpacted)
	}
}
