# Semantic Health Features Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add semantic duplication detection to code_health using existing pgvector embeddings infrastructure.

**Architecture:** New `internal/semhealth/` package bridges `embeddings.Store` and `compare.RepoMetrics`. Adds `FindSimilarPairs` SQL self-join to Store, computes `SemanticDupRatio` metric, integrates into code_health tool handler with optional semantic enrichment (graceful degradation when embeddings unavailable).

**Tech Stack:** Go, PostgreSQL pgvector, existing jina-code-v2 embeddings (768-dim HNSW)

---

### Task 1: Add `FindSimilarPairs` to embeddings Store

Add a method to `embeddings.Store` that finds semantically similar function pairs within a single repo using pgvector cosine distance self-join.

**Files:**
- Modify: `internal/embeddings/store.go`
- Create: `internal/embeddings/store_similarity.go`
- Create: `internal/embeddings/store_similarity_test.go`

**Step 1: Write the failing test**

Create `internal/embeddings/store_similarity_test.go`:

```go
package embeddings

import (
	"testing"
)

func TestSimilarPairResult(t *testing.T) {
	// Verify the type exists and fields are correct.
	p := SimilarPair{
		SymbolA:    "Foo",
		FileA:      "a.go",
		LineA:      10,
		SymbolB:    "Bar",
		FileB:      "b.go",
		LineB:      20,
		Similarity: 0.97,
	}
	if p.SymbolA != "Foo" || p.Similarity != 0.97 {
		t.Errorf("unexpected SimilarPair: %+v", p)
	}
}

func TestSimilarPairOpts(t *testing.T) {
	opts := SimilarPairOpts{RepoKey: "test", Threshold: 0.95, Limit: 50}
	if opts.effectiveThreshold() != 0.95 {
		t.Errorf("threshold = %f, want 0.95", opts.effectiveThreshold())
	}

	// Default threshold.
	opts2 := SimilarPairOpts{RepoKey: "test"}
	if opts2.effectiveThreshold() != defaultSimilarityThreshold {
		t.Errorf("default threshold = %f, want %f", opts2.effectiveThreshold(), defaultSimilarityThreshold)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/embeddings/ -run TestSimilarPair -v`
Expected: FAIL — `SimilarPair` type undefined

**Step 3: Write minimal implementation**

Create `internal/embeddings/store_similarity.go`:

```go
package embeddings

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultSimilarityThreshold = 0.92
	defaultSimilarLimit        = 50
	maxSimilarLimit            = 200
)

// SimilarPair represents two semantically similar symbols within the same repo.
type SimilarPair struct {
	SymbolA    string
	FileA      string
	LineA      int
	SymbolB    string
	FileB      string
	LineB      int
	Similarity float32
}

// SimilarPairOpts controls the similarity search parameters.
type SimilarPairOpts struct {
	RepoKey   string
	Threshold float32 // minimum cosine similarity (default 0.92)
	Limit     int     // max pairs returned (default 50)
}

func (o SimilarPairOpts) effectiveThreshold() float32 {
	if o.Threshold > 0 {
		return o.Threshold
	}
	return defaultSimilarityThreshold
}

func (o SimilarPairOpts) effectiveLimit() int {
	if o.Limit > 0 && o.Limit <= maxSimilarLimit {
		return o.Limit
	}
	if o.Limit > maxSimilarLimit {
		return maxSimilarLimit
	}
	return defaultSimilarLimit
}

// FindSimilarPairs finds semantically similar function pairs within a repo
// using pgvector cosine distance self-join.
func (s *Store) FindSimilarPairs(ctx context.Context, opts SimilarPairOpts) ([]SimilarPair, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	threshold := opts.effectiveThreshold()
	limit := opts.effectiveLimit()

	// Cosine distance: 0 = identical, 2 = opposite.
	// Similarity = 1 - distance. Threshold 0.92 → max distance 0.08.
	maxDist := 1.0 - float64(threshold)

	q := `SELECT a.symbol_name, a.file_path, a.start_line,
	             b.symbol_name, b.file_path, b.start_line,
	             1 - (a.embedding <=> b.embedding) AS similarity
	      FROM code_embeddings a, code_embeddings b
	      WHERE a.repo_key = $1 AND b.repo_key = $1
	        AND (a.file_path || ':' || a.symbol_name) < (b.file_path || ':' || b.symbol_name)
	        AND (a.embedding <=> b.embedding) < $2
	      ORDER BY similarity DESC
	      LIMIT $3`

	rows, err := s.pool.Query(ctx, q, opts.RepoKey, maxDist, limit)
	if err != nil {
		return nil, fmt.Errorf("find similar pairs: %w", err)
	}
	defer rows.Close()

	var pairs []SimilarPair
	for rows.Next() {
		var p SimilarPair
		if err := rows.Scan(&p.SymbolA, &p.FileA, &p.LineA,
			&p.SymbolB, &p.FileB, &p.LineB, &p.Similarity); err != nil {
			return nil, fmt.Errorf("scan pair: %w", err)
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err()
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/embeddings/ -run TestSimilarPair -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/embeddings/store_similarity.go internal/embeddings/store_similarity_test.go
git commit -m "feat(embeddings): add FindSimilarPairs for semantic duplication"
```

---

### Task 2: Create `semhealth` package with semantic duplication analysis

New package that bridges embeddings and compare packages. Computes semantic duplication ratio and collects duplicate groups for recommendations.

**Files:**
- Create: `internal/semhealth/semhealth.go`
- Create: `internal/semhealth/semhealth_test.go`

**Step 1: Write the failing test**

Create `internal/semhealth/semhealth_test.go`:

```go
package semhealth

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

func TestComputeSemanticDupRatio(t *testing.T) {
	tests := []struct {
		name       string
		pairs      []embeddings.SimilarPair
		totalFuncs int
		wantMin    float64
		wantMax    float64
	}{
		{
			name:       "no pairs",
			pairs:      nil,
			totalFuncs: 10,
			wantMin:    0,
			wantMax:    0,
		},
		{
			name: "one pair out of 10",
			pairs: []embeddings.SimilarPair{
				{SymbolA: "Foo", FileA: "a.go", SymbolB: "Bar", FileB: "b.go", Similarity: 0.95},
			},
			totalFuncs: 10,
			wantMin:    0.19,
			wantMax:    0.21, // 2/10 = 0.20
		},
		{
			name:       "zero funcs",
			pairs:      nil,
			totalFuncs: 0,
			wantMin:    0,
			wantMax:    0,
		},
		{
			name: "overlapping pairs share symbols",
			pairs: []embeddings.SimilarPair{
				{SymbolA: "A", FileA: "x.go", SymbolB: "B", FileB: "y.go", Similarity: 0.95},
				{SymbolA: "A", FileA: "x.go", SymbolB: "C", FileB: "z.go", Similarity: 0.93},
			},
			totalFuncs: 10,
			wantMin:    0.29,
			wantMax:    0.31, // 3 unique symbols / 10 = 0.30
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := ComputeSemanticDupRatio(tt.pairs, tt.totalFuncs)
			if ratio < tt.wantMin || ratio > tt.wantMax {
				t.Errorf("ratio = %f, want [%f, %f]", ratio, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCollectDupGroups(t *testing.T) {
	pairs := []embeddings.SimilarPair{
		{SymbolA: "Foo", FileA: "a.go", LineA: 1, SymbolB: "Bar", FileB: "b.go", LineB: 5, Similarity: 0.96},
		{SymbolA: "Foo", FileA: "a.go", LineA: 1, SymbolB: "Baz", FileB: "c.go", LineB: 10, Similarity: 0.94},
		{SymbolA: "X", FileA: "d.go", LineA: 20, SymbolB: "Y", FileB: "e.go", LineB: 30, Similarity: 0.93},
	}

	groups := CollectDupGroups(pairs)

	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	// First group should have 3 symbols (Foo, Bar, Baz).
	if len(groups[0].Symbols) != 3 {
		t.Errorf("group[0] has %d symbols, want 3", len(groups[0].Symbols))
	}
	// Second group has 2 symbols (X, Y).
	if len(groups[1].Symbols) != 2 {
		t.Errorf("group[1] has %d symbols, want 2", len(groups[1].Symbols))
	}
}

func TestSemanticResult(t *testing.T) {
	r := &SemanticResult{
		SemanticDupRatio: 0.15,
		DupGroups: []DupGroup{
			{Symbols: []DupSymbol{{Name: "A", File: "a.go"}, {Name: "B", File: "b.go"}}},
		},
	}
	if r.SemanticDupRatio != 0.15 {
		t.Errorf("ratio = %f, want 0.15", r.SemanticDupRatio)
	}
	if len(r.DupGroups) != 1 {
		t.Errorf("groups = %d, want 1", len(r.DupGroups))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/semhealth/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Write minimal implementation**

Create `internal/semhealth/semhealth.go`:

```go
// Package semhealth provides semantic health analysis bridging
// embeddings search with code quality metrics.
package semhealth

import (
	"context"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// SemanticResult holds semantic analysis results for a repository.
type SemanticResult struct {
	SemanticDupRatio float64    // fraction of functions involved in semantic duplication
	DupGroups        []DupGroup // groups of semantically similar functions
}

// DupGroup is a cluster of semantically similar functions.
type DupGroup struct {
	Symbols       []DupSymbol
	AvgSimilarity float32
}

// DupSymbol identifies a function in a duplicate group.
type DupSymbol struct {
	Name string
	File string
	Line int
}

// ComputeSemanticDupRatio computes the fraction of functions involved in
// semantic duplication. Each unique symbol appearing in any pair counts once.
func ComputeSemanticDupRatio(pairs []embeddings.SimilarPair, totalFuncs int) float64 {
	if totalFuncs == 0 || len(pairs) == 0 {
		return 0
	}
	unique := make(map[string]struct{})
	for _, p := range pairs {
		unique[p.FileA+":"+p.SymbolA] = struct{}{}
		unique[p.FileB+":"+p.SymbolB] = struct{}{}
	}
	return float64(len(unique)) / float64(totalFuncs)
}

// CollectDupGroups clusters similar pairs into groups using union-find.
// Pairs sharing a symbol are merged into the same group.
func CollectDupGroups(pairs []embeddings.SimilarPair) []DupGroup {
	if len(pairs) == 0 {
		return nil
	}

	parent := make(map[string]string)
	symInfo := make(map[string]DupSymbol)
	simSum := make(map[string]float32)
	simCount := make(map[string]int)

	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	for _, p := range pairs {
		keyA := p.FileA + ":" + p.SymbolA
		keyB := p.FileB + ":" + p.SymbolB
		if _, ok := parent[keyA]; !ok {
			parent[keyA] = keyA
			symInfo[keyA] = DupSymbol{Name: p.SymbolA, File: p.FileA, Line: p.LineA}
		}
		if _, ok := parent[keyB]; !ok {
			parent[keyB] = keyB
			symInfo[keyB] = DupSymbol{Name: p.SymbolB, File: p.FileB, Line: p.LineB}
		}
		union(keyA, keyB)
		root := find(keyA)
		simSum[root] += p.Similarity
		simCount[root]++
	}

	// Collect groups by root.
	groups := make(map[string][]string)
	for key := range parent {
		root := find(key)
		groups[root] = append(groups[root], key)
	}

	result := make([]DupGroup, 0, len(groups))
	for root, members := range groups {
		g := DupGroup{
			Symbols: make([]DupSymbol, len(members)),
		}
		for i, m := range members {
			g.Symbols[i] = symInfo[m]
		}
		if simCount[root] > 0 {
			g.AvgSimilarity = simSum[root] / float32(simCount[root])
		}
		result = append(result, g)
	}

	// Sort by group size descending.
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && len(result[j].Symbols) > len(result[j-1].Symbols); j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}

	return result
}

// Analyze runs semantic health analysis for a repo.
// Returns nil result (not error) if embeddings are unavailable.
func Analyze(ctx context.Context, store *embeddings.Store, repoKey string, totalFuncs int) *SemanticResult {
	if store == nil || repoKey == "" || totalFuncs == 0 {
		return nil
	}

	pairs, err := store.FindSimilarPairs(ctx, embeddings.SimilarPairOpts{
		RepoKey: repoKey,
	})
	if err != nil {
		slog.Debug("semhealth: find similar pairs failed",
			slog.String("repo", repoKey), slog.Any("error", err))
		return nil
	}

	if len(pairs) == 0 {
		return &SemanticResult{}
	}

	return &SemanticResult{
		SemanticDupRatio: ComputeSemanticDupRatio(pairs, totalFuncs),
		DupGroups:        CollectDupGroups(pairs),
	}
}

// FormatDupGroupMessage formats a duplicate group for recommendation output.
func FormatDupGroupMessage(g DupGroup) string {
	var sb strings.Builder
	for i, s := range g.Symbols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(s.Name)
		sb.WriteString(" (")
		sb.WriteString(s.File)
		sb.WriteString(")")
	}
	return sb.String()
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/semhealth/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/semhealth/
git commit -m "feat(semhealth): add semantic duplication analysis package"
```

---

### Task 3: Integrate semantic metrics into RepoMetrics and grading

Add `SemanticDupRatio` field to `RepoMetrics`, include it in `GradeScore` with weight, and add recommendation for semantic duplication.

**Files:**
- Modify: `internal/compare/compare.go` (add field to RepoMetrics)
- Modify: `internal/compare/grade.go` (add sub-score)
- Modify: `internal/compare/recommend.go` (add recommendation)

**Step 1: Write the failing test**

Add to existing test files:

In `internal/compare/grade_test.go` (or create if needed):
```go
func TestSemanticDupAffectsScore(t *testing.T) {
	base := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 15, MaxFuncLines: 50,
		AvgComplexity: 4.0, MaxComplexity: 10,
		TestRatio: 0.3, DocRatio: 0.6,
		ErrorHandlingRatio: 0.6,
	}
	baseScore := GradeScore(base)

	withSemDup := base
	withSemDup.SemanticDupRatio = 0.4
	semScore := GradeScore(withSemDup)

	if semScore >= baseScore {
		t.Errorf("semantic dup ratio 0.4: score=%.0f >= base=%.0f", semScore, baseScore)
	}
}
```

In `internal/compare/recommend_test.go` (or add to existing):
```go
func TestSemanticDupRecommendation(t *testing.T) {
	m := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 10, MaxFuncLines: 30,
		AvgComplexity: 2.0, MaxComplexity: 5,
		TestRatio: 0.35, DocRatio: 0.8,
		ErrorHandlingRatio: 0.7,
		SemanticDupRatio: 0.5,
	}
	recs := ComputeRecommendations(m, Outliers{}, 0)

	found := false
	for _, r := range recs {
		if r.Area == "semantic_duplication" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected recommendation for semantic_duplication area")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run "TestSemanticDup" -v`
Expected: FAIL — `SemanticDupRatio` field undefined

**Step 3: Write minimal implementation**

Add to `internal/compare/compare.go` in `RepoMetrics` struct:
```go
SemanticDupRatio float64 `json:"semantic_dup_ratio,omitempty"` // fraction of functions in semantic dup groups
```

Add to `internal/compare/grade.go` in `GradeScore`:
- New sub-score: `semanticDup` with weight 0.05
- Formula: `clamp01(1.0 - m.SemanticDupRatio * 4.0)`
- Reduce existing weight of `duplication` from 0.08 to 0.05 and `magicNumbers` from 0.08 to 0.06 to keep sum = 1.0

Add to `internal/compare/recommend.go`:
- New area `"semantic_duplication"` in score computation
- Message: "N% of functions are semantically similar to others — consider extracting shared logic into reusable helpers"

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run "TestSemanticDup" -v`
Expected: PASS

Also run full compare tests:
Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/compare/compare.go internal/compare/grade.go internal/compare/recommend.go
git commit -m "feat(compare): add SemanticDupRatio metric to grading and recommendations"
```

---

### Task 4: Wire semantic analysis into code_health tool handler

Pass `SemanticDeps` to `registerCodeHealth`, call `semhealth.Analyze` after snapshot, populate `SemanticDupRatio` in metrics, add XML output.

**Files:**
- Modify: `cmd/go-code/tool_code_health.go` (accept SemanticDeps, call Analyze)
- Modify: `cmd/go-code/register.go` (pass semDeps to registerCodeHealth)
- Modify: `cmd/go-code/xml_health.go` or inline (add semantic dup XML fields)

**Step 1: Write the failing test**

In `cmd/go-code/tool_code_health_test.go` (or create):
```go
func TestCodeHealthXMLHasSemanticDup(t *testing.T) {
	// Verify the XML struct accepts SemanticDupRatio.
	resp := buildHealthXML("test", "go",
		compare.RepoMetrics{SemanticDupRatio: 0.15},
		85.0, nil, nil, nil)
	if resp.Health.Metrics.SemanticDupRatio != 0.15 {
		t.Errorf("SemanticDupRatio = %f, want 0.15", resp.Health.Metrics.SemanticDupRatio)
	}
}
```

**Step 2: Run test to verify it fails**

Expected: FAIL — `SemanticDupRatio` not in `xmlCompMetrics`

**Step 3: Write minimal implementation**

1. Add `SemanticDupRatio` to `xmlCompMetrics` struct
2. Update `convertMetrics` to copy the field
3. Change `registerCodeHealth` signature to accept `*SemanticDeps`
4. Update `register.go` to pass `&semDeps`
5. In handler: after `ComputeMetrics`, optionally call `semhealth.Analyze` and set `metrics.SemanticDupRatio`

Key code in handler:
```go
// Semantic duplication (optional, non-fatal).
if semDeps != nil && semDeps.Store != nil {
    repoKey := codegraph.GraphNameFor(root)
    funcCount := countFunctions(snap.Symbols)
    if sem := semhealth.Analyze(ctx, semDeps.Store, repoKey, funcCount); sem != nil {
        metrics.SemanticDupRatio = sem.SemanticDupRatio
        // Re-compute score with semantic data.
        metrics.Score = compare.GradeScore(metrics)
        metrics.Grade = compare.ComputeGrade(metrics)
    }
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./cmd/go-code/ -run TestCodeHealthXML -v`
Expected: PASS

Also: `go build ./cmd/go-code/` to verify compilation.

**Step 5: Commit**

```bash
git add cmd/go-code/tool_code_health.go cmd/go-code/register.go
git commit -m "feat(code_health): integrate semantic duplication analysis"
```

---

### Task 5: Add semantic duplication report mode

Add `focus=semantic_duplicates` to code_health that returns detailed duplicate groups (like `focus=magic_numbers`).

**Files:**
- Modify: `cmd/go-code/tool_code_health.go` (add semantic dup report mode)

**Step 1: Write the failing test**

```go
func TestSemanticDupReportXML(t *testing.T) {
	groups := []semhealth.DupGroup{
		{
			Symbols:       []semhealth.DupSymbol{{Name: "A", File: "a.go", Line: 1}, {Name: "B", File: "b.go", Line: 5}},
			AvgSimilarity: 0.96,
		},
	}
	resp := buildSemanticDupXML("test-repo", "go", groups)
	if resp.Report.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Report.Total)
	}
	if len(resp.Report.Groups) != 1 {
		t.Errorf("Groups = %d, want 1", len(resp.Report.Groups))
	}
}
```

**Step 2: Run test to verify it fails**

Expected: FAIL — `buildSemanticDupXML` undefined

**Step 3: Write minimal implementation**

Add XML types and builder:
```go
type xmlSemanticDupResponse struct {
	XMLName xml.Name           `xml:"response"`
	Report  xmlSemanticReport  `xml:"semantic_duplicates"`
}

type xmlSemanticReport struct {
	Repo     string            `xml:"repo,attr"`
	Language string            `xml:"language,attr,omitempty"`
	Total    int               `xml:"total,attr"`
	Groups   []xmlDupGroup     `xml:"group"`
}

type xmlDupGroup struct {
	Similarity string          `xml:"similarity,attr"`
	Symbols    []xmlDupSymbol  `xml:"function"`
}

type xmlDupSymbol struct {
	Name string `xml:"name,attr"`
	File string `xml:"file,attr"`
	Line int    `xml:"line,attr"`
}
```

Add handler branch:
```go
if input.Focus == "semantic_duplicates" {
    // ... resolve repo, get repoKey, call semhealth.Analyze
    // ... build and return xmlSemanticDupResponse
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./cmd/go-code/ -run TestSemanticDup -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/go-code/tool_code_health.go
git commit -m "feat(code_health): add focus=semantic_duplicates report mode"
```

---

### Task 6: End-to-end test and deploy

Run full test suite, lint, build, and deploy.

**Step 1: Run full test suite**

```bash
cd /home/krolik/src/go-code && go test ./...
```

Expected: All PASS

**Step 2: Run linter**

```bash
cd /home/krolik/src/go-code && make lint
```

Expected: PASS

**Step 3: Build**

```bash
cd /home/krolik/src/go-code && make build
```

Expected: SUCCESS

**Step 4: Deploy**

```bash
cd ~/deploy/krolik-server && docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 5: Commit any remaining fixes**

```bash
git add -A && git commit -m "chore: final fixes for semantic health features"
```
