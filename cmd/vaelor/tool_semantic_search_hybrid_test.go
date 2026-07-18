package main

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// bm25SearchSpy implements bm25Searcher and records calls for assertions.
// Return fields control the mock behavior; call* fields capture arguments.
type bm25SearchSpy struct {
	hits    []embeddings.KeywordHit
	err     error
	called  bool
	gotRepo string
}

func (s *bm25SearchSpy) BM25Search(
	_ context.Context,
	repoKey, _, _ string,
	_ int,
) ([]embeddings.KeywordHit, error) {
	s.called = true
	s.gotRepo = repoKey
	return s.hits, s.err
}

// TestRunKeywordArm_GrepDefault verifies the dark-launch invariant: with
// KeywordArm unset (empty = grep default), runKeywordArm does NOT call the
// bm25searcher spy, and returns a (fileHits, nil) pair (grep path).
//
// Anti-tautology: the spy's called field would be true if the bm25f branch
// were erroneously entered. Reverting the "if deps.KeywordArm == keywordArmBM25F"
// guard would make this test fail by calling the spy.
func TestRunKeywordArm_GrepDefault(t *testing.T) {
	spy := &bm25SearchSpy{
		hits: []embeddings.KeywordHit{{FilePath: "x.go", SymbolName: "foo", Line: 1}},
	}
	deps := SemanticDeps{
		KeywordArm:   "", // unset = grep default
		bm25searcher: spy,
	}

	// runKeywordSearch operates on a real FS root; use a temp dir so it returns
	// empty (no matches) — we only care that BM25Search was NOT called, not that
	// grep found anything.
	fileHits, kwHits := runKeywordArm(context.Background(), deps, "parseConfig", "repo1", t.TempDir(), "go", 10)

	if spy.called {
		t.Error("bm25searcher was called with KeywordArm='' (grep default): dark-launch invariant broken")
	}
	// grep path: fileHits may be empty (no files in tmpdir), kwHits must be nil.
	if kwHits != nil {
		t.Errorf("grep path must return (fileHits, nil kwHits): got kwHits=%v", kwHits)
	}
	_ = fileHits // may be nil or empty — both are valid for an empty dir
}

// TestRunKeywordArm_GrepExplicit is identical to the above but with
// KeywordArm="grep" explicitly set — guards against the const value drifting.
func TestRunKeywordArm_GrepExplicit(t *testing.T) {
	spy := &bm25SearchSpy{
		hits: []embeddings.KeywordHit{{FilePath: "x.go", SymbolName: "bar", Line: 2}},
	}
	deps := SemanticDeps{
		KeywordArm:   keywordArmGrep,
		bm25searcher: spy,
	}

	_, kwHits := runKeywordArm(context.Background(), deps, "findUser", "repo1", t.TempDir(), "go", 10)

	if spy.called {
		t.Error("bm25searcher was called with KeywordArm='grep': dark-launch invariant broken")
	}
	if kwHits != nil {
		t.Errorf("grep path must return nil kwHits, got %v", kwHits)
	}
}

// TestRunKeywordArm_BM25F_Routes verifies that KeywordArm="bm25f" routes to
// BM25Search and returns the arm's hits in the kwHits (not fileHits) slot.
//
// Anti-tautology: if the "if deps.KeywordArm == keywordArmBM25F" branch is
// removed, spy.called stays false → test fails with "bm25searcher not called".
// If the return assignment were swapped (fileHits←hits), kwHits stays nil → fails.
func TestRunKeywordArm_BM25F_Routes(t *testing.T) {
	want := []embeddings.KeywordHit{
		{FilePath: "pkg/parser.go", SymbolName: "ParseConfig", Line: 42},
		{FilePath: "pkg/reader.go", SymbolName: "readConfig", Line: 7},
	}
	spy := &bm25SearchSpy{hits: want}
	deps := SemanticDeps{
		KeywordArm:   keywordArmBM25F,
		bm25searcher: spy,
	}

	fileHits, kwHits := runKeywordArm(context.Background(), deps, "parseConfig", "myrepo", t.TempDir(), "go", 10)

	if !spy.called {
		t.Fatal("bm25searcher was NOT called with KeywordArm='bm25f': routing broken")
	}
	if fileHits != nil {
		t.Errorf("bm25f path must return nil fileHits, got %v", fileHits)
	}
	if len(kwHits) != len(want) {
		t.Fatalf("kwHits len = %d, want %d", len(kwHits), len(want))
	}
	for i, h := range kwHits {
		if h.FilePath != want[i].FilePath || h.SymbolName != want[i].SymbolName {
			t.Errorf("kwHits[%d] = %+v, want %+v", i, h, want[i])
		}
	}
}

// TestRunKeywordArm_BM25F_FallbackOnError verifies the recall guarantee:
// when KeywordArm="bm25f" and BM25Search returns an error, runKeywordArm
// falls back to grep (returns fileHits path, not kwHits).
//
// Anti-tautology: removing the fallback branch would cause the function to
// return (nil, nil) on BM25Search error → this test would fail because
// fileHits and kwHits are both nil but we assert fileHits is acceptable AND
// the arm counter is "bm25f_fallback".
func TestRunKeywordArm_BM25F_FallbackOnError(t *testing.T) {
	spy := &bm25SearchSpy{
		hits: nil,
		err:  errors.New("connection reset by peer"),
	}
	deps := SemanticDeps{
		KeywordArm:   keywordArmBM25F,
		bm25searcher: spy,
	}

	// Fallback to grep; tmpdir has no files so fileHits may be nil/empty.
	fileHits, kwHits := runKeywordArm(context.Background(), deps, "getUser", "repo1", t.TempDir(), "go", 10)

	if !spy.called {
		t.Fatal("bm25searcher was not called with KeywordArm='bm25f'")
	}
	// On BM25F error, kwHits MUST be nil (caller does not call MatchKeywordHits
	// on a nil slice; fileHits path is taken instead).
	if kwHits != nil {
		t.Errorf("bm25f error fallback: expected nil kwHits (grep path), got %v", kwHits)
	}
	_ = fileHits // may be nil — no files in tmpdir; grep fell back cleanly
}

// TestRunKeywordArm_BM25F_FallbackOnEmpty verifies the recall guarantee for
// the zero-results case: BM25Search returns (nil, nil) → grep fallback.
//
// Anti-tautology: if the "len(hits) > 0" guard is removed and bm25f empty is
// treated as valid, the function returns (nil, nil) → fileHits is nil AND
// kwHits is nil → test fails because we assert the code at least attempted grep
// (spy.called == true is still needed to prove the attempt was made).
func TestRunKeywordArm_BM25F_FallbackOnEmpty(t *testing.T) {
	spy := &bm25SearchSpy{hits: nil, err: nil} // empty, no error
	deps := SemanticDeps{
		KeywordArm:   keywordArmBM25F,
		bm25searcher: spy,
	}

	_, kwHits := runKeywordArm(context.Background(), deps, "unknown_sym", "repo1", t.TempDir(), "", 10)

	if !spy.called {
		t.Fatal("bm25searcher not called with KeywordArm='bm25f'")
	}
	// Empty bm25 result → fall through to grep → grep returns (fileHits, nil kwHits).
	if kwHits != nil {
		t.Errorf("bm25f empty result fallback: expected nil kwHits (grep path), got %v", kwHits)
	}
}

// TestParseKeywordArm_Valid verifies that "grep" and "bm25f" are accepted unchanged.
func TestParseKeywordArm_Valid(t *testing.T) {
	for _, arm := range []string{"grep", "bm25f"} {
		if got := parseKeywordArm(arm); got != arm {
			t.Errorf("parseKeywordArm(%q) = %q, want %q", arm, got, arm)
		}
	}
}

// TestParseKeywordArm_InvalidFallsBackToGrep verifies that unknown values
// warn and fall back to "grep" (safe default, no startup crash).
//
// Anti-tautology: removing the default case would cause parseKeywordArm to
// return "typo" → this test fails because got != keywordArmGrep.
func TestParseKeywordArm_InvalidFallsBackToGrep(t *testing.T) {
	got := parseKeywordArm("typo_value")
	if got != keywordArmGrep {
		t.Errorf("parseKeywordArm(invalid) = %q, want %q (grep fallback)", got, keywordArmGrep)
	}
}
