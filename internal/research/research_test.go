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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	assert.Equal(t, "", RenderMap(nil, false, ""))
	assert.Equal(t, "", RenderMap([]scoredFile{}, false, ""))
}

// TestRenderMap_basic produces path + annotation line.
func TestRenderMap_basic(t *testing.T) {
	t.Parallel()
	files := []scoredFile{
		{
			expand:  expandResult{relPath: "internal/foo/bar.go", distance: 0, whyLinked: "seed"},
			symbols: makeSymbols("Foo"),
		},
		{
			expand:  expandResult{relPath: "internal/baz/qux.go", distance: 1, whyLinked: "imports seed"},
			symbols: makeSymbols("Qux"),
		},
	}
	out := RenderMap(files, false, "")
	assert.Contains(t, out, "internal/foo/bar.go")
	assert.Contains(t, out, "[seed]")
	assert.Contains(t, out, "internal/baz/qux.go")
	assert.Contains(t, out, "distance=1")
}

func TestRenderMap_stripsRoot(t *testing.T) {
	t.Parallel()
	files := []scoredFile{
		{
			expand:  expandResult{relPath: "/tmp/workspace/repo/pkg/foo.go", distance: 0, whyLinked: "seed"},
			symbols: makeSymbols("Foo"),
		},
	}
	out := RenderMap(files, false, "/tmp/workspace/repo")
	assert.Contains(t, out, "pkg/foo.go")
	assert.NotContains(t, out, "/tmp/workspace")
}

func TestRenderMap_skipsEmptySymbols(t *testing.T) {
	t.Parallel()
	files := []scoredFile{
		{
			expand:  expandResult{relPath: "a.go", distance: 0, whyLinked: "seed"},
			symbols: makeSymbols("Foo"),
		},
		{
			expand: expandResult{relPath: "empty.go", distance: 0, whyLinked: "seed"},
			// no symbols
		},
	}
	out := RenderMap(files, false, "")
	assert.Contains(t, out, "a.go")
	assert.NotContains(t, out, "empty.go")
}

// TestFilterSymbolsByQuery matches on name substring.
func TestFilterSymbolsByQuery(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	syms := makeSymbols("A", "B", "C")
	assert.Equal(t, syms, filterSymbolsByQuery(syms, nil))
}

func TestEstimateTokensRespectsIncludeBody(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestPruneAppliesMMRDiversity(t *testing.T) {
	t.Parallel()
	// Three files: two near-duplicates (foo, foo2) and one unique (bar).
	// Budget fits exactly 2. MMR should pick foo + bar (not foo + foo2)
	// because foo2 is ~100% Jaccard-similar to foo.
	mkSym := func(names ...string) []*parser.Symbol {
		out := make([]*parser.Symbol, 0, len(names))
		for _, n := range names {
			out = append(out, &parser.Symbol{Name: n, Kind: parser.KindFunction})
		}
		return out
	}
	expanded := []expandResult{
		{relPath: "foo.go", distance: 0},
		{relPath: "foo2.go", distance: 0},
		{relPath: "bar.go", distance: 0},
	}
	scores := map[string]float64{
		"foo.go":  0.95,
		"foo2.go": 0.93,
		"bar.go":  0.50,
	}
	syms := map[string][]*parser.Symbol{
		"foo.go":  mkSym("Retry", "Backoff", "Wait"),
		"foo2.go": mkSym("Retry", "Backoff", "Wait"), // identical names
		"bar.go":  mkSym("ParseURL", "Encode"),
	}
	// Budget sized so only ~2 files fit.
	// Each file cost: mapOverheadCharsPerFile(80) + sum(name_len+30) / charsPerToken(4)
	// foo.go: (80+35+36+34)/4 = 46 tokens; bar.go: (80+38+36)/4 = 38 tokens; total=84
	// foo2.go identical cost to foo.go=46; 46+46+38=130 > 90, so budget=90 fits exactly 2.
	kept, _ := pruneToTokenBudget(expanded, scores, syms, 90, false)

	got := map[string]bool{}
	for _, sf := range kept {
		got[sf.expand.relPath] = true
	}
	if !got["foo.go"] {
		t.Errorf("MMR should pick foo.go (highest score), got %v", got)
	}
	if !got["bar.go"] {
		t.Errorf("MMR should pick bar.go (diverse), got %v", got)
	}
	if got["foo2.go"] {
		t.Errorf("MMR should drop near-duplicate foo2.go, got %v", got)
	}
}

func TestJaccardBasic(t *testing.T) {
	t.Parallel()
	a := map[string]bool{"x": true, "y": true, "z": true}
	b := map[string]bool{"x": true, "y": true, "z": true}
	if got := jaccard(a, b); got != 1.0 {
		t.Errorf("identical sets: got %f, want 1.0", got)
	}
	c := map[string]bool{"p": true, "q": true}
	if got := jaccard(a, c); got != 0.0 {
		t.Errorf("disjoint sets: got %f, want 0.0", got)
	}
	d := map[string]bool{"x": true, "y": true, "w": true}
	// {x,y,z} ∩ {x,y,w} = {x,y} → 2; union = {x,y,z,w} → 4; jaccard = 0.5
	if got := jaccard(a, d); got != 0.5 {
		t.Errorf("half overlap: got %f, want 0.5", got)
	}
	if got := jaccard(nil, a); got != 0.0 {
		t.Errorf("nil input: got %f, want 0.0", got)
	}
}

func TestTestCandidatesPython(t *testing.T) {
	t.Parallel()
	got := testCandidates("pkg/foo.py")
	want := map[string]bool{"pkg/test_foo.py": true, "pkg/tests/test_foo.py": true}
	for _, c := range got {
		delete(want, c)
	}
	if len(want) > 0 {
		t.Errorf("missing Python test candidates: %v", want)
	}
}

func TestTestCandidatesSvelte(t *testing.T) {
	t.Parallel()
	cases := []struct {
		prod string
		want []string
	}{
		{
			prod: "Button.svelte",
			want: []string{"Button.test.svelte", "Button.spec.svelte", "__tests__/Button.test.svelte", "__tests__/Button.test.ts"},
		},
		{
			prod: "src/components/Modal.svelte",
			want: []string{
				"src/components/Modal.test.svelte",
				"src/components/Modal.spec.svelte",
				"src/components/__tests__/Modal.test.svelte",
				"src/components/__tests__/Modal.test.ts",
			},
		},
	}
	for _, tc := range cases {
		got := testCandidates(tc.prod)
		gotSet := make(map[string]bool, len(got))
		for _, c := range got {
			gotSet[c] = true
		}
		for _, w := range tc.want {
			if !gotSet[w] {
				t.Errorf("testCandidates(%q): missing %q; got %v", tc.prod, w, got)
			}
		}
	}
}

func TestTestCandidatesAstro(t *testing.T) {
	t.Parallel()
	cases := []struct {
		prod string
		want []string
	}{
		{
			prod: "Layout.astro",
			want: []string{"Layout.test.astro", "Layout.spec.astro", "__tests__/Layout.test.astro"},
		},
		{
			prod: "src/pages/Index.astro",
			want: []string{
				"src/pages/Index.test.astro",
				"src/pages/Index.spec.astro",
				"src/pages/__tests__/Index.test.astro",
			},
		},
	}
	for _, tc := range cases {
		got := testCandidates(tc.prod)
		gotSet := make(map[string]bool, len(got))
		for _, c := range got {
			gotSet[c] = true
		}
		for _, w := range tc.want {
			if !gotSet[w] {
				t.Errorf("testCandidates(%q): missing %q; got %v", tc.prod, w, got)
			}
		}
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
	t.Parallel()
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

func TestSemanticTopKScales(t *testing.T) {
	t.Parallel()
	cases := []struct {
		maxTokens, want int
	}{
		{1000, 10},   // 1000/400 = 2.5 → floor 10
		{4000, 10},   // 4000/400 = 10 → exactly floor
		{8000, 20},   // 8000/400 = 20
		{20000, 50},  // 20000/400 = 50
		{60000, 100}, // ceiling
		{0, 20},      // default: DefaultMaxTokens=8000 → 20
	}
	for _, c := range cases {
		got := semanticTopK(c.maxTokens)
		if got != c.want {
			t.Errorf("semanticTopK(%d) = %d, want %d", c.maxTokens, got, c.want)
		}
	}
}

func TestFilterSymbolsByQueryMatchesDocComment(t *testing.T) {
	t.Parallel()
	syms := []*parser.Symbol{
		{Name: "Backoff", Kind: parser.KindFunction, DocComment: "implements exponential retry backoff"},
		{Name: "Encode", Kind: parser.KindFunction, DocComment: "URL encoding helper"},
	}
	// Query term "retry" only matches the doc-comment of Backoff — not its name.
	matched := filterSymbolsByQuery(syms, []string{"retry"})
	if len(matched) != 1 {
		t.Fatalf("expected exactly 1 match, got %d: %+v", len(matched), matched)
	}
	if matched[0].Name != "Backoff" {
		t.Errorf("expected Backoff, got %s", matched[0].Name)
	}
}

func TestFilterSymbolsByQueryStillMatchesName(t *testing.T) {
	t.Parallel()
	// Regression: name matching must still work when doc is empty or non-matching.
	syms := []*parser.Symbol{
		{Name: "RetryWithBackoff", Kind: parser.KindFunction},
		{Name: "Helper", Kind: parser.KindFunction, DocComment: "unrelated"},
	}
	matched := filterSymbolsByQuery(syms, []string{"retry"})
	if len(matched) != 1 || matched[0].Name != "RetryWithBackoff" {
		t.Errorf("expected RetryWithBackoff, got %+v", matched)
	}
}
