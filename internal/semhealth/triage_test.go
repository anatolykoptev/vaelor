package semhealth

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// ---------------------------------------------------------------------------
// Fake store that satisfies the dupStore interface used by AnalyzeTriage.
// ---------------------------------------------------------------------------

type fakeDupStore struct {
	similarPairs  []embeddings.SimilarPair
	similarErr    error
	nearDupResult embeddings.NearDupResult
	nearDupErr    error
	exactPairs    []embeddings.ExactDupPair
	exactErr      error
}

func (f *fakeDupStore) FindSimilarPairs(_ context.Context, _ embeddings.SimilarPairOpts) ([]embeddings.SimilarPair, error) {
	if f.similarErr != nil {
		return nil, f.similarErr
	}
	return f.similarPairs, nil
}

func (f *fakeDupStore) FindNearDuplicates(_ context.Context, _ string, _ int, _ float32) (embeddings.NearDupResult, error) {
	if f.nearDupErr != nil {
		return embeddings.NearDupResult{}, f.nearDupErr
	}
	// If no explicit NearDupResult is set, derive it from similarPairs so
	// existing tests that set similarPairs keep working without changes.
	if len(f.nearDupResult.Pairs) == 0 && len(f.similarPairs) > 0 {
		return embeddings.NearDupResult{Pairs: f.similarPairs}, nil
	}
	return f.nearDupResult, nil
}

func (f *fakeDupStore) FindExactDuplicates(_ context.Context, _ string) ([]embeddings.ExactDupPair, error) {
	if f.exactErr != nil {
		return nil, f.exactErr
	}
	return f.exactPairs, nil
}

// ---------------------------------------------------------------------------
// Fake graph filter that records the input it received.
// ---------------------------------------------------------------------------

type captureGraphFilter struct {
	receivedLen int
}

func (c *captureGraphFilter) PairsConnectedByCalls(_ context.Context, _ string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
	c.receivedLen = len(pairs)
	return map[embeddings.PairKey]bool{}, nil
}

func (c *captureGraphFilter) PairsSharingInterface(_ context.Context, _ string, _ []embeddings.PairKey) (map[embeddings.PairKey]bool, error) {
	return map[embeddings.PairKey]bool{}, nil
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_NilGuards: nil store / empty repoKey / zero funcs → nil.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_NilGuards(t *testing.T) {
	store := &fakeDupStore{}
	t.Run("nil store returns nil", func(t *testing.T) {
		got := AnalyzeTriage(context.Background(), nil, nil, "g", "repo", 10, TriageOpts{})
		if got != nil {
			t.Errorf("nil store: want nil result, got %+v", got)
		}
	})
	t.Run("empty repoKey returns nil", func(t *testing.T) {
		got := AnalyzeTriage(context.Background(), store, nil, "g", "", 10, TriageOpts{})
		if got != nil {
			t.Errorf("empty repoKey: want nil result, got %+v", got)
		}
	})
	t.Run("zero totalFuncs returns nil", func(t *testing.T) {
		got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 0, TriageOpts{})
		if got != nil {
			t.Errorf("zero funcs: want nil result, got %+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_RepoSizeGuard: large repos short-circuit to empty result.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_RepoSizeGuard(t *testing.T) {
	store := &fakeDupStore{
		similarPairs: []embeddings.SimilarPair{
			{FileA: "a.go", SymbolA: "F", FileB: "b.go", SymbolB: "G", Similarity: 0.95},
		},
	}
	// totalFuncs > semhealthMaxFuncs (5000)
	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", semhealthMaxFuncs+1, TriageOpts{})
	if got == nil {
		t.Fatal("large repo: want &TriageResult{}, got nil")
	}
	if len(got.Groups) != 0 {
		t.Errorf("large repo: want 0 groups, got %d", len(got.Groups))
	}
	if got.Candidates != 0 {
		t.Errorf("large repo: want Candidates=0, got %d", got.Candidates)
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_TierAssignment: very-close vs related tiers.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_TierAssignment(t *testing.T) {
	// Pair above very-close threshold → "very-close"
	// Pair between related and very-close → "related"
	store := &fakeDupStore{
		similarPairs: []embeddings.SimilarPair{
			{
				FileA: "a.go", SymbolA: "Close", KindA: "function",
				FileB: "b.go", SymbolB: "CloseV2", KindB: "function",
				Similarity: 0.92, // ≥ tierVeryCloseSimilarity (0.88) → very-close
			},
			{
				FileA: "c.go", SymbolA: "Related", KindA: "function",
				FileB: "d.go", SymbolB: "RelatedV2", KindB: "function",
				Similarity: 0.83, // ≥ tierRelatedSimilarity (0.80), < very-close → related
			},
		},
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	if got.Candidates != 2 {
		t.Errorf("Candidates = %d, want 2", got.Candidates)
	}

	// Find groups by tier.
	tierCounts := make(map[string]int)
	for _, g := range got.Groups {
		tierCounts[g.Tier]++
	}
	if tierCounts["very-close"] == 0 {
		t.Errorf("expected at least one very-close group; groups = %+v", got.Groups)
	}
	if tierCounts["related"] == 0 {
		t.Errorf("expected at least one related group; groups = %+v", got.Groups)
	}
	// ReportedByTier should match.
	if got.ReportedByTier["very-close"] != tierCounts["very-close"] {
		t.Errorf("ReportedByTier[very-close] = %d, want %d", got.ReportedByTier["very-close"], tierCounts["very-close"])
	}
	if got.ReportedByTier["related"] != tierCounts["related"] {
		t.Errorf("ReportedByTier[related] = %d, want %d", got.ReportedByTier["related"], tierCounts["related"])
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_ExactGroupsFirst: exact groups must precede similar groups.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_ExactGroupsFirst(t *testing.T) {
	store := &fakeDupStore{
		exactPairs: []embeddings.ExactDupPair{
			{
				A: embeddings.SymbolRef{FilePath: "x.go", SymbolName: "ExactFunc", SymbolKind: "function"},
				B: embeddings.SymbolRef{FilePath: "y.go", SymbolName: "ExactFunc", SymbolKind: "function"},
			},
		},
		similarPairs: []embeddings.SimilarPair{
			{
				FileA: "a.go", SymbolA: "SimA", KindA: "function",
				FileB: "b.go", SymbolB: "SimB", KindB: "function",
				Similarity: 0.90,
			},
		},
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	if len(got.Groups) < 2 {
		t.Fatalf("want at least 2 groups (exact + similar), got %d", len(got.Groups))
	}
	if got.Groups[0].Tier != "exact" {
		t.Errorf("Groups[0].Tier = %q, want %q (exact groups must come first)", got.Groups[0].Tier, "exact")
	}
	// Verify ReportedByTier["exact"] is populated.
	if got.ReportedByTier["exact"] == 0 {
		t.Errorf("ReportedByTier[exact] = 0, want > 0")
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_DroppedMap: filter stats in Dropped map.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_DroppedMap(t *testing.T) {
	// One test pair (filterTests should drop it) + one valid very-close pair.
	store := &fakeDupStore{
		similarPairs: []embeddings.SimilarPair{
			// Test file endpoint → filterTests drops it.
			{
				FileA: "store_test.go", SymbolA: "TestFoo", KindA: "function",
				FileB: "b.go", SymbolB: "Foo", KindB: "function",
				Similarity: 0.91,
			},
			// Valid pair → kept.
			{
				FileA: "a.go", SymbolA: "Bar", KindA: "function",
				FileB: "b.go", SymbolB: "Baz", KindB: "function",
				Similarity: 0.89,
			},
		},
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	if got.Dropped["tests"] == 0 {
		t.Errorf("Dropped[tests] = 0, want > 0 (filterTests should have dropped the test pair)")
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_FilterOrder: graph filters receive the post-cheap-filter set.
// This verifies cheap filters (tests, same_file, kind) run before AGE filters.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_FilterOrder(t *testing.T) {
	// Three pairs:
	// 1. test file → dropped by filterTests (cheap)
	// 2. same file → dropped by filterSameFile (cheap)
	// 3. valid pair → reaches graph filter
	store := &fakeDupStore{
		similarPairs: []embeddings.SimilarPair{
			{
				FileA: "impl_test.go", SymbolA: "TestA", KindA: "function",
				FileB: "b.go", SymbolB: "A", KindB: "function",
				Similarity: 0.91,
			},
			{
				FileA: "pkg/foo.go", SymbolA: "Same", KindA: "function",
				FileB: "pkg/foo.go", SymbolB: "SameV2", KindB: "function",
				Similarity: 0.92,
			},
			{
				FileA: "real/a.go", SymbolA: "Real", KindA: "function",
				FileB: "real/b.go", SymbolB: "RealV2", KindB: "function",
				Similarity: 0.94,
			},
		},
	}
	capture := &captureGraphFilter{}

	got := AnalyzeTriage(context.Background(), store, capture, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	// Graph filter should have received only the 1 valid pair (not the 2 dropped ones).
	// Default IncludeSameFile=false → same-file pair is dropped before graph.
	if capture.receivedLen != 1 {
		t.Errorf("graph filter received %d pairs, want 1 (cheap filters should prune first)", capture.receivedLen)
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_ExactErrorDoesNotFailRun: exact-tier errors are swallowed.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_ExactErrorDoesNotFailRun(t *testing.T) {
	store := &fakeDupStore{
		exactErr: errors.New("exact query unavailable"),
		similarPairs: []embeddings.SimilarPair{
			{
				FileA: "a.go", SymbolA: "F", KindA: "function",
				FileB: "b.go", SymbolB: "G", KindB: "function",
				Similarity: 0.90,
			},
		},
	}

	// Must NOT return nil; should succeed with similar-tier groups.
	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("exact error should not fail the run; want non-nil TriageResult")
	}
	// Exact groups absent.
	for _, g := range got.Groups {
		if g.Tier == "exact" {
			t.Errorf("unexpected exact group despite exact error: %+v", g)
		}
	}
	// Similar groups present.
	if len(got.Groups) == 0 {
		t.Errorf("want at least one similar group when similar pairs are available")
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_KindPopulated: DupSymbol.Kind comes from pair's Kind fields.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_KindPopulated(t *testing.T) {
	store := &fakeDupStore{
		similarPairs: []embeddings.SimilarPair{
			{
				FileA: "a.go", SymbolA: "Handler", KindA: "method",
				FileB: "b.go", SymbolB: "Handle", KindB: "method",
				Similarity: 0.93,
			},
		},
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	if len(got.Groups) == 0 {
		t.Fatal("want at least one group")
	}
	for _, g := range got.Groups {
		for _, s := range g.Symbols {
			if s.Kind == "" {
				t.Errorf("DupSymbol.Kind is empty for %s in %s; want kind populated from pair", s.Name, s.File)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TestAnalyzeTriage_IncludeSameFile: opt-in same-file keeps pairs in graph scope.
// RED: AnalyzeTriage does not exist yet.
// ---------------------------------------------------------------------------

func TestAnalyzeTriage_IncludeSameFile(t *testing.T) {
	sameFilePair := embeddings.SimilarPair{
		FileA: "pkg/foo.go", SymbolA: "A", KindA: "function",
		FileB: "pkg/foo.go", SymbolB: "B", KindB: "function",
		Similarity: 0.95,
	}
	store := &fakeDupStore{
		similarPairs: []embeddings.SimilarPair{sameFilePair},
	}
	capture := &captureGraphFilter{}

	got := AnalyzeTriage(context.Background(), store, capture,
		"g", "repo", 100, TriageOpts{IncludeSameFile: true})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	// With IncludeSameFile=true the pair should survive to the graph filter.
	if capture.receivedLen != 1 {
		t.Errorf("graph filter received %d pairs with IncludeSameFile=true, want 1", capture.receivedLen)
	}
}

// ---------------------------------------------------------------------------
// Phase 5: TimedOut / SearchErrors propagation tests.
// RED: TriageResult.TimedOut does not exist yet; these tests must fail.
// ---------------------------------------------------------------------------

// TestAnalyzeTriage_TimedOutFalse_WhenNoErrors asserts TimedOut is false when
// FindNearDuplicates reports zero SearchErrors.
func TestAnalyzeTriage_TimedOutFalse_WhenNoErrors(t *testing.T) {
	store := &fakeDupStore{
		nearDupResult: embeddings.NearDupResult{
			Pairs: []embeddings.SimilarPair{
				{
					FileA: "a.go", SymbolA: "F", KindA: "function",
					FileB: "b.go", SymbolB: "G", KindB: "function",
					Similarity: 0.90,
				},
			},
			SearchErrors: 0,
		},
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	if got.TimedOut {
		t.Error("TimedOut should be false when SearchErrors == 0")
	}
}

// TestAnalyzeTriage_TimedOutTrue_WhenSearchErrors asserts TimedOut is true when
// FindNearDuplicates reports one or more SearchErrors (partial run).
func TestAnalyzeTriage_TimedOutTrue_WhenSearchErrors(t *testing.T) {
	store := &fakeDupStore{
		nearDupResult: embeddings.NearDupResult{
			Pairs:        nil,
			SearchErrors: 3, // 3 per-symbol searches failed
		},
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("want non-nil TriageResult")
	}
	if !got.TimedOut {
		t.Error("TimedOut should be true when NearDupResult.SearchErrors > 0")
	}
}

// TestAnalyzeTriage_FindNearDupFatalError asserts that when FindNearDuplicates
// returns a non-nil error (fatal bulk-load failure), AnalyzeTriage still returns
// a non-nil TriageResult (same contract as the old similar-pairs error path) and
// TimedOut is set.
func TestAnalyzeTriage_FindNearDupFatalError(t *testing.T) {
	store := &fakeDupStore{
		nearDupErr: errors.New("bulk load failed"),
	}

	got := AnalyzeTriage(context.Background(), store, nil, "g", "repo", 100, TriageOpts{})
	if got == nil {
		t.Fatal("fatal FindNearDuplicates error must not return nil (use empty TriageResult + TimedOut)")
	}
	if !got.TimedOut {
		t.Error("TimedOut should be true on fatal FindNearDuplicates error")
	}
	if len(got.Groups) != 0 {
		t.Errorf("groups should be empty on fatal error, got %d", len(got.Groups))
	}
}
