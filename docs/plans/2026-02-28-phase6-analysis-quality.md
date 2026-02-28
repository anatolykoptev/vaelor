# Phase 6: Analysis Quality — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve go-code's repo_analyze quality by adopting best practices from Aider, Sourcegraph, Repomix, and CodeMCP — better scoring, smarter context formatting, intent-aware prompts, and structured output.

**Architecture:** New `internal/ranking` package for BM25F + PageRank (pure logic, no deps on analyze). Modify `internal/analyze/context.go` for XML format, annotations, and query understanding. Modify `internal/render/render.go` for skeleton markers. Add intent classification to `internal/llm/`. Add response envelope to `cmd/go-code/tool_repo_analyze.go`.

**Tech Stack:** Go 1.24, tree-sitter (existing), math (BM25F/PageRank), XML-style tags (string formatting)

---

## Task 1: Query Understanding — camelCase/snake_case Splitting

**Why:** Current `extractQueryTerms()` only splits on whitespace. A query like "handleUserAuth" stays as one term and won't match `handle_user_auth` or files containing `User`, `Auth` separately. We need to split identifiers into subwords.

**Files:**
- Modify: `internal/analyze/context.go:331-345` (`extractQueryTerms`)
- Test: `internal/analyze/analyze_test.go`

**Step 1: Write the failing tests**

Add to `internal/analyze/analyze_test.go`:

```go
func TestExtractQueryTerms_CamelCase(t *testing.T) {
	terms := extractQueryTerms("handleUserAuth middleware")
	// Should split camelCase into subwords.
	want := map[string]bool{
		"handle": true, "user": true, "auth": true,
		"handleuserauth": true, "middleware": true,
	}
	got := make(map[string]bool)
	for _, term := range terms {
		got[term] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("missing term %q in %v", w, terms)
		}
	}
}

func TestExtractQueryTerms_SnakeCase(t *testing.T) {
	terms := extractQueryTerms("parse_file_content")
	want := map[string]bool{
		"parse": true, "file": true, "content": true,
		"parse_file_content": true,
	}
	got := make(map[string]bool)
	for _, term := range terms {
		got[term] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("missing term %q in %v", w, terms)
		}
	}
}

func TestExtractQueryTerms_MixedIdentifiers(t *testing.T) {
	terms := extractQueryTerms("What does BuildLLMContext do?")
	got := make(map[string]bool)
	for _, term := range terms {
		got[term] = true
	}
	// "BuildLLMContext" should split into "build", "llm", "context" + keep original
	for _, w := range []string{"build", "llm", "context", "buildllmcontext", "what", "does"} {
		if !got[w] {
			t.Errorf("missing term %q in %v", w, terms)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -run TestExtractQueryTerms_ -v`
Expected: FAIL — "missing term" errors because camelCase is not split

**Step 3: Implement camelCase/snake_case splitting**

Replace `extractQueryTerms` in `internal/analyze/context.go`:

```go
// splitIdentifier splits a camelCase or snake_case identifier into lowercase subwords.
// "handleUserAuth" → ["handle", "user", "auth"]
// "parse_file_content" → ["parse", "file", "content"]
// "BuildLLMContext" → ["build", "llm", "context"]
func splitIdentifier(s string) []string {
	// First split on underscores.
	parts := strings.Split(s, "_")
	var result []string
	for _, part := range parts {
		result = append(result, splitCamelCase(part)...)
	}
	return result
}

// splitCamelCase splits a camelCase string into lowercase parts.
// "handleUserAuth" → ["handle", "user", "auth"]
// "LLMClient" → ["llm", "client"]
func splitCamelCase(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	runes := []rune(strings.ToLower(s))
	origRunes := []rune(s)
	start := 0

	for i := 1; i < len(origRunes); i++ {
		// Split at lower→Upper boundary ("eU" in "handleUser")
		// or at Upper→Upper→Lower boundary ("LMC" → "LM" + "C..." in "LLMClient")
		if unicode.IsUpper(origRunes[i]) && (unicode.IsLower(origRunes[i-1]) ||
			(i+1 < len(origRunes) && unicode.IsLower(origRunes[i+1]) && unicode.IsUpper(origRunes[i-1]))) {
			part := string(runes[start:i])
			if len(part) >= 2 {
				parts = append(parts, part)
			}
			start = i
		}
	}
	// Last part.
	part := string(runes[start:])
	if len(part) >= 2 {
		parts = append(parts, part)
	}
	return parts
}

// extractQueryTerms splits the query into lowercase terms for matching.
// Splits words on whitespace, then further splits camelCase/snake_case identifiers
// into subwords. Keeps the original compound term alongside its parts.
func extractQueryTerms(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	seen := make(map[string]struct{})
	var terms []string

	addTerm := func(t string) {
		t = nonAlphanumRe.ReplaceAllString(t, "")
		if len(t) >= 3 && t != "" {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				terms = append(terms, t)
			}
		}
	}

	for _, w := range words {
		clean := nonAlphanumRe.ReplaceAllString(w, "")
		addTerm(clean)
	}

	// Re-process original (pre-lowercase) words for camelCase splitting.
	origWords := strings.Fields(query)
	for _, w := range origWords {
		clean := nonAlphanumRe.ReplaceAllString(w, "")
		if len(clean) < 3 {
			continue
		}
		subwords := splitIdentifier(clean)
		if len(subwords) > 1 {
			for _, sw := range subwords {
				addTerm(sw)
			}
		}
	}

	return terms
}
```

Note: add `"unicode"` to the imports at the top of the file.

**Step 4: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -run TestExtractQueryTerms -v`
Expected: PASS

**Step 5: Run all analyze tests to check no regressions**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/analyze/context.go internal/analyze/analyze_test.go
git commit -m "feat(analyze): add camelCase/snake_case splitting to query term extraction

Splits compound identifiers like handleUserAuth into [handle, user, auth]
for better file matching. Keeps the original compound term alongside parts."
```

---

## Task 2: BM25F File Scoring

**Why:** Current `scoreFile()` uses naive keyword matching (+100 per term match). BM25F (field-weighted BM25) uses TF-IDF with field weights — symbol names ×5, file path ×3, content ×1. Sourcegraph Zoekt reports +20% quality improvement.

**Files:**
- Create: `internal/ranking/bm25.go`
- Create: `internal/ranking/bm25_test.go`
- Modify: `internal/analyze/context.go:276-294` (`scoreFile`, `prioritizeFiles`)

**Step 1: Write the failing tests for BM25F**

Create `internal/ranking/bm25_test.go`:

```go
package ranking

import (
	"testing"
)

func TestBM25F_EmptyCorpus(t *testing.T) {
	scorer := NewBM25F(nil)
	score := scorer.Score("test", Document{})
	if score != 0 {
		t.Errorf("empty corpus should score 0, got %f", score)
	}
}

func TestBM25F_SingleDocument(t *testing.T) {
	docs := []Document{
		{Path: "internal/auth/handler.go", Symbols: []string{"HandleLogin", "HandleLogout"}, Content: "func HandleLogin"},
	}
	scorer := NewBM25F(docs)

	score := scorer.Score("handler", docs[0])
	if score <= 0 {
		t.Errorf("matching document should have positive score, got %f", score)
	}
}

func TestBM25F_SymbolWeightHigherThanContent(t *testing.T) {
	docs := []Document{
		{Path: "a.go", Symbols: []string{"Handler"}, Content: "unrelated content"},
		{Path: "b.go", Symbols: []string{"Unrelated"}, Content: "handler is mentioned in content"},
	}
	scorer := NewBM25F(docs)

	scoreA := scorer.Score("handler", docs[0])
	scoreB := scorer.Score("handler", docs[1])
	if scoreA <= scoreB {
		t.Errorf("symbol match (%f) should score higher than content match (%f)", scoreA, scoreB)
	}
}

func TestBM25F_PathMatchWeighted(t *testing.T) {
	docs := []Document{
		{Path: "internal/handler/main.go", Symbols: []string{"Run"}, Content: "runs stuff"},
		{Path: "internal/utils/main.go", Symbols: []string{"Run"}, Content: "runs stuff with handler"},
	}
	scorer := NewBM25F(docs)

	scoreA := scorer.Score("handler", docs[0])
	scoreB := scorer.Score("handler", docs[1])
	if scoreA <= scoreB {
		t.Errorf("path match (%f) should score higher than content-only match (%f)", scoreA, scoreB)
	}
}

func TestBM25F_MultipleTerms(t *testing.T) {
	docs := []Document{
		{Path: "auth.go", Symbols: []string{"Login", "Auth"}, Content: "authentication"},
		{Path: "util.go", Symbols: []string{"Helper"}, Content: "utility"},
	}
	scorer := NewBM25F(docs)

	scoreA := scorer.ScoreTerms([]string{"auth", "login"}, docs[0])
	scoreB := scorer.ScoreTerms([]string{"auth", "login"}, docs[1])
	if scoreA <= scoreB {
		t.Errorf("doc with both terms (%f) should score higher than doc with neither (%f)", scoreA, scoreB)
	}
}

func TestBM25F_IDF_CommonTermLowerScore(t *testing.T) {
	// "main" appears in all docs, "handler" only in one.
	docs := []Document{
		{Path: "a.go", Symbols: []string{"main", "Handler"}, Content: "main handler"},
		{Path: "b.go", Symbols: []string{"main"}, Content: "main func"},
		{Path: "c.go", Symbols: []string{"main"}, Content: "main entry"},
	}
	scorer := NewBM25F(docs)

	scoreMain := scorer.Score("main", docs[0])
	scoreHandler := scorer.Score("handler", docs[0])
	if scoreMain >= scoreHandler {
		t.Errorf("rare term 'handler' (%f) should score higher than common term 'main' (%f)", scoreHandler, scoreMain)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/ranking/ -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Implement BM25F scorer**

Create `internal/ranking/bm25.go`:

```go
// Package ranking provides file relevance scoring algorithms for LLM context.
package ranking

import (
	"math"
	"strings"
)

// Field weight constants for BM25F.
const (
	WeightSymbol  = 5.0 // symbol name matches are most valuable
	WeightPath    = 3.0 // file path matches are high value
	WeightContent = 1.0 // content matches are baseline

	bm25K1 = 1.2  // term frequency saturation
	bm25B  = 0.75 // length normalization
)

// Document represents a file for BM25F scoring.
type Document struct {
	Path    string   // relative file path
	Symbols []string // symbol names in the file
	Content string   // file content (or cleaned excerpt)
}

// BM25F implements field-weighted BM25 scoring.
type BM25F struct {
	docs   []Document
	avgLen float64 // average document length across all fields
	idf    map[string]float64
}

// NewBM25F creates a BM25F scorer from a corpus of documents.
// Precomputes IDF values and average document length.
func NewBM25F(docs []Document) *BM25F {
	if len(docs) == 0 {
		return &BM25F{}
	}

	// Compute average field length (combined).
	totalLen := 0
	for _, d := range docs {
		totalLen += fieldLen(d)
	}
	avgLen := float64(totalLen) / float64(len(docs))

	// Precompute IDF: how many docs contain each term.
	docFreq := make(map[string]int)
	for _, d := range docs {
		seen := make(map[string]struct{})
		for _, term := range allTerms(d) {
			lower := strings.ToLower(term)
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				docFreq[lower]++
			}
		}
	}

	n := float64(len(docs))
	idf := make(map[string]float64, len(docFreq))
	for term, df := range docFreq {
		// Standard BM25 IDF: log((N - df + 0.5) / (df + 0.5) + 1)
		idf[term] = math.Log((n-float64(df)+0.5)/(float64(df)+0.5) + 1)
	}

	return &BM25F{docs: docs, avgLen: avgLen, idf: idf}
}

// Score computes the BM25F score for a single query term against a document.
func (b *BM25F) Score(term string, doc Document) float64 {
	if b == nil || len(b.docs) == 0 {
		return 0
	}
	term = strings.ToLower(term)
	idf := b.idf[term]
	if idf == 0 {
		return 0
	}

	tf := b.weightedTF(term, doc)
	dl := float64(fieldLen(doc))
	numerator := tf * (bm25K1 + 1)
	denominator := tf + bm25K1*(1-bm25B+bm25B*dl/b.avgLen)

	return idf * numerator / denominator
}

// ScoreTerms computes the total BM25F score for multiple query terms.
func (b *BM25F) ScoreTerms(terms []string, doc Document) float64 {
	total := 0.0
	for _, term := range terms {
		total += b.Score(term, doc)
	}
	return total
}

// weightedTF computes the weighted term frequency across all fields.
func (b *BM25F) weightedTF(term string, doc Document) float64 {
	tf := 0.0

	// Symbol field: count how many symbol names contain the term.
	for _, sym := range doc.Symbols {
		if strings.Contains(strings.ToLower(sym), term) {
			tf += WeightSymbol
		}
	}

	// Path field: check if path contains the term.
	if strings.Contains(strings.ToLower(doc.Path), term) {
		tf += WeightPath
	}

	// Content field: count occurrences in content.
	content := strings.ToLower(doc.Content)
	count := strings.Count(content, term)
	tf += float64(count) * WeightContent

	return tf
}

// fieldLen returns a combined field length for normalization.
func fieldLen(doc Document) int {
	l := len(strings.Fields(doc.Content))
	l += len(doc.Symbols)
	l += len(strings.Split(doc.Path, "/"))
	return l
}

// allTerms extracts all words from a document across all fields.
func allTerms(doc Document) []string {
	var terms []string
	terms = append(terms, strings.Fields(doc.Content)...)
	terms = append(terms, doc.Symbols...)
	terms = append(terms, strings.Split(doc.Path, "/")...)
	return terms
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/ranking/ -v`
Expected: ALL PASS

**Step 5: Wire BM25F into context.go**

Modify `internal/analyze/context.go` — replace `scoreFile()` and update `prioritizeFiles()`:

```go
// In imports, add:
// "github.com/anatolykoptev/go-code/internal/ranking"

// Replace scoreFile and update prioritizeFiles:

func prioritizeFiles(files []*ingest.File, results []fileParseResult, queryTerms []string) []*ingest.File {
	importCounts := computeImportCounts(results)

	// Build BM25F documents from files.
	docs := make([]ranking.Document, len(files))
	fileSymbols := buildFileSymbolMap(results)
	for i, f := range files {
		docs[i] = ranking.Document{
			Path:    f.RelPath,
			Symbols: fileSymbols[f.RelPath],
		}
	}
	scorer := ranking.NewBM25F(docs)

	type scoredFile struct {
		file  *ingest.File
		score float64
	}

	scored := make([]scoredFile, 0, len(files))
	for i, f := range files {
		bm25Score := scorer.ScoreTerms(queryTerms, docs[i])
		// Add import connectivity bonus (preserved from original).
		importBonus := float64(importCounts[f.RelPath]) * 0.5
		scored = append(scored, scoredFile{file: f, score: bm25Score + importBonus})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	out := make([]*ingest.File, len(scored))
	for i, sf := range scored {
		out[i] = sf.file
	}
	return out
}

// buildFileSymbolMap returns symbol names per file RelPath.
func buildFileSymbolMap(results []fileParseResult) map[string][]string {
	m := make(map[string][]string)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		names := make([]string, 0, len(pr.result.Symbols))
		for _, sym := range pr.result.Symbols {
			names = append(names, sym.Name)
		}
		m[pr.file.RelPath] = names
	}
	return m
}
```

Remove the old `scoreFile()`, `computeSymbolCounts()` functions since they're replaced by BM25F.

**Step 6: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ ./internal/ranking/ -v`
Expected: ALL PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/ranking/bm25.go internal/ranking/bm25_test.go internal/analyze/context.go internal/analyze/analyze_test.go
git commit -m "feat(ranking): add BM25F field-weighted file scoring

Replace naive keyword matching with BM25F algorithm.
Symbol names weighted ×5, file paths ×3, content ×1.
IDF penalizes common terms across the corpus."
```

---

## Task 3: PageRank on Import Graph

**Why:** Files imported by many other files are architecturally important. PageRank propagates importance through the import graph, surfacing core files even if they don't match query terms directly.

**Files:**
- Create: `internal/ranking/pagerank.go`
- Create: `internal/ranking/pagerank_test.go`
- Modify: `internal/analyze/context.go` (`prioritizeFiles`)

**Step 1: Write the failing tests**

Create `internal/ranking/pagerank_test.go`:

```go
package ranking

import (
	"math"
	"testing"
)

func TestPageRank_EmptyGraph(t *testing.T) {
	ranks := PageRank(nil, 20, 0.85)
	if len(ranks) != 0 {
		t.Errorf("empty graph should return empty ranks, got %d", len(ranks))
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	graph := map[string][]string{
		"main.go": {},
	}
	ranks := PageRank(graph, 20, 0.85)
	if _, ok := ranks["main.go"]; !ok {
		t.Error("single node should have a rank")
	}
}

func TestPageRank_StarTopology(t *testing.T) {
	// All files import "core.go" — it should have the highest rank.
	graph := map[string][]string{
		"a.go":    {"core.go"},
		"b.go":    {"core.go"},
		"c.go":    {"core.go"},
		"core.go": {},
	}
	ranks := PageRank(graph, 20, 0.85)

	if ranks["core.go"] <= ranks["a.go"] {
		t.Errorf("core.go (%f) should rank higher than a.go (%f)", ranks["core.go"], ranks["a.go"])
	}
	if ranks["core.go"] <= ranks["b.go"] {
		t.Errorf("core.go (%f) should rank higher than b.go (%f)", ranks["core.go"], ranks["b.go"])
	}
}

func TestPageRank_ChainTopology(t *testing.T) {
	// a → b → c: rank should increase along the chain.
	graph := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
		"c.go": {},
	}
	ranks := PageRank(graph, 20, 0.85)

	if ranks["c.go"] <= ranks["b.go"] {
		t.Errorf("c.go (%f) should rank higher than b.go (%f)", ranks["c.go"], ranks["b.go"])
	}
	if ranks["b.go"] <= ranks["a.go"] {
		t.Errorf("b.go (%f) should rank higher than a.go (%f)", ranks["b.go"], ranks["a.go"])
	}
}

func TestPageRank_Convergence(t *testing.T) {
	graph := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
		"c.go": {"a.go"},
	}
	ranks := PageRank(graph, 100, 0.85)

	// In a cycle, all ranks should converge to roughly equal values.
	sum := 0.0
	for _, r := range ranks {
		sum += r
	}
	avg := sum / float64(len(ranks))
	for node, r := range ranks {
		if math.Abs(r-avg) > 0.01 {
			t.Errorf("node %s rank %f deviates from average %f in cycle graph", node, r, avg)
		}
	}
}

func TestPageRank_Normalized(t *testing.T) {
	graph := map[string][]string{
		"a.go": {"b.go", "c.go"},
		"b.go": {"c.go"},
		"c.go": {},
	}
	ranks := PageRank(graph, 20, 0.85)

	sum := 0.0
	for _, r := range ranks {
		sum += r
	}
	// Ranks should sum to approximately 1.0 (normalized).
	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("ranks should sum to ~1.0, got %f", sum)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/ranking/ -run TestPageRank -v`
Expected: FAIL — function doesn't exist

**Step 3: Implement PageRank**

Create `internal/ranking/pagerank.go`:

```go
package ranking

// PageRank computes PageRank scores for nodes in a directed graph.
// graph maps each node to the list of nodes it links TO (outgoing edges).
// iterations controls convergence (20 is typical), damping is usually 0.85.
// Returns normalized scores (sum ≈ 1.0).
func PageRank(graph map[string][]string, iterations int, damping float64) map[string]float64 {
	if len(graph) == 0 {
		return nil
	}

	// Collect all nodes (sources and targets).
	nodes := make(map[string]struct{})
	for src, targets := range graph {
		nodes[src] = struct{}{}
		for _, tgt := range targets {
			nodes[tgt] = struct{}{}
		}
	}

	n := float64(len(nodes))
	ranks := make(map[string]float64, len(nodes))
	for node := range nodes {
		ranks[node] = 1.0 / n
	}

	// Build reverse graph: who links TO each node.
	inbound := make(map[string][]string, len(nodes))
	outCount := make(map[string]int, len(nodes))
	for src, targets := range graph {
		outCount[src] = len(targets)
		for _, tgt := range targets {
			inbound[tgt] = append(inbound[tgt], src)
		}
	}

	// Iterative computation.
	for range iterations {
		newRanks := make(map[string]float64, len(nodes))
		for node := range nodes {
			sum := 0.0
			for _, src := range inbound[node] {
				if outCount[src] > 0 {
					sum += ranks[src] / float64(outCount[src])
				}
			}
			newRanks[node] = (1-damping)/n + damping*sum
		}
		ranks = newRanks
	}

	return ranks
}
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/ranking/ -v`
Expected: ALL PASS

**Step 5: Wire PageRank into prioritizeFiles**

In `internal/analyze/context.go`, update `prioritizeFiles` to combine BM25F + PageRank:

```go
func prioritizeFiles(files []*ingest.File, results []fileParseResult, queryTerms []string) []*ingest.File {
	// Build BM25F documents.
	fileSymbols := buildFileSymbolMap(results)
	docs := make([]ranking.Document, len(files))
	for i, f := range files {
		docs[i] = ranking.Document{
			Path:    f.RelPath,
			Symbols: fileSymbols[f.RelPath],
		}
	}
	scorer := ranking.NewBM25F(docs)

	// Build import graph for PageRank.
	prGraph := buildPageRankGraph(results)
	pageRanks := ranking.PageRank(prGraph, 20, 0.85)

	type scoredFile struct {
		file  *ingest.File
		score float64
	}

	scored := make([]scoredFile, 0, len(files))
	for i, f := range files {
		bm25Score := scorer.ScoreTerms(queryTerms, docs[i])
		prScore := pageRanks[f.RelPath] * 100 // normalize to same magnitude
		// Combined: 70% BM25F relevance + 30% PageRank importance.
		combined := bm25Score*0.7 + prScore*0.3
		scored = append(scored, scoredFile{file: f, score: combined})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	out := make([]*ingest.File, len(scored))
	for i, sf := range scored {
		out[i] = sf.file
	}
	return out
}

// buildPageRankGraph builds a file→file import graph for PageRank.
func buildPageRankGraph(results []fileParseResult) map[string][]string {
	// Build base-name → relPath lookup.
	baseToRel := make(map[string]string)
	for _, pr := range results {
		base := filepath.Base(pr.file.RelPath)
		baseToRel[base] = pr.file.RelPath
	}

	graph := make(map[string][]string)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		var targets []string
		for _, imp := range pr.result.Imports {
			base := filepath.Base(imp)
			if rel, ok := baseToRel[base]; ok {
				targets = append(targets, rel)
			}
		}
		graph[pr.file.RelPath] = targets
	}
	return graph
}
```

Remove the old `computeImportCounts` function.

**Step 6: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ ./internal/ranking/ -v`
Expected: ALL PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/ranking/pagerank.go internal/ranking/pagerank_test.go internal/analyze/context.go
git commit -m "feat(ranking): add PageRank for import graph importance scoring

Iterative PageRank (damping=0.85, 20 iterations) on file→file import graph.
Combined scoring: 70% BM25F relevance + 30% PageRank importance."
```

---

## Task 4: XML Prompt Format

**Why:** Current context uses `=== File: path ===` delimiters. Anthropic and Repomix recommend `<file path="x.go">` XML tags — they're more parseable by LLMs and allow embedding metadata attributes like score.

**Files:**
- Modify: `internal/analyze/context.go` (`buildLLMContext`, `formatFileBlock`, `buildSymbolSummary`)
- Test: `internal/analyze/analyze_test.go`

**Step 1: Write the failing tests**

Add to `internal/analyze/analyze_test.go`:

```go
func TestBuildLLMContext_XMLFormat(t *testing.T) {
	root := makeFixtureRepo(t)
	ir, err := ingest.IngestRepo(context.Background(), ingest.IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	results := parseFilesParallel(context.Background(), ir.Files, false, nil)
	ctx := buildLLMContext(ir, results, "test query", render.ModeDefault, "")

	// Should use XML-style tags instead of === File: ===
	assertNotContains(t, ctx, "=== File:")
	assertContains(t, ctx, "<file path=")
	assertContains(t, ctx, "</file>")

	// Should use <query> tag
	assertContains(t, ctx, "<query>")
	assertContains(t, ctx, "</query>")

	// Should use <file-tree> tag
	assertContains(t, ctx, "<file-tree>")
	assertContains(t, ctx, "</file-tree>")
}

func TestFormatFileBlock_XML(t *testing.T) {
	block := formatFileBlock("internal/auth.go", "package auth\n")
	assertContains(t, block, `<file path="internal/auth.go">`)
	assertContains(t, block, "</file>")
	assertContains(t, block, "package auth")
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -run TestBuildLLMContext_XML -v`
Expected: FAIL — still using `=== File:` format

**Step 3: Convert to XML format**

Update `buildLLMContext` in `internal/analyze/context.go`:

```go
func buildLLMContext(ir *ingest.IngestResult, results []fileParseResult, query string, renderMode render.Mode, depth string) string {
	var sb strings.Builder

	budget := budgetForDepth(depth)

	sb.WriteString("<query>\n")
	sb.WriteString(query)
	sb.WriteString("\n</query>\n\n")

	sb.WriteString("<file-tree>\n")
	sb.WriteString(ingest.RenderTree(ir.Files))
	sb.WriteString("\n</file-tree>\n\n")

	if section := buildPolyglotSection(ir.Files); section != "" {
		sb.WriteString(section)
	}

	symbolSection := buildSymbolSummary(results, budget.symbolSummary)
	sb.WriteString("<symbols>\n")
	sb.WriteString(symbolSection)
	sb.WriteString("</symbols>\n\n")

	if budget.depGraph > 0 {
		appendDepGraph(&sb, ir.Root, results, budget.depGraph)
	}

	queryTerms := extractQueryTerms(query)
	prioritized := prioritizeFiles(ir.Files, results, queryTerms)

	parseMap := make(map[string]*parser.ParseResult, len(results))
	for _, pr := range results {
		if pr.result != nil {
			parseMap[pr.file.Path] = pr.result
		}
	}

	appendFileContents(&sb, prioritized, budget.fileBudget(), renderMode, queryTerms, parseMap, budget.maxFileChars)

	return sb.String()
}
```

Update `formatFileBlock`:

```go
func formatFileBlock(relPath, content string) string {
	return fmt.Sprintf("<file path=%q>\n%s\n</file>\n\n", relPath, content)
}
```

Update `appendFileContents` header:

```go
// In appendFileContents, change the header line from:
//   sb.WriteString("## File Contents\n\n")
// to:
//   sb.WriteString("<files>\n\n")
// And add closing tag logic (or just keep it simple since each file self-closes).
```

**Step 4: Update TestBuildLLMContext_ContainsSections**

The existing test checks for `## Query`, `## Repository File Tree`, etc. Update it:

```go
func TestBuildLLMContext_ContainsSections(t *testing.T) {
	root := makeFixtureRepo(t)
	ir, err := ingest.IngestRepo(context.Background(), ingest.IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	results := parseFilesParallel(context.Background(), ir.Files, false, nil)
	ctx := buildLLMContext(ir, results, "test query", render.ModeDefault, "")

	for _, section := range []string{"<query>", "<file-tree>", "<symbols>", "<file path="} {
		if !strings.Contains(ctx, section) {
			t.Errorf("LLM context missing section %q", section)
		}
	}
}
```

**Step 5: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/analyze/context.go internal/analyze/analyze_test.go
git commit -m "feat(analyze): switch LLM context to XML tag format

Replace Markdown headers and === File: === delimiters with XML tags:
<query>, <file-tree>, <symbols>, <file path=\"...\">
Per Anthropic and Repomix recommendations for better LLM parsing."
```

---

## Task 5: Skeleton Markers

**Why:** Current skeleton mode uses `// ...` placeholder which is ambiguous — it could be mistaken for a comment. Aider uses `⋮...` (ellipsis with vertical dots) which is visually distinct. Adding `│` prefix to visible lines helps LLMs distinguish skeleton context from actual code.

**Files:**
- Modify: `internal/render/render.go:55` (`bodyPlaceholder`), `applyReplacements`
- Test: `internal/render/render_test.go`

**Step 1: Write the failing tests**

Add to `internal/render/render_test.go`:

```go
func TestRenderFile_Skeleton_EllipsisMarker(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: ModeSkeleton})

	// Should use ⋮... marker instead of // ...
	assertContains(t, result, "⋮...")
	assertNotContains(t, result, "    // ...")
}

func TestRenderFile_Skeleton_LinePrefix(t *testing.T) {
	result := RenderFile(sampleGoSource, sampleSymbols, Opts{Mode: ModeSkeleton})

	// Visible lines in skeleton should have │ prefix.
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if line == "" || strings.Contains(line, "⋮...") {
			continue
		}
		if !strings.HasPrefix(line, "│") {
			t.Errorf("visible line should have │ prefix, got: %q", line)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/render/ -run TestRenderFile_Skeleton_Ellipsis -v`
Expected: FAIL — still using `// ...`

**Step 3: Update skeleton markers**

In `internal/render/render.go`:

```go
// Change bodyPlaceholder:
const bodyPlaceholder = "    ⋮..."

// Update applyReplacements to add │ prefix to visible lines:
func applyReplacements(lines []string, replacements []replacement) string {
	ops := buildLineOps(lines, replacements)
	hasAnyReplacement := len(replacements) > 0

	var out strings.Builder
	for i, line := range lines {
		lineNum := i + 1
		if ops.skip[lineNum] {
			continue
		}
		if text, ok := ops.replaceWith[lineNum]; ok {
			if hasAnyReplacement {
				out.WriteString("│")
			}
			out.WriteString(text)
		} else {
			if hasAnyReplacement {
				out.WriteString("│")
			}
			out.WriteString(line)
		}
		out.WriteByte('\n')
		if text, ok := ops.insertAfter[lineNum]; ok {
			out.WriteString(text)
			out.WriteByte('\n')
		}
	}

	return out.String()
}
```

**Step 4: Update existing tests**

Update tests that check for `// ...` to check for `⋮...` instead. Update tests that don't expect `│` prefix.

The `TestRenderFile_Skeleton` test asserts `assertContains(t, result, "// ...")` — change to `assertContains(t, result, "⋮...")`.

The `TestRenderFile_NestedFunctions_Skeleton` test similarly.

**Step 5: Run all render tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/render/ -v`
Expected: ALL PASS

**Step 6: Run full test suite**

Run: `cd /path/to/repos/src/go-code && go test ./... 2>&1 | tail -30`
Expected: ALL PASS (analyze tests should still work since they don't check skeleton markers directly)

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/render/render.go internal/render/render_test.go
git commit -m "feat(render): use ⋮... ellipsis markers and │ line prefix in skeleton mode

Replace ambiguous // ... placeholder with visually distinct ⋮... marker.
Add │ prefix to visible lines to help LLMs distinguish context from code.
Inspired by Aider's repomap format."
```

---

## Task 6: Contextual File Annotations

**Why:** LLMs benefit from metadata about each file before reading its code. Annotations like "imported by 8 files", "handler layer", "score: 240" help the LLM prioritize attention.

**Files:**
- Modify: `internal/analyze/context.go` (`appendFileContents`)
- Test: `internal/analyze/analyze_test.go`

**Step 1: Write the failing tests**

Add to `internal/analyze/analyze_test.go`:

```go
func TestBuildLLMContext_FileAnnotations(t *testing.T) {
	root := makeFixtureRepo(t)
	ir, err := ingest.IngestRepo(context.Background(), ingest.IngestOpts{Root: root})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	results := parseFilesParallel(context.Background(), ir.Files, false, nil)
	ctx := buildLLMContext(ir, results, "Add function", render.ModeDefault, "")

	// File blocks should include annotation comments.
	assertContains(t, ctx, "<!-- ")
	assertContains(t, ctx, " symbols")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -run TestBuildLLMContext_FileAnnotations -v`
Expected: FAIL

**Step 3: Implement file annotations**

Update `appendFileContents` signature and logic in `internal/analyze/context.go`. We need to pass import counts and symbol counts so annotations can reference them:

```go
// fileAnnotation builds a short annotation comment for a file.
func fileAnnotation(relPath string, importCounts, symbolCounts map[string]int, language string) string {
	parts := []string{}
	if n := importCounts[relPath]; n > 0 {
		parts = append(parts, fmt.Sprintf("imported by %d files", n))
	}
	if n := symbolCounts[relPath]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d symbols", n))
	}
	if language != "" {
		parts = append(parts, language)
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("<!-- %s -->\n", strings.Join(parts, ", "))
}
```

Update `appendFileContents` to accept and use annotations:

```go
func appendFileContents(
	sb *strings.Builder,
	files []*ingest.File,
	budget int,
	renderMode render.Mode,
	queryTerms []string,
	parseMap map[string]*parser.ParseResult,
	maxFileChars int,
	importCounts, symbolCounts map[string]int,
) {
	// ... existing code ...
	for _, f := range files {
		// ... existing read/render/clean logic ...
		annotation := fileAnnotation(f.RelPath, importCounts, symbolCounts, f.Language)
		block := annotation + formatFileBlock(f.RelPath, cleaned)
		if remaining < len(block) {
			break
		}
		sb.WriteString(block)
		remaining -= len(block)
	}
}
```

Update `buildLLMContext` to pass the new parameters:

```go
// In buildLLMContext, before calling appendFileContents:
importCounts := computeImportCounts(results)
symbolCounts := computeSymbolCounts(results)

appendFileContents(&sb, prioritized, budget.fileBudget(), renderMode, queryTerms, parseMap, budget.maxFileChars, importCounts, symbolCounts)
```

Note: we need to bring back `computeImportCounts` and `computeSymbolCounts` as they are needed for annotations (they were used in the old scoring but may have been removed in Task 2 — if so, keep them for annotation use).

**Step 4: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/analyze/context.go internal/analyze/analyze_test.go
git commit -m "feat(analyze): add contextual annotations before each file in LLM context

Each file block is preceded by <!-- imported by N files, M symbols, lang -->
annotation to help the LLM prioritize attention."
```

---

## Task 7: Intent-Aware System Prompts

**Why:** Current system prompt is generic for all queries. "How is auth implemented?" (architecture) needs a different prompt than "Why does login fail?" (debugging). Intent classification selects a specialized prompt.

**Files:**
- Modify: `internal/llm/llm.go` (new intent constants, prompts, and classification function)
- Create: `internal/llm/intent.go`
- Create: `internal/llm/intent_test.go`
- Modify: `internal/analyze/analyze.go` (`AnalyzeRepo`)

**Step 1: Write the failing tests**

Create `internal/llm/intent_test.go`:

```go
package llm

import "testing"

func TestClassifyIntent_Architecture(t *testing.T) {
	tests := []struct {
		query string
		want  Intent
	}{
		{"How is the authentication system designed?", IntentArchitecture},
		{"Explain the overall architecture", IntentArchitecture},
		{"What patterns does this codebase use?", IntentArchitecture},
		{"Describe the module structure", IntentArchitecture},
	}
	for _, tc := range tests {
		got := ClassifyIntent(tc.query)
		if got != tc.want {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestClassifyIntent_Debug(t *testing.T) {
	tests := []struct {
		query string
		want  Intent
	}{
		{"Why does the login handler return 500?", IntentDebug},
		{"Find the bug in user validation", IntentDebug},
		{"What causes the nil pointer error?", IntentDebug},
		{"Fix the race condition in cache", IntentDebug},
	}
	for _, tc := range tests {
		got := ClassifyIntent(tc.query)
		if got != tc.want {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestClassifyIntent_Navigate(t *testing.T) {
	tests := []struct {
		query string
		want  Intent
	}{
		{"Where is HandleLogin defined?", IntentNavigate},
		{"Find the Config struct", IntentNavigate},
		{"Show me the middleware chain", IntentNavigate},
		{"What file contains the router setup?", IntentNavigate},
	}
	for _, tc := range tests {
		got := ClassifyIntent(tc.query)
		if got != tc.want {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestClassifyIntent_Dependency(t *testing.T) {
	tests := []struct {
		query string
		want  Intent
	}{
		{"What imports does the auth package have?", IntentDependency},
		{"Show dependency graph for internal/", IntentDependency},
		{"Which packages depend on util?", IntentDependency},
	}
	for _, tc := range tests {
		got := ClassifyIntent(tc.query)
		if got != tc.want {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestClassifyIntent_Default(t *testing.T) {
	tests := []struct {
		query string
		want  Intent
	}{
		{"Tell me about this repo", IntentGeneral},
		{"What does this code do?", IntentGeneral},
		{"", IntentGeneral},
	}
	for _, tc := range tests {
		got := ClassifyIntent(tc.query)
		if got != tc.want {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/llm/ -run TestClassifyIntent -v`
Expected: FAIL — type and function don't exist

**Step 3: Implement intent classification**

Create `internal/llm/intent.go`:

```go
package llm

import "strings"

// Intent represents the classified intent of a user query.
type Intent string

const (
	IntentArchitecture Intent = "architecture"
	IntentDebug        Intent = "debug"
	IntentNavigate     Intent = "navigate"
	IntentDependency   Intent = "dependency"
	IntentGeneral      Intent = "general"
)

// intentKeywords maps intents to their trigger keywords.
var intentKeywords = map[Intent][]string{
	IntentArchitecture: {
		"architecture", "design", "pattern", "structure", "module",
		"organized", "layered", "component", "overview", "approach",
		"designed", "architected",
	},
	IntentDebug: {
		"bug", "error", "fail", "crash", "panic", "nil pointer",
		"race condition", "fix", "broken", "wrong", "issue",
		"cause", "debug", "500", "404", "timeout",
	},
	IntentNavigate: {
		"where", "find", "locate", "defined", "definition",
		"show me", "which file", "what file", "contains",
		"look for", "search for",
	},
	IntentDependency: {
		"import", "depend", "dependency", "graph",
		"packages depend", "module depend", "coupling",
	},
}

// ClassifyIntent determines the user's intent from their query text.
// Uses keyword matching with scoring — the intent with the most keyword hits wins.
func ClassifyIntent(query string) Intent {
	if query == "" {
		return IntentGeneral
	}
	lower := strings.ToLower(query)

	bestIntent := IntentGeneral
	bestScore := 0

	for intent, keywords := range intentKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestIntent = intent
		}
	}

	return bestIntent
}

// SystemPromptForIntent returns a specialized system prompt for the given intent.
func SystemPromptForIntent(intent Intent, depth string) string {
	// Depth overrides still apply for overview/deep.
	if depth == "overview" {
		return SystemPromptOverview
	}
	if depth == "deep" {
		return SystemPromptDeep
	}

	switch intent {
	case IntentArchitecture:
		return systemPromptArchitecture
	case IntentDebug:
		return systemPromptDebug
	case IntentNavigate:
		return systemPromptNavigate
	case IntentDependency:
		return SystemPromptDepGraph
	default:
		return SystemPromptRepoAnalysis
	}
}

const systemPromptArchitecture = `You are a senior software architect analyzing a code repository.
Focus on: design patterns, architectural decisions, module boundaries, separation of concerns.
Explain the high-level structure first, then drill into key components.
Reference specific packages and their responsibilities.
Highlight strengths and potential improvements in the architecture.`

const systemPromptDebug = `You are a senior software engineer debugging a code issue.
Focus on: error handling paths, edge cases, race conditions, null/nil checks.
Trace the execution flow from the suspected entry point.
Identify potential root causes and suggest fixes with specific code references.
Prioritize the most likely cause first.`

const systemPromptNavigate = `You are a code navigator helping locate specific code elements.
Focus on: exact file paths, line numbers, function signatures.
Provide the precise location of the requested symbol or pattern.
Show how it connects to related code (callers, callees, types used).
Be direct — answer with locations first, context second.`
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/llm/ -v`
Expected: ALL PASS

**Step 5: Wire intent classification into AnalyzeRepo**

In `internal/analyze/analyze.go`, update `AnalyzeRepo`:

```go
// Replace:
//   systemPrompt := llm.SystemPromptForDepth(input.Depth)
// With:
	intent := llm.ClassifyIntent(input.Query)
	systemPrompt := llm.SystemPromptForIntent(intent, input.Depth)
```

**Step 6: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ ./internal/llm/ -v`
Expected: ALL PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/llm/intent.go internal/llm/intent_test.go internal/analyze/analyze.go
git commit -m "feat(llm): add intent-aware system prompt selection

Classify queries into architecture/debug/navigate/dependency/general intents
via keyword scoring. Each intent gets a specialized system prompt.
Depth overrides still apply for overview/deep modes."
```

---

## Task 8: Response Envelope

**Why:** Current output is plain text. A structured response envelope enables machine consumption by downstream agents. Fields: schemaVersion, data, meta (confidence, fileCount, truncated), suggestedNextCalls.

**Files:**
- Modify: `internal/analyze/analyze.go` (`RepoAnalysisResult`)
- Modify: `cmd/go-code/tool_repo_analyze.go` (`RepoAnalyzeInput`, `handleDeepMode`, `formatAnalysisResult`)
- Test: `cmd/go-code/tool_repo_analyze_test.go` (new)

**Step 1: Write the failing test**

Create `cmd/go-code/tool_repo_analyze_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestFormatAnalysisResult_TextMode(t *testing.T) {
	r := &analyze.RepoAnalysisResult{
		Answer:    "The repo does X.",
		RepoName:  "test-repo",
		Language:  "go",
		FileCount: 10,
		Packages:  []string{"cmd/", "internal/"},
	}
	text := formatAnalysisResult(r, "")
	if text == "" {
		t.Error("expected non-empty text output")
	}
	assertContains(t, text, "test-repo")
	assertContains(t, text, "The repo does X.")
}

func TestFormatAnalysisResult_JSONMode(t *testing.T) {
	r := &analyze.RepoAnalysisResult{
		Answer:    "The repo does X.",
		RepoName:  "test-repo",
		Language:  "go",
		FileCount: 10,
		Packages:  []string{"cmd/", "internal/"},
		Symbols: []*parser.Symbol{
			{Name: "main", Kind: parser.KindFunction, StartLine: 5},
		},
	}
	out := formatAnalysisResult(r, "json")
	// Should be valid JSON.
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	// Should have schema version.
	if _, ok := envelope["schemaVersion"]; !ok {
		t.Error("missing schemaVersion in envelope")
	}
	// Should have data.answer.
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatal("missing data in envelope")
	}
	if _, ok := data["answer"]; !ok {
		t.Error("missing data.answer in envelope")
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./cmd/go-code/ -run TestFormatAnalysisResult -v`
Expected: FAIL — `formatAnalysisResult` doesn't accept format parameter

**Step 3: Add format parameter and envelope**

Update `cmd/go-code/tool_repo_analyze.go`:

Add `Format` field to `RepoAnalyzeInput`:

```go
// Format controls output format: text (default) or json (structured envelope).
Format string `json:"format,omitempty" jsonschema_description:"Output format: text (default, human-readable) | json (structured envelope with schemaVersion, data, meta, suggestedNextCalls)"`
```

Add response envelope type and formatting:

```go
// responseEnvelope wraps analysis results in a structured JSON envelope.
type responseEnvelope struct {
	SchemaVersion    string              `json:"schemaVersion"`
	Data             envelopeData        `json:"data"`
	Meta             envelopeMeta        `json:"meta"`
	SuggestedNext    []suggestedCall     `json:"suggestedNextCalls,omitempty"`
}

type envelopeData struct {
	Answer    string   `json:"answer"`
	RepoName  string   `json:"repoName"`
	Language  string   `json:"language"`
	FileCount int      `json:"fileCount"`
	Packages  []string `json:"packages,omitempty"`
}

type envelopeMeta struct {
	FileCount int  `json:"filesAnalyzed"`
	Truncated bool `json:"truncated"`
}

type suggestedCall struct {
	Tool   string            `json:"tool"`
	Params map[string]string `json:"params"`
	Reason string            `json:"reason"`
}
```

Update `formatAnalysisResult` to accept format:

```go
func formatAnalysisResult(r *analyze.RepoAnalysisResult, format string) string {
	if format == "json" {
		return formatAnalysisJSON(r)
	}
	return formatAnalysisText(r)
}

func formatAnalysisJSON(r *analyze.RepoAnalysisResult) string {
	env := responseEnvelope{
		SchemaVersion: "1.0",
		Data: envelopeData{
			Answer:    r.Answer,
			RepoName:  r.RepoName,
			Language:  r.Language,
			FileCount: r.FileCount,
			Packages:  r.Packages,
		},
		Meta: envelopeMeta{
			FileCount: r.FileCount,
		},
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// formatAnalysisText is the existing text formatter (renamed from formatAnalysisResult).
func formatAnalysisText(r *analyze.RepoAnalysisResult) string {
	// ... existing implementation ...
}
```

Update `handleDeepMode` to pass format:

```go
return textResult(formatAnalysisResult(result, input.Format)), nil, nil
```

**Step 4: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./cmd/go-code/ -run TestFormatAnalysisResult -v`
Expected: ALL PASS

**Step 5: Run full test suite**

Run: `cd /path/to/repos/src/go-code && go test ./... 2>&1 | tail -30`
Expected: ALL PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add cmd/go-code/tool_repo_analyze.go cmd/go-code/tool_repo_analyze_test.go internal/analyze/analyze.go
git commit -m "feat(tool): add format=json structured response envelope for repo_analyze

New format parameter: text (default) or json (structured envelope).
JSON envelope includes schemaVersion, data, meta, and suggestedNextCalls.
Enables machine consumption by downstream MCP agents."
```

---

## Task 9: Lint & Final Integration

**Why:** Ensure all new code passes golangci-lint and the full test suite.

**Files:**
- All modified files

**Step 1: Run linter**

Run: `cd /path/to/repos/src/go-code && make lint`
Expected: PASS (fix any issues found)

**Step 2: Run full test suite**

Run: `cd /path/to/repos/src/go-code && make test`
Expected: ALL PASS

**Step 3: Update CLAUDE.md toolCount if needed**

Check if the `toolCount` constant in `cmd/go-code/main.go` needs updating (it shouldn't — no new tools added, just enhancements).

**Step 4: Update ROADMAP.md — mark Phase 6 items as complete**

Mark 6.1-6.6, 6.7, 6.9 as ✅ in `docs/ROADMAP.md`.

**Step 5: Final commit**

```bash
cd /path/to/repos/src/go-code
git add -A
git commit -m "chore: lint fixes and Phase 6 roadmap updates"
```

---

## Execution Notes

**Dependencies between tasks:**
- Task 1 (query understanding) should be done first — Tasks 2, 3 use its output
- Tasks 2, 3 (BM25F, PageRank) are independent of each other
- Task 4 (XML format) is independent
- Task 5 (skeleton markers) is independent
- Task 6 (annotations) depends on Task 4 (XML format)
- Task 7 (intent classification) is independent
- Task 8 (response envelope) is independent
- Task 9 (lint) must be last

**Parallel groups:**
- Group A (independent): Tasks 2, 5, 7
- Group B (independent): Tasks 4, 8
- Sequential: 1 → 3 → 6 → 9
