package research

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandFromSeeds_downward verifies that files imported by seeds are found.
func TestExpandFromSeeds_downward(t *testing.T) {
	// seed: a.go imports b.go, b.go imports c.go
	importGraph := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
	}
	seeds := map[string]bool{"a.go": true}

	results := expandFromSeeds(seeds, importGraph, 2)

	byPath := make(map[string]expandResult)
	for _, r := range results {
		byPath[r.relPath] = r
	}

	assert.Equal(t, 0, byPath["a.go"].distance, "seed should be distance 0")
	assert.Equal(t, 1, byPath["b.go"].distance, "b.go is 1 hop from seed")
	assert.Equal(t, 2, byPath["c.go"].distance, "c.go is 2 hops from seed")
}

// TestExpandFromSeeds_upward verifies importers of seeds are found.
func TestExpandFromSeeds_upward(t *testing.T) {
	// x.go imports seed, y.go imports seed
	importGraph := map[string][]string{
		"x.go": {"seed.go"},
		"y.go": {"seed.go"},
	}
	seeds := map[string]bool{"seed.go": true}

	results := expandFromSeeds(seeds, importGraph, 1)

	byPath := make(map[string]expandResult)
	for _, r := range results {
		byPath[r.relPath] = r
	}

	assert.Equal(t, 0, byPath["seed.go"].distance)
	assert.Equal(t, 1, byPath["x.go"].distance, "x.go imports seed, should be found")
	assert.Equal(t, 1, byPath["y.go"].distance, "y.go imports seed, should be found")
}

// TestExpandFromSeeds_maxHops limits traversal depth.
func TestExpandFromSeeds_maxHops(t *testing.T) {
	// chain: seed → a → b → c → d
	importGraph := map[string][]string{
		"seed.go": {"a.go"},
		"a.go":    {"b.go"},
		"b.go":    {"c.go"},
		"c.go":    {"d.go"},
	}
	seeds := map[string]bool{"seed.go": true}

	results := expandFromSeeds(seeds, importGraph, 2)

	byPath := make(map[string]expandResult)
	for _, r := range results {
		byPath[r.relPath] = r
	}

	assert.Contains(t, byPath, "seed.go")
	assert.Contains(t, byPath, "a.go")
	assert.Contains(t, byPath, "b.go")
	assert.NotContains(t, byPath, "c.go", "3 hops away, beyond maxHops=2")
	assert.NotContains(t, byPath, "d.go", "4 hops away")
}

// TestPruneToTokenBudget verifies files are selected within budget.
func TestPruneToTokenBudget(t *testing.T) {
	expanded := []expandResult{
		{relPath: "high.go", distance: 0, whyLinked: "seed"},
		{relPath: "mid.go", distance: 1, whyLinked: "imports seed"},
		{relPath: "low.go", distance: 2, whyLinked: "imports mid"},
	}
	seedScores := map[string]float64{
		"high.go": 1.0,
		"mid.go":  0.0,
		"low.go":  0.0,
	}

	kept, pruned := pruneToTokenBudget(expanded, seedScores, nil, 10000, false)
	require.NotEmpty(t, kept)
	_ = pruned

	// high.go should be first (highest score)
	assert.Equal(t, "high.go", kept[0].expand.relPath)
}

// TestPruneToTokenBudget_budget enforces token limit.
func TestPruneToTokenBudget_budget(t *testing.T) {
	// 100 files, tiny budget — should keep only a few
	expanded := make([]expandResult, 100)
	scores := make(map[string]float64, 100)
	for i := range expanded {
		p := "file_" + string(rune('a'+i%26))
		expanded[i] = expandResult{relPath: p, distance: 0, whyLinked: "seed"}
		scores[p] = 1.0
	}

	kept, pruned := pruneToTokenBudget(expanded, scores, nil, 50, false) // tiny budget
	assert.True(t, len(kept) < 100, "should prune many files")
	assert.True(t, pruned > 0, "should report pruned count")
}

// TestRenderMap_empty returns empty string for no files.
func TestRenderMap_empty(t *testing.T) {
	assert.Equal(t, "", RenderMap(nil, false))
	assert.Equal(t, "", RenderMap([]scoredFile{}, false))
}

// TestRenderMap_basic produces path + annotation line.
func TestRenderMap_basic(t *testing.T) {
	files := []scoredFile{
		{
			expand: expandResult{relPath: "internal/foo/bar.go", distance: 0, whyLinked: "seed"},
		},
		{
			expand: expandResult{relPath: "internal/baz/qux.go", distance: 1, whyLinked: "imports seed"},
		},
	}
	out := RenderMap(files, false)
	assert.Contains(t, out, "internal/foo/bar.go")
	assert.Contains(t, out, "[seed]")
	assert.Contains(t, out, "internal/baz/qux.go")
	assert.Contains(t, out, "distance=1")
}

// TestFilterSymbolsByQuery matches on name substring.
func TestFilterSymbolsByQuery(t *testing.T) {
	syms := makeSymbols("RunDAG", "detectCycle", "helper", "dagColor")
	terms := []string{"dag"}

	matched := filterSymbolsByQuery(syms, terms)
	names := symbolNames(matched)

	assert.Contains(t, names, "RunDAG")
	assert.Contains(t, names, "dagColor")
	assert.NotContains(t, names, "helper")
}

// TestFilterSymbolsByQuery_noTerms returns all symbols when no terms given.
func TestFilterSymbolsByQuery_noTerms(t *testing.T) {
	syms := makeSymbols("A", "B", "C")
	assert.Equal(t, syms, filterSymbolsByQuery(syms, nil))
}

func TestEstimateTokensRespectsIncludeBody(t *testing.T) {
	syms := []*parser.Symbol{{
		Name:      "Foo",
		Kind:      parser.KindFunction,
		Signature: "func Foo(x int) error",
		Body:      strings.Repeat("body line\n", 50), // ~500 chars
	}}
	sf := scoredFile{
		expand:  expandResult{relPath: "foo.go"},
		symbols: syms,
	}

	withBody := estimateTokens(sf, true)
	withoutBody := estimateTokens(sf, false)

	if withBody <= withoutBody {
		t.Errorf("withBody (%d) must exceed withoutBody (%d)", withBody, withoutBody)
	}
	if withoutBody >= withBody/2 {
		t.Errorf("withoutBody=%d should be much smaller than withBody=%d", withoutBody, withBody)
	}
}

func TestRRFFusionIsRankBased(t *testing.T) {
	// foo.go is rank 1 in both input lists → must come out on top
	// after rank-based RRF, regardless of absolute score magnitudes.
	fused := map[string]float64{
		"foo.go": 0.9, // BM25 rank 1
		"bar.go": 0.5, // rank 2
		"baz.go": 0.1, // rank 3
	}
	semantic := map[string]float64{
		"foo.go": 0.95, // semantic rank 1
		"qux.go": 0.6,  // rank 2
	}

	merged := fuseScores(fused, semantic)

	// foo.go: 1/(60+1) + 1/(60+1) ≈ 0.0328 — must be highest.
	// bar.go: 1/(60+2) ≈ 0.0161 — only in BM25 list at rank 2.
	// qux.go: 1/(60+2) ≈ 0.0161 — only in semantic list at rank 2.
	if merged["foo.go"] <= merged["bar.go"] {
		t.Errorf("foo.go (rank 1 in both) must beat bar.go, got foo=%f bar=%f",
			merged["foo.go"], merged["bar.go"])
	}
	if merged["foo.go"] <= merged["qux.go"] {
		t.Errorf("foo.go must beat qux.go, got foo=%f qux=%f",
			merged["foo.go"], merged["qux.go"])
	}
	// bar and qux are both at rank 2 in their own list → approximately equal.
	diff := merged["bar.go"] - merged["qux.go"]
	if diff < -0.0001 || diff > 0.0001 {
		t.Errorf("bar.go and qux.go should score approximately equal, got bar=%f qux=%f",
			merged["bar.go"], merged["qux.go"])
	}
}

func TestFuseScoresIgnoresAbsoluteMagnitudes(t *testing.T) {
	// Prove the fusion is rank-based, not score-based, by using wildly
	// different score magnitudes that share the same rank ordering.
	smallScores := map[string]float64{"a": 0.001, "b": 0.0005}
	largeScores := map[string]float64{"a": 1000, "b": 500}

	small := fuseScores(smallScores, nil)
	large := fuseScores(largeScores, nil)

	if small["a"] != large["a"] || small["b"] != large["b"] {
		t.Errorf("rank-based RRF must ignore magnitudes: small=%v large=%v", small, large)
	}
}

func TestInputAcceptsFileGlob(t *testing.T) {
	in := Input{
		Root:     "/tmp/x",
		Query:    "Foo",
		FileGlob: "internal/**",
	}
	if in.FileGlob != "internal/**" {
		t.Errorf("FileGlob field missing or wrong value")
	}
}

func TestRunFiltersByFileGlob(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "internal/foo/foo.go"), "package foo\nfunc Foo() {}\n")
	mustWriteFile(t, filepath.Join(tmp, "cmd/main.go"), "package main\nfunc Foo() {}\n")

	res, err := Run(context.Background(), Input{
		Root:     tmp,
		Query:    "Foo",
		FileGlob: "internal/**",
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Seeds {
		if strings.HasPrefix(s.File, "cmd/") {
			t.Errorf("FileGlob 'internal/**' should have excluded %s", s.File)
		}
	}
}

func TestRunExcludesTestFilesByDefault(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "foo.go"), "package foo\nfunc Foo() {}\n")
	mustWriteFile(t, filepath.Join(tmp, "foo_test.go"), "package foo\nimport \"testing\"\nfunc TestFoo(t *testing.T) {}\n")

	res, err := Run(context.Background(), Input{Root: tmp, Query: "Foo"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Seeds {
		if strings.HasSuffix(s.File, "_test.go") {
			t.Errorf("test files must be excluded by default, got %s", s.File)
		}
	}
}

func TestRunIncludesTestsWhenAsked(t *testing.T) {
	tmp := t.TempDir()
	// foo.go has no reference to "UniqueTestHelper" — the test file is the sole match.
	mustWriteFile(t, filepath.Join(tmp, "foo.go"), "package foo\nfunc Foo() {}\n")
	mustWriteFile(t, filepath.Join(tmp, "foo_test.go"), "package foo\nimport \"testing\"\nfunc UniqueTestHelper(t *testing.T) {}\n")

	res, err := Run(context.Background(), Input{Root: tmp, Query: "UniqueTestHelper", IncludeTests: true}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range res.Seeds {
		if strings.HasSuffix(s.File, "_test.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected foo_test.go in seeds when IncludeTests=true, got %+v", res.Seeds)
	}
}

func TestLinkTestFilesAttachesSiblings(t *testing.T) {
	allFiles := map[string]bool{
		"foo.go":      true,
		"foo_test.go": true,
		"bar.go":      true,
		// no bar_test.go on purpose
	}
	kept := []scoredFile{
		{expand: expandResult{relPath: "foo.go", distance: 0, whyLinked: "seed"}, seedScore: 1.0},
		{expand: expandResult{relPath: "bar.go", distance: 1, whyLinked: "imports foo"}, seedScore: 0.5},
	}
	out := linkTestFiles(kept, allFiles)

	got := map[string]bool{}
	for _, sf := range out {
		got[sf.expand.relPath] = true
	}
	if !got["foo_test.go"] {
		t.Error("foo_test.go must be linked next to foo.go")
	}
	if got["bar_test.go"] {
		t.Error("bar_test.go does not exist on disk; must not appear")
	}
	// Originals still present.
	if !got["foo.go"] || !got["bar.go"] {
		t.Errorf("original kept files must remain, got %v", got)
	}
}

func TestLinkTestFilesSkipsExistingTestFiles(t *testing.T) {
	// When a kept file is itself a test file, don't try to link a sibling.
	allFiles := map[string]bool{
		"foo_test.go": true,
	}
	kept := []scoredFile{
		{expand: expandResult{relPath: "foo_test.go", distance: 0, whyLinked: "seed"}},
	}
	out := linkTestFiles(kept, allFiles)
	if len(out) != 1 {
		t.Errorf("expected exactly 1 file (no duplicate linking), got %d: %+v", len(out), out)
	}
}

func TestTestCandidatesPython(t *testing.T) {
	got := testCandidates("pkg/foo.py")
	want := map[string]bool{"pkg/test_foo.py": true, "pkg/tests/test_foo.py": true}
	for _, c := range got {
		delete(want, c)
	}
	if len(want) > 0 {
		t.Errorf("missing Python test candidates: %v", want)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeEmbedStore returns canned SearchResult slices for tests.
type fakeEmbedStore struct {
	results []embeddings.SearchResult
}

func (f *fakeEmbedStore) Search(_ context.Context, _ []float32, _ embeddings.SearchOpts) ([]embeddings.SearchResult, error) {
	return f.results, nil
}

// fakeEmbedClient returns a constant vector (content is irrelevant — fakeEmbedStore ignores it).
type fakeEmbedClient struct{}

func (fakeEmbedClient) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func TestRunPropagatesSemanticSymbols(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "foo.go"),
		"package foo\n"+
			"// RetryWithBackoff retries with exponential delay.\n"+
			"func RetryWithBackoff() {}\n"+
			"\n"+
			"// RetryOnce runs once.\n"+
			"func RetryOnce() {}\n",
	)

	store := &fakeEmbedStore{results: []embeddings.SearchResult{
		{FilePath: "foo.go", SymbolName: "RetryWithBackoff", SymbolKind: "function", StartLine: 2, Distance: 0.05},
		{FilePath: "foo.go", SymbolName: "RetryOnce", SymbolKind: "function", StartLine: 5, Distance: 0.10},
	}}

	res, err := Run(context.Background(), Input{
		Root:  tmp,
		Query: "retry",
	}, Deps{
		EmbedClient: fakeEmbedClient{},
		EmbedStore:  store,
		RepoKey:     "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]string{}
	for _, s := range res.Seeds {
		if s.File == "foo.go" {
			names[s.Name] = s.Source
		}
	}

	if _, ok := names["RetryWithBackoff"]; !ok {
		t.Errorf("RetryWithBackoff must be in seeds, got %+v", res.Seeds)
	}
	if _, ok := names["RetryOnce"]; !ok {
		t.Errorf("RetryOnce must be in seeds, got %+v", res.Seeds)
	}

	// Both should have Source in {"semantic", "hybrid"} — semantic hits must carry provenance.
	for name, source := range names {
		if source != "semantic" && source != "hybrid" {
			t.Errorf("symbol %q Source must be semantic or hybrid, got %q", name, source)
		}
	}
}
