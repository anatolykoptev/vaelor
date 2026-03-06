# Semantic Search v2: Hybrid + Graph Expansion + Auto-Index

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `semantic_search` smarter by combining vector similarity with keyword matching, expanding results via the code graph (callers/callees), and pre-indexing local repos at startup.

**Architecture:** Three independent features layered onto the existing `semantic_search` tool. (1) Hybrid search: run `codesearch.Search` in parallel with `store.Search`, merge results using RRF (Reciprocal Rank Fusion). (2) Graph expansion: after getting semantic hits, query `code_embeddings` + `code_graph` (AGE) for 1-hop neighbors (CALLS edges) and append them. (3) Auto-index: on startup, scan `PATH_MAPPINGS` dirs for repos and call `Pipeline.IndexRepoAsync` for each.

**Tech Stack:** Go 1.26, pgvector, Apache AGE, `codesearch` package, `codegraph` package

**Key files reference:**
- `cmd/go-code/tool_semantic_search.go` — tool handler (170 lines)
- `internal/embeddings/store.go` — pgvector Store (195 lines)
- `internal/embeddings/pipeline.go` — indexing Pipeline (140 lines)
- `internal/embeddings/client.go` — embedding API client (114 lines)
- `internal/codesearch/` — keyword search engine (used by `code_search` tool)
- `internal/codegraph/query.go` — AGE graph queries (197 lines)
- `cmd/go-code/register.go` — tool registration, DB pool init (101 lines)
- `cmd/go-code/config.go` — env config (135 lines)

---

## Phase 1: Hybrid Search (semantic + keyword with RRF)

### Task 1: Add RRF merge function

**Files:**
- Create: `internal/embeddings/rrf.go`
- Create: `internal/embeddings/rrf_test.go`

**Step 1: Write the test**

```go
// internal/embeddings/rrf_test.go
package embeddings

import (
	"testing"
)

func TestRRFMerge(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "Foo", Distance: 0.1},
		{FilePath: "b.go", SymbolName: "Bar", Distance: 0.3},
		{FilePath: "c.go", SymbolName: "Baz", Distance: 0.5},
	}
	keyword := []KeywordHit{
		{FilePath: "b.go", SymbolName: "Bar", Line: 10},
		{FilePath: "d.go", SymbolName: "Qux", Line: 20},
	}
	merged := MergeRRF(semantic, keyword, 10)

	if len(merged) != 4 {
		t.Fatalf("expected 4 results, got %d", len(merged))
	}
	// Bar should be first — it appears in both lists.
	if merged[0].SymbolName != "Bar" {
		t.Errorf("expected Bar first (in both lists), got %s", merged[0].SymbolName)
	}
	// All results should have Source set.
	for _, r := range merged {
		if r.Source == "" {
			t.Errorf("result %s has empty Source", r.SymbolName)
		}
	}
}

func TestRRFMergeEmptySemantic(t *testing.T) {
	keyword := []KeywordHit{
		{FilePath: "a.go", SymbolName: "Foo", Line: 5},
	}
	merged := MergeRRF(nil, keyword, 10)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}
	if merged[0].Source != "keyword" {
		t.Errorf("expected source=keyword, got %s", merged[0].Source)
	}
}

func TestRRFMergeEmptyKeyword(t *testing.T) {
	semantic := []SearchResult{
		{FilePath: "a.go", SymbolName: "Foo", Distance: 0.1},
	}
	merged := MergeRRF(semantic, nil, 10)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}
	if merged[0].Source != "semantic" {
		t.Errorf("expected source=semantic, got %s", merged[0].Source)
	}
}

func TestRRFMergeTopK(t *testing.T) {
	semantic := make([]SearchResult, 20)
	for i := range semantic {
		semantic[i] = SearchResult{FilePath: "a.go", SymbolName: fmt.Sprintf("F%d", i), Distance: float32(i) * 0.01}
	}
	merged := MergeRRF(semantic, nil, 5)
	if len(merged) != 5 {
		t.Fatalf("expected 5 results, got %d", len(merged))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /path/to/repos/src/go-code && go test ./internal/embeddings/ -run TestRRF -v
```

Expected: FAIL — `MergeRRF` undefined, `KeywordHit` undefined, `Source` field missing.

**Step 3: Implement RRF merge**

```go
// internal/embeddings/rrf.go
package embeddings

import "sort"

const rrfK = 60 // standard RRF constant

// KeywordHit represents a grep/code_search match mapped to a symbol.
type KeywordHit struct {
	FilePath   string
	SymbolName string
	Line       int
}

// HybridResult extends SearchResult with source attribution.
type HybridResult struct {
	SearchResult
	Source string // "semantic", "keyword", or "hybrid"
}

// MergeRRF combines semantic and keyword results using Reciprocal Rank Fusion.
// Results appearing in both lists get boosted. Returns at most topK results.
func MergeRRF(semantic []SearchResult, keyword []KeywordHit, topK int) []HybridResult {
	scores := make(map[string]*HybridResult) // key = file:symbol

	for rank, r := range semantic {
		key := r.FilePath + ":" + r.SymbolName
		scores[key] = &HybridResult{SearchResult: r, Source: "semantic"}
		scores[key].rrf = 1.0 / float64(rrfK+rank+1)
	}

	for rank, h := range keyword {
		key := h.FilePath + ":" + h.SymbolName
		if existing, ok := scores[key]; ok {
			existing.rrf += 1.0 / float64(rrfK+rank+1)
			existing.Source = "hybrid"
		} else {
			scores[key] = &HybridResult{
				SearchResult: SearchResult{
					FilePath:   h.FilePath,
					SymbolName: h.SymbolName,
					StartLine:  h.Line,
				},
				Source: "keyword",
			}
			scores[key].rrf = 1.0 / float64(rrfK+rank+1)
		}
	}

	results := make([]HybridResult, 0, len(scores))
	for _, r := range scores {
		results = append(results, *r)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].rrf > results[j].rrf
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}
```

Wait — we can't add an unexported field `rrf` to `HybridResult` and use it in the sort. Let me use a cleaner approach with a local struct:

```go
// internal/embeddings/rrf.go
package embeddings

import "sort"

const rrfK = 60 // standard RRF constant

// KeywordHit represents a grep/code_search match mapped to a symbol.
type KeywordHit struct {
	FilePath   string
	SymbolName string
	Line       int
}

// HybridResult extends SearchResult with source attribution.
type HybridResult struct {
	SearchResult
	Source   string  // "semantic", "keyword", or "hybrid"
	RRFScore float64 // combined reciprocal rank fusion score
}

// MergeRRF combines semantic and keyword results using Reciprocal Rank Fusion.
// Results appearing in both lists get boosted. Returns at most topK results.
func MergeRRF(semantic []SearchResult, keyword []KeywordHit, topK int) []HybridResult {
	index := make(map[string]int) // key -> position in results slice
	var results []HybridResult

	for rank, r := range semantic {
		key := r.FilePath + ":" + r.SymbolName
		score := 1.0 / float64(rrfK+rank+1)
		index[key] = len(results)
		results = append(results, HybridResult{
			SearchResult: r,
			Source:        "semantic",
			RRFScore:      score,
		})
	}

	for rank, h := range keyword {
		key := h.FilePath + ":" + h.SymbolName
		score := 1.0 / float64(rrfK+rank+1)
		if idx, ok := index[key]; ok {
			results[idx].RRFScore += score
			results[idx].Source = "hybrid"
		} else {
			index[key] = len(results)
			results = append(results, HybridResult{
				SearchResult: SearchResult{
					FilePath:   h.FilePath,
					SymbolName: h.SymbolName,
					StartLine:  h.Line,
				},
				Source:   "keyword",
				RRFScore: score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	if len(results) > topK {
		results = results[:topK]
	}
	return results
}
```

**Step 4: Run test to verify it passes**

```bash
cd /path/to/repos/src/go-code && go test ./internal/embeddings/ -run TestRRF -v
```

Expected: PASS (all 4 tests).

**Step 5: Commit**

```bash
git add internal/embeddings/rrf.go internal/embeddings/rrf_test.go
git commit -m "feat(semantic): add RRF merge for hybrid search"
```

---

### Task 2: Add keyword-to-symbol mapping

Keyword search returns file:line matches. We need to map those to the nearest symbol in `code_embeddings` so RRF can merge them.

**Files:**
- Modify: `internal/embeddings/store.go`
- Create: `internal/embeddings/store_test.go` (for this specific method — needs DB, can be integration test)

**Step 1: Add `FindByFileLines` method to Store**

```go
// Add to internal/embeddings/store.go

// FindByFileLines finds indexed symbols whose start_line is closest to the given file:line pairs.
// Used to map keyword search hits back to indexed symbols for hybrid search.
func (s *Store) FindByFileLines(ctx context.Context, repoKey string, hits []FileLineHit) ([]SearchResult, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	// Build a single query: for each hit, find the symbol in the same file
	// with the closest start_line (within 50 lines above the match).
	var results []SearchResult
	seen := make(map[string]bool)

	for _, h := range hits {
		rows, err := s.pool.Query(ctx,
			`SELECT file_path, symbol_name, symbol_kind, language, start_line
			 FROM code_embeddings
			 WHERE repo_key=$1 AND file_path=$2 AND start_line <= $3
			 ORDER BY start_line DESC LIMIT 1`,
			repoKey, h.FilePath, h.Line)
		if err != nil {
			return nil, fmt.Errorf("find symbol for %s:%d: %w", h.FilePath, h.Line, err)
		}
		for rows.Next() {
			var r SearchResult
			if err := rows.Scan(&r.FilePath, &r.SymbolName, &r.SymbolKind, &r.Language, &r.StartLine); err != nil {
				rows.Close()
				return nil, err
			}
			key := r.FilePath + ":" + r.SymbolName
			if !seen[key] {
				seen[key] = true
				r.RepoKey = repoKey
				results = append(results, r)
			}
		}
		rows.Close()
	}
	return results, nil
}

// FileLineHit is a file path + line number from keyword search.
type FileLineHit struct {
	FilePath string
	Line     int
}
```

Actually, doing N queries is inefficient. Let's batch with a CTE or `ANY()`:

```go
// Better approach — single query with unnest.
// Add to internal/embeddings/store.go

// FileLineHit is a file path + line number from keyword search.
type FileLineHit struct {
	FilePath string
	Line     int
}

// MatchKeywordHits maps keyword search hits to the nearest indexed symbol.
// For each hit, finds the symbol in the same file with start_line <= hit.Line.
// Returns deduplicated results.
func (s *Store) MatchKeywordHits(ctx context.Context, repoKey string, hits []FileLineHit) ([]KeywordHit, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []KeywordHit

	for _, h := range hits {
		var name string
		err := s.pool.QueryRow(ctx,
			`SELECT symbol_name FROM code_embeddings
			 WHERE repo_key=$1 AND file_path=$2 AND start_line <= $3
			 ORDER BY start_line DESC LIMIT 1`,
			repoKey, h.FilePath, h.Line).Scan(&name)
		if err != nil {
			continue // no matching symbol — skip
		}
		key := h.FilePath + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, KeywordHit{
			FilePath:   h.FilePath,
			SymbolName: name,
			Line:       h.Line,
		})
	}
	return result, nil
}
```

**Step 2: Build and verify**

```bash
cd /path/to/repos/src/go-code && go build ./...
```

Expected: compiles clean.

**Step 3: Commit**

```bash
git add internal/embeddings/store.go
git commit -m "feat(semantic): add MatchKeywordHits for hybrid search"
```

---

### Task 3: Wire hybrid search into tool handler

**Files:**
- Modify: `cmd/go-code/tool_semantic_search.go`

**Step 1: Add `hybrid` parameter to input**

Add to `SemanticSearchInput`:

```go
Hybrid bool `json:"hybrid,omitempty" jsonschema_description:"Enable hybrid search: combine semantic similarity with keyword matching (default: true)"`
```

**Step 2: Update `handleSemanticSearch` to run keyword search in parallel**

After the existing `store.Search` call succeeds with results, add:

```go
// Hybrid: run keyword search in parallel, merge with RRF.
if len(results) > 0 && !input.noHybrid() {
	keyHits := runKeywordSearch(ctx, input, root, deps)
	if len(keyHits) > 0 {
		matched, _ := deps.Store.MatchKeywordHits(ctx, repoKey, keyHits)
		if len(matched) > 0 {
			hybrid := embeddings.MergeRRF(results, matched, topK)
			return textResult(formatHybridResults(input, hybrid)), nil
		}
	}
}
```

**Step 3: Add `runKeywordSearch` helper**

```go
func runKeywordSearch(ctx context.Context, input SemanticSearchInput, root string, deps SemanticDeps) []embeddings.FileLineHit {
	matches, err := codesearch.Search(ctx, codesearch.SearchInput{
		Root:          root,
		Pattern:       input.Query,
		IsRegex:       false,
		CaseSensitive: false,
		MaxResults:    50,
		ContextLines:  0,
	})
	if err != nil {
		return nil
	}
	hits := make([]embeddings.FileLineHit, len(matches))
	for i, m := range matches {
		hits[i] = embeddings.FileLineHit{FilePath: m.File, Line: m.Line}
	}
	return hits
}
```

Note: `m.File` from codesearch returns relative paths — verify this matches `code_embeddings.file_path` format. Both use paths relative to repo root.

**Step 4: Add `formatHybridResults`**

Same as `formatSemanticResults` but includes `source` attribute and uses `HybridResult`:

```go
func formatHybridResults(input SemanticSearchInput, results []embeddings.HybridResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"semantic_search\" mode=\"hybrid\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <repo>%s</repo>\n", escapeXML(input.Repo))
	fmt.Fprintf(&sb, "  <results count=\"%d\">\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&sb, "    <result rank=\"%d\" source=\"%s\" score=\"%.4f\">\n",
			i+1, r.Source, r.RRFScore)
		fmt.Fprintf(&sb, "      <file>%s</file>\n", escapeXML(r.FilePath))
		fmt.Fprintf(&sb, "      <symbol kind=\"%s\">%s</symbol>\n",
			escapeXML(r.SymbolKind), escapeXML(r.SymbolName))
		fmt.Fprintf(&sb, "      <line>%d</line>\n", r.StartLine)
		fmt.Fprintf(&sb, "      <language>%s</language>\n", escapeXML(r.Language))
		fmt.Fprintf(&sb, "    </result>\n")
	}
	sb.WriteString("  </results>\n</response>")
	return sb.String()
}
```

**Step 5: Add `noHybrid` helper to input**

```go
func (i SemanticSearchInput) noHybrid() bool {
	// Hybrid is opt-out: enabled by default unless explicitly set to false.
	// Since Go zero-value for bool is false, we use a different field name.
	return false // hybrid always enabled for now
}
```

Actually simpler: just always do hybrid. Remove the `Hybrid` field and `noHybrid()`. Keep it simple — hybrid is always on, no config needed.

**Step 6: Build**

```bash
cd /path/to/repos/src/go-code && go build ./...
```

**Step 7: Commit**

```bash
git add cmd/go-code/tool_semantic_search.go
git commit -m "feat(semantic): wire hybrid search with RRF merge"
```

---

## Phase 2: Graph Expansion (1-hop callers/callees)

### Task 4: Add graph neighbor lookup to Store

**Files:**
- Modify: `internal/embeddings/store.go`

**Step 1: Add `FindNeighbors` method**

This queries Apache AGE for 1-hop CALLS edges from the matched symbols, then looks up those neighbors in `code_embeddings` to get their metadata.

```go
// Add to internal/embeddings/store.go

// FindNeighbors queries the code graph for 1-hop CALLS neighbors of the given symbols.
// Returns symbols that call or are called by the input symbols.
// Requires the AGE graph to exist (populated by code_graph tool).
func (s *Store) FindNeighbors(ctx context.Context, repoKey string, symbols []string) ([]SearchResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	graphName := repoKey // code_graph uses same naming

	// Query AGE for 1-hop neighbors via CALLS edges (both directions).
	// Use a simple approach: find symbols in code_embeddings that are
	// callers or callees of our result symbols.
	// Since AGE queries are complex and may not have a graph, fall back gracefully.
	cypher := fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$
		 MATCH (a)-[:CALLS]->(b)
		 WHERE a.name = ANY($names) OR b.name = ANY($names)
		 RETURN DISTINCT CASE WHEN a.name = ANY($names) THEN b.name ELSE a.name END AS neighbor
		 $$) AS (neighbor agtype)`, graphName)

	// AGE doesn't support parameterized ANY() in Cypher well.
	// Simpler: query code_embeddings directly — look for symbols that share
	// the same file as our results (co-location heuristic).
	// This is a pragmatic approximation when the graph isn't available.

	// For now, use co-location: symbols in the same files as results.
	seen := make(map[string]bool)
	for _, sym := range symbols {
		seen[sym] = true
	}

	// Collect files from input symbols.
	var files []string
	fileSet := make(map[string]bool)
	for _, sym := range symbols {
		// We need file info — but we only have symbol names here.
		// Better approach: accept SearchResult slice instead of string slice.
	}
	_ = files
	_ = fileSet

	return nil, nil // placeholder
}
```

Hmm, the AGE integration is tricky because:
1. AGE Cypher doesn't support `ANY()` with parameters easily
2. The graph may not exist for the repo
3. We need the `codegraph.Store` which is separate from `embeddings.Store`

**Better approach:** Do graph expansion at the tool handler level using `codegraph.Store.ExecCypher` directly, since the handler already has access to both stores.

Let me redesign this task.

### Task 4: Add graph expansion at handler level

**Files:**
- Create: `internal/embeddings/expand.go`
- Modify: `cmd/go-code/tool_semantic_search.go`

**Step 1: Create the expander**

```go
// internal/embeddings/expand.go
package embeddings

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Expander enriches semantic search results with graph neighbors.
type Expander struct {
	pool *pgxpool.Pool
}

// NewExpander creates an Expander that queries Apache AGE for call graph neighbors.
func NewExpander(pool *pgxpool.Pool) *Expander {
	return &Expander{pool: pool}
}

// Expand takes semantic search results and finds 1-hop CALLS neighbors
// in the code graph, returning additional symbols not already in results.
// graphName is the AGE graph name (from codegraph.GraphNameFor).
func (e *Expander) Expand(ctx context.Context, graphName string, results []SearchResult, maxExtra int) []SearchResult {
	if len(results) == 0 || maxExtra <= 0 {
		return nil
	}

	// Collect symbol names from results.
	names := make([]string, len(results))
	seen := make(map[string]bool)
	for i, r := range results {
		names[i] = r.SymbolName
		seen[r.FilePath+":"+r.SymbolName] = true
	}

	// Build Cypher to find 1-hop CALLS neighbors.
	// AGE limitation: can't use parameterized arrays, so inline names.
	var conditions []string
	for _, n := range names {
		escaped := strings.ReplaceAll(n, "'", "\\'")
		conditions = append(conditions, fmt.Sprintf("a.name = '%s'", escaped))
	}
	if len(conditions) == 0 {
		return nil
	}

	whereClause := strings.Join(conditions, " OR ")
	cypher := fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$
		 MATCH (a)-[:CALLS]->(b)
		 WHERE %s
		 RETURN DISTINCT b.name, b.file, b.kind
		 LIMIT %d
		 $$) AS (name agtype, file agtype, kind agtype)`,
		graphName, whereClause, maxExtra*2)

	rows, err := e.pool.Query(ctx, cypher)
	if err != nil {
		// Graph may not exist — not an error, just no expansion.
		return nil
	}
	defer rows.Close()

	var extra []SearchResult
	for rows.Next() {
		var name, file, kind string
		if err := rows.Scan(&name, &file, &kind); err != nil {
			continue
		}
		// Strip agtype quotes.
		name = stripAgtype(name)
		file = stripAgtype(file)
		kind = stripAgtype(kind)

		key := file + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		extra = append(extra, SearchResult{
			FilePath:   file,
			SymbolName: name,
			SymbolKind: kind,
		})
		if len(extra) >= maxExtra {
			break
		}
	}

	// Also find reverse direction (callers).
	var conditionsRev []string
	for _, n := range names {
		escaped := strings.ReplaceAll(n, "'", "\\'")
		conditionsRev = append(conditionsRev, fmt.Sprintf("b.name = '%s'", escaped))
	}
	cypherRev := fmt.Sprintf(
		`SELECT * FROM cypher('%s', $$
		 MATCH (a)-[:CALLS]->(b)
		 WHERE %s
		 RETURN DISTINCT a.name, a.file, a.kind
		 LIMIT %d
		 $$) AS (name agtype, file agtype, kind agtype)`,
		graphName, strings.Join(conditionsRev, " OR "), maxExtra*2)

	rowsRev, err := e.pool.Query(ctx, cypherRev)
	if err != nil {
		return extra
	}
	defer rowsRev.Close()

	for rowsRev.Next() {
		var name, file, kind string
		if err := rowsRev.Scan(&name, &file, &kind); err != nil {
			continue
		}
		name = stripAgtype(name)
		file = stripAgtype(file)
		kind = stripAgtype(kind)

		key := file + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		extra = append(extra, SearchResult{
			FilePath:   file,
			SymbolName: name,
			SymbolKind: kind,
		})
		if len(extra) >= maxExtra {
			break
		}
	}

	return extra
}

// stripAgtype removes agtype JSON quotes: `"foo"` → `foo`.
func stripAgtype(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
```

**Step 2: Add `expand` param to input and wire in handler**

In `cmd/go-code/tool_semantic_search.go`, add to `SemanticSearchInput`:

```go
Expand bool `json:"expand,omitempty" jsonschema_description:"Expand results with 1-hop call graph neighbors (callers/callees). Requires code_graph index. Default: true"`
```

Add to `SemanticDeps`:

```go
Expander *embeddings.Expander
```

In `register.go`, after creating `semDeps`, wire the expander:

```go
if cfg.EmbedURL != "" && dbPool != nil {
	ec := embeddings.NewClient(cfg.EmbedURL, cfg.EmbedModel)
	es := embeddings.NewStore(dbPool)
	semDeps = SemanticDeps{
		Client:      ec,
		Store:       es,
		Pipeline:    embeddings.NewPipeline(ec, es),
		Expander:    embeddings.NewExpander(dbPool), // ADD THIS
		AnalyzeDeps: deps,
	}
}
```

In `handleSemanticSearch`, after getting results and before returning:

```go
// Graph expansion: find 1-hop CALLS neighbors.
if len(results) > 0 && deps.Expander != nil {
	extra := deps.Expander.Expand(ctx, repoKey, results, 5)
	if len(extra) > 0 {
		// Append graph neighbors with a sentinel distance.
		for i := range extra {
			extra[i].Distance = 1.0 // mark as graph-expanded, not similarity
			extra[i].RepoKey = repoKey
		}
		results = append(results, extra...)
	}
}
```

**Step 3: Update `formatSemanticResults` to show expansion source**

Add a `source` attribute to each result. Semantic hits get `source="semantic"`, graph-expanded hits get `source="graph"`. Detect by checking `Distance == 1.0` sentinel.

Better: add a `Source` field to `SearchResult`:

```go
// In store.go, add to SearchResult:
Source string // "semantic", "keyword", "hybrid", "graph" — empty for pure semantic
```

Then in the format function:

```go
source := r.Source
if source == "" {
	source = "semantic"
}
fmt.Fprintf(&sb, "    <result rank=\"%d\" distance=\"%.4f\" source=\"%s\">\n",
	i+1, r.Distance, source)
```

**Step 4: Build**

```bash
cd /path/to/repos/src/go-code && go build ./...
```

**Step 5: Commit**

```bash
git add internal/embeddings/expand.go cmd/go-code/tool_semantic_search.go cmd/go-code/register.go internal/embeddings/store.go
git commit -m "feat(semantic): add 1-hop graph expansion for search results"
```

---

## Phase 3: Auto-Indexing at Startup

### Task 5: Add startup indexer

**Files:**
- Create: `internal/embeddings/autoindex.go`
- Modify: `cmd/go-code/register.go`

**Step 1: Create auto-indexer**

```go
// internal/embeddings/autoindex.go
package embeddings

import (
	"log/slog"
	"os"
	"path/filepath"
)

// AutoIndex scans directories for Git repositories and starts background indexing.
// dirs is a list of host-mapped directories to scan (e.g. /host-src).
// Each immediate subdirectory containing a .git folder is treated as a repo.
func AutoIndex(pipeline *Pipeline, dirs []string) {
	if pipeline == nil || len(dirs) == 0 {
		return
	}

	var repos []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Warn("autoindex: cannot read dir", slog.String("dir", dir), slog.Any("error", err))
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			repoPath := filepath.Join(dir, e.Name())
			if isGitRepo(repoPath) {
				repos = append(repos, repoPath)
			}
		}
	}

	if len(repos) == 0 {
		slog.Info("autoindex: no repos found")
		return
	}

	slog.Info("autoindex: starting background indexing", slog.Int("repos", len(repos)))
	for _, root := range repos {
		repoKey := filepath.Base(root) // matches codegraph.GraphNameFor for local repos
		pipeline.IndexRepoAsync(repoKey, root)
	}
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}
```

Wait — `codegraph.GraphNameFor` may produce a different key than `filepath.Base`. Let me check.

The `GraphNameFor` function is in the codegraph package. We shouldn't import codegraph from embeddings (wrong dependency direction). Instead, accept a key-generation function or just use the same logic.

Better: call `AutoIndex` from `register.go` where we have access to `codegraph.GraphNameFor`.

```go
// internal/embeddings/autoindex.go
package embeddings

import (
	"log/slog"
	"os"
	"path/filepath"
)

// RepoKeyFunc generates a graph key from a repo root path.
type RepoKeyFunc func(root string) string

// AutoIndex scans directories for Git repositories and starts background indexing.
// dirs is a list of directories to scan. Each immediate subdirectory with .git is indexed.
// keyFn generates the repo key (should match codegraph.GraphNameFor).
func AutoIndex(pipeline *Pipeline, dirs []string, keyFn RepoKeyFunc) {
	if pipeline == nil || len(dirs) == 0 {
		return
	}

	var count int
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Debug("autoindex: skip dir", slog.String("dir", dir), slog.Any("error", err))
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(dir, e.Name())
			if !isGitRepo(root) {
				continue
			}
			key := keyFn(root)
			if pipeline.IndexRepoAsync(key, root) {
				count++
			}
		}
	}

	if count > 0 {
		slog.Info("autoindex: started background indexing", slog.Int("repos", count))
	}
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}
```

**Step 2: Add config for auto-index directories**

In `cmd/go-code/config.go`, add to `Config`:

```go
// AutoIndexDirs are directories to scan for repos at startup (comma-separated).
// Each subdirectory with .git gets background-indexed for semantic search.
AutoIndexDirs []string
```

In `loadConfig()`:

```go
AutoIndexDirs: env.List("AUTO_INDEX_DIRS", ""),
```

**Step 3: Wire in register.go**

After `registerSemanticSearch`, add:

```go
// Auto-index local repos in background.
if semDeps.Pipeline != nil && len(cfg.AutoIndexDirs) > 0 {
	go embeddings.AutoIndex(semDeps.Pipeline, cfg.AutoIndexDirs, codegraph.GraphNameFor)
}
```

**Step 4: Add env var to docker-compose.yml**

In `/path/to/repos/deploy/example-server/docker-compose.yml`, add to `go-code` service environment:

```yaml
AUTO_INDEX_DIRS: "/host-src"
```

This maps to the existing volume mount `/path/to/repos/src:/host-src:ro`.

**Step 5: Build**

```bash
cd /path/to/repos/src/go-code && go build ./...
```

**Step 6: Commit**

```bash
git add internal/embeddings/autoindex.go cmd/go-code/config.go cmd/go-code/register.go
git commit -m "feat(semantic): auto-index local repos at startup"
```

---

## Phase 4: Deploy and Verify

### Task 6: Deploy and test all three features

**Step 1: Build and deploy**

```bash
cd /path/to/repos/deploy/example-server
docker compose build --no-cache go-code
docker compose up -d --no-deps --force-recreate go-code
```

**Step 2: Verify startup auto-indexing**

```bash
docker logs go-code --tail 30 2>&1 | grep -i 'autoindex\|background index'
```

Expected: "autoindex: started background indexing" with repo count matching `~/src/` subdirectories.

**Step 3: Test hybrid search**

Use Claude Code MCP tool `semantic_search` with a query that has both semantic meaning AND keyword matches:

```
semantic_search repo=/path/to/repos/src/go-code query="parse file" top_k=10
```

Expected: Results should include both semantically similar functions AND functions containing "parse file" as text. Results with `source="hybrid"` appear in both lists.

**Step 4: Test graph expansion**

```
semantic_search repo=/path/to/repos/src/go-code query="embedding client" top_k=5
```

Expected: Primary results + additional results with `source="graph"` showing callers/callees of matched functions.

**Step 5: Verify search quality**

Compare results with pure semantic search (before this change) to ensure hybrid doesn't degrade quality. The RRF merge should boost relevant results that appear in both lists.

---

## Notes

### RRF (Reciprocal Rank Fusion)
Standard technique for merging ranked lists. Score = Σ 1/(k+rank) where k=60 is a smoothing constant. Items appearing in multiple lists get higher scores. Used by Elasticsearch, Weaviate, and Pinecone for hybrid search.

### Graph expansion limitations
- Only works if `code_graph` has been run on the repo (AGE graph exists)
- AGE Cypher has no parameterized arrays — symbol names are inlined (SQL injection safe because they come from our own parser, not user input)
- Falls back gracefully: if no graph exists, expansion returns empty and search works as before

### Auto-index performance
- Background indexing uses `IndexRepoAsync` which is concurrent-safe (one goroutine per repo)
- First run may take 1-2 minutes for all repos, subsequent runs skip unchanged symbols (hash check)
- After ONNX O3 optimization, embedding is ~0.15s per request, so indexing is fast
