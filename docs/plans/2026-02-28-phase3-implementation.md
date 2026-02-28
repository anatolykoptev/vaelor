# Phase 3: Comparison Engine — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `code_compare` MCP tool that compares two repositories, finds the better implementation, identifies missing features, and recommends architectural improvements. Output: JSON with computed metrics + structured LLM analysis.

**Architecture:** Structural diff approach — ingest both repos in parallel, match symbols (exact → fuzzy → semantic), compute quality metrics, build comparison context for LLM, return JSON verdict. All logic in `internal/compare/`, tool handler in `cmd/go-code/tool_code_compare.go`.

**Tech Stack:** Go, tree-sitter (existing parser), LLM via CLIProxyAPI, Levenshtein for fuzzy matching.

---

### Task 1: Update Types in `internal/compare/compare.go`

**Files:**
- Modify: `internal/compare/compare.go` (full rewrite, 115 lines)
- Test: `internal/compare/compare_test.go` (create)

**Step 1: Write the test for new types**

```go
// internal/compare/compare_test.go
package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestSymbolMatchTypes(t *testing.T) {
	m := SymbolMatch{
		SymbolA:   &parser.Symbol{Name: "Serve", Kind: parser.KindFunction},
		SymbolB:   &parser.Symbol{Name: "Serve", Kind: parser.KindFunction},
		MatchType: MatchExact,
		Category:  "routing",
		Score:     1.0,
	}
	if m.IsGap() {
		t.Error("matched pair should not be a gap")
	}

	gap := SymbolMatch{SymbolA: nil, SymbolB: &parser.Symbol{Name: "Shutdown"}, MatchType: MatchSemantic}
	if !gap.IsGap() {
		t.Error("nil SymbolA should be a gap")
	}
	if gap.MissingIn() != "repo_a" {
		t.Errorf("expected missing_in=repo_a, got %s", gap.MissingIn())
	}
}

func TestRepoMetricsZero(t *testing.T) {
	m := RepoMetrics{}
	if m.AvgFuncLines != 0 {
		t.Error("zero value should be 0")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestSymbolMatch -v`
Expected: FAIL — `SymbolMatch`, `MatchExact`, `IsGap`, `MissingIn` not defined.

**Step 3: Rewrite `compare.go` with new types**

Replace entire `internal/compare/compare.go` with:

```go
package compare

import "github.com/anatolykoptev/go-code/internal/parser"

// MatchType classifies how two symbols were matched.
type MatchType string

const (
	MatchExact    MatchType = "exact"
	MatchFuzzy    MatchType = "fuzzy"
	MatchSemantic MatchType = "semantic"
)

// SymbolMatch pairs two symbols from different repos.
// SymbolA or SymbolB may be nil (coverage gap).
type SymbolMatch struct {
	SymbolA   *parser.Symbol
	SymbolB   *parser.Symbol
	MatchType MatchType
	Category  string  // semantic category: "routing", "auth", etc.
	Score     float64 // confidence 0-1
}

// IsGap reports whether one side is missing.
func (m SymbolMatch) IsGap() bool {
	return m.SymbolA == nil || m.SymbolB == nil
}

// MissingIn returns "repo_a" or "repo_b" for gaps, empty for matches.
func (m SymbolMatch) MissingIn() string {
	if m.SymbolA == nil {
		return "repo_a"
	}
	if m.SymbolB == nil {
		return "repo_b"
	}
	return ""
}

// RepoSnapshot is a parsed, summarized view of a repository.
type RepoSnapshot struct {
	Name       string
	Root       string
	Language   string
	Symbols    []*parser.Symbol
	Imports    []string
	Files      []SnapshotFile
	FileCount  int
	TotalLines int
}

// SnapshotFile holds per-file metadata within a snapshot.
type SnapshotFile struct {
	RelPath  string
	Language string
	Lines    int
	Symbols  []*parser.Symbol
	Imports  []string
}

// RepoMetrics holds computed quality signals.
type RepoMetrics struct {
	Files              int     `json:"files"`
	TotalLines         int     `json:"total_lines"`
	AvgFuncLines       float64 `json:"avg_func_lines"`
	MaxFuncLines       int     `json:"max_func_lines"`
	TestRatio          float64 `json:"test_ratio"`
	DocRatio           float64 `json:"doc_ratio"`
	ErrorHandlingRatio float64 `json:"error_handling_ratio"`
	Interfaces         int     `json:"interfaces"`
	ExternalDeps       int     `json:"external_deps"`
}

// LLMAnalysis is the structured JSON expected from the LLM.
type LLMAnalysis struct {
	Quality         []QualityAspect       `json:"quality"`
	Gaps            []CoverageGap         `json:"gaps"`
	Architecture    []ArchitectureInsight `json:"architecture"`
	Recommendations []string              `json:"recommendations"`
}

// QualityAspect is a single quality comparison point.
type QualityAspect struct {
	Aspect   string `json:"aspect"`
	Winner   string `json:"winner"`
	Reason   string `json:"reason"`
	SnippetA string `json:"snippet_a,omitempty"`
	SnippetB string `json:"snippet_b,omitempty"`
}

// CoverageGap is a feature present in one repo but missing in another.
type CoverageGap struct {
	MissingIn  string `json:"missing_in"`
	Feature    string `json:"feature"`
	LocationB  string `json:"location_b,omitempty"`
	Importance string `json:"importance"`
}

// ArchitectureInsight is a pattern worth adopting.
type ArchitectureInsight struct {
	Insight string `json:"insight"`
	Source  string `json:"source"`
	Example string `json:"example,omitempty"`
	Benefit string `json:"benefit"`
}

// CompareResult is the final output of a comparison.
type CompareResult struct {
	RepoA          string      `json:"repo_a"`
	RepoB          string      `json:"repo_b"`
	Query          string      `json:"query"`
	MetricsA       RepoMetrics `json:"metrics_a"`
	MetricsB       RepoMetrics `json:"metrics_b"`
	Analysis       LLMAnalysis `json:"analysis"`
	MatchedSymbols int         `json:"matched_symbols"`
	UnmatchedA     int         `json:"unmatched_a"`
	UnmatchedB     int         `json:"unmatched_b"`
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/compare/compare.go internal/compare/compare_test.go
git commit -m "feat(compare): define Phase 3 types — SymbolMatch, RepoMetrics, CompareResult"
```

---

### Task 2: Implement `snapshot.go` — BuildSnapshot

**Files:**
- Create: `internal/compare/snapshot.go`
- Test: `internal/compare/snapshot_test.go` (create)

**Step 1: Write the test**

```go
// internal/compare/snapshot_test.go
package compare

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSnapshot(t *testing.T) {
	// Use the go-code repo itself as test input.
	root := findRepoRoot(t)

	snap, err := BuildSnapshot(context.Background(), root, SnapshotOpts{
		Language: "go",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snap.Name == "" {
		t.Error("expected non-empty Name")
	}
	if snap.FileCount == 0 {
		t.Error("expected files > 0")
	}
	if len(snap.Symbols) == 0 {
		t.Error("expected symbols > 0")
	}
}

func TestBuildSnapshotWithFocus(t *testing.T) {
	root := findRepoRoot(t)

	snap, err := BuildSnapshot(context.Background(), root, SnapshotOpts{
		Focus: "internal/parser",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	// All files should be under internal/parser.
	for _, f := range snap.Files {
		if !hasPrefix(f.RelPath, "internal/parser") {
			t.Errorf("unexpected file outside focus: %s", f.RelPath)
		}
	}
}

func hasPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}

// findRepoRoot walks up from the test file to find go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find repo root")
		}
		dir = parent
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestBuildSnapshot -v`
Expected: FAIL — `BuildSnapshot`, `SnapshotOpts` not defined.

**Step 3: Implement `snapshot.go`**

```go
// Package compare — snapshot.go builds RepoSnapshot from a local repo path.
package compare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// SnapshotOpts controls snapshot building.
type SnapshotOpts struct {
	Focus    string // subdirectory filter
	Language string // language filter
}

// BuildSnapshot ingests and parses a local repo into a RepoSnapshot.
func BuildSnapshot(ctx context.Context, root string, opts SnapshotOpts) (*RepoSnapshot, error) {
	var langs []string
	if opts.Language != "" {
		langs = []string{opts.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		Focus:        opts.Focus,
		Languages:    langs,
		MaxFileBytes: 512 * 1024, // 512 KB
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ir.Files)

	snap := &RepoSnapshot{
		Name:      filepath.Base(root),
		Root:      root,
		Language:  dominantLang(ir.Files),
		FileCount: len(ir.Files),
	}

	importSet := make(map[string]struct{})
	for _, pr := range parseResults {
		sf := SnapshotFile{
			RelPath:  pr.file.RelPath,
			Language: pr.file.Language,
		}
		if pr.result != nil {
			sf.Symbols = pr.result.Symbols
			sf.Imports = pr.result.Imports
			snap.Symbols = append(snap.Symbols, pr.result.Symbols...)
			for _, imp := range pr.result.Imports {
				importSet[imp] = struct{}{}
			}
		}
		// Count lines.
		if content, err := os.ReadFile(pr.file.Path); err == nil {
			lines := strings.Count(string(content), "\n") + 1
			sf.Lines = lines
			snap.TotalLines += lines
		}
		snap.Files = append(snap.Files, sf)
	}

	snap.Imports = make([]string, 0, len(importSet))
	for imp := range importSet {
		snap.Imports = append(snap.Imports, imp)
	}

	return snap, nil
}

// fileParseResult pairs a file with its parse output.
type fileParseResult struct {
	file   *ingest.File
	result *parser.ParseResult
}

// parseFilesParallel parses files using a CPU-bound worker pool.
func parseFilesParallel(ctx context.Context, files []*ingest.File) []fileParseResult {
	results := make([]fileParseResult, len(files))

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	work := make(chan int, len(files))
	for i := range files {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				f := files[idx]
				source, err := os.ReadFile(f.Path)
				if err != nil {
					results[idx] = fileParseResult{file: f}
					continue
				}
				pr, err := parser.ParseFile(f.Path, source, parser.ParseOpts{
					Language:       f.Language,
					IncludeBody:    true,
					IncludeImports: true,
				})
				if err != nil {
					results[idx] = fileParseResult{file: f}
					continue
				}
				results[idx] = fileParseResult{file: f, result: pr}
			}
		}()
	}

	wg.Wait()
	return results
}

// dominantLang returns the most common language among files.
func dominantLang(files []*ingest.File) string {
	counts := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			counts[f.Language]++
		}
	}
	best := ""
	max := 0
	for lang, c := range counts {
		if c > max {
			max = c
			best = lang
		}
	}
	return best
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestBuildSnapshot -v`
Expected: PASS

**Step 5: Lint and commit**

```bash
cd /home/krolik/src/go-code && make lint
git add internal/compare/snapshot.go internal/compare/snapshot_test.go
git commit -m "feat(compare): add BuildSnapshot — ingest+parse into RepoSnapshot"
```

---

### Task 3: Implement `metrics.go` — ComputeMetrics

**Files:**
- Create: `internal/compare/metrics.go`
- Test: `internal/compare/metrics_test.go` (create)

**Step 1: Write the test**

```go
// internal/compare/metrics_test.go
package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestComputeMetrics(t *testing.T) {
	snap := &RepoSnapshot{
		FileCount:  10,
		TotalLines: 500,
		Files: []SnapshotFile{
			{RelPath: "main.go", Lines: 50, Symbols: []*parser.Symbol{
				{Name: "main", Kind: parser.KindFunction, StartLine: 1, EndLine: 10},
				{Name: "run", Kind: parser.KindFunction, StartLine: 12, EndLine: 42, Signature: "func run() error"},
			}},
			{RelPath: "main_test.go", Lines: 30},
			{RelPath: "handler.go", Lines: 80, Symbols: []*parser.Symbol{
				{Name: "Handler", Kind: parser.KindInterface, StartLine: 1, EndLine: 5},
				{Name: "Serve", Kind: parser.KindFunction, StartLine: 7, EndLine: 50, Signature: "func Serve() error", DocComment: "// Serve handles requests."},
			}},
		},
		Symbols: []*parser.Symbol{
			{Name: "main", Kind: parser.KindFunction, StartLine: 1, EndLine: 10},
			{Name: "run", Kind: parser.KindFunction, StartLine: 12, EndLine: 42, Signature: "func run() error"},
			{Name: "Handler", Kind: parser.KindInterface},
			{Name: "Serve", Kind: parser.KindFunction, StartLine: 7, EndLine: 50, Signature: "func Serve() error", DocComment: "// Serve handles requests."},
		},
		Imports: []string{"fmt", "net/http", "github.com/gorilla/mux"},
	}

	m := ComputeMetrics(snap)

	if m.Files != 10 {
		t.Errorf("files: got %d, want 10", m.Files)
	}
	if m.TotalLines != 500 {
		t.Errorf("total_lines: got %d, want 500", m.TotalLines)
	}
	// 3 functions: main(10), run(31), Serve(44) → avg = 28.33
	if m.AvgFuncLines < 20 || m.AvgFuncLines > 35 {
		t.Errorf("avg_func_lines: got %.1f, want ~28", m.AvgFuncLines)
	}
	if m.MaxFuncLines != 44 {
		t.Errorf("max_func_lines: got %d, want 44", m.MaxFuncLines)
	}
	// 1 test file out of 3 → 0.33
	if m.TestRatio < 0.3 || m.TestRatio > 0.4 {
		t.Errorf("test_ratio: got %.2f, want ~0.33", m.TestRatio)
	}
	if m.Interfaces != 1 {
		t.Errorf("interfaces: got %d, want 1", m.Interfaces)
	}
	// 1 external dep (gorilla/mux), fmt and net/http are stdlib.
	if m.ExternalDeps != 1 {
		t.Errorf("external_deps: got %d, want 1", m.ExternalDeps)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestComputeMetrics -v`
Expected: FAIL — `ComputeMetrics` not defined.

**Step 3: Implement `metrics.go`**

```go
package compare

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ComputeMetrics calculates quality signals from a RepoSnapshot.
func ComputeMetrics(snap *RepoSnapshot) RepoMetrics {
	m := RepoMetrics{
		Files:      snap.FileCount,
		TotalLines: snap.TotalLines,
	}

	// Function line counts.
	var funcLines []int
	for _, sym := range snap.Symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			lines := int(sym.EndLine) - int(sym.StartLine) + 1
			if lines > 0 {
				funcLines = append(funcLines, lines)
			}
		}
	}
	if len(funcLines) > 0 {
		total := 0
		for _, l := range funcLines {
			total += l
			if l > m.MaxFuncLines {
				m.MaxFuncLines = l
			}
		}
		m.AvgFuncLines = float64(total) / float64(len(funcLines))
	}

	// Test file ratio.
	testFiles := 0
	totalFiles := len(snap.Files)
	for _, f := range snap.Files {
		if isTestFile(f.RelPath) {
			testFiles++
		}
	}
	if totalFiles > 0 {
		m.TestRatio = float64(testFiles) / float64(totalFiles)
	}

	// Doc comment ratio (exported symbols with doc comments).
	exported := 0
	documented := 0
	for _, sym := range snap.Symbols {
		if isExported(sym.Name) {
			exported++
			if sym.DocComment != "" {
				documented++
			}
		}
	}
	if exported > 0 {
		m.DocRatio = float64(documented) / float64(exported)
	}

	// Error handling ratio (functions returning error).
	funcCount := 0
	errorFuncs := 0
	for _, sym := range snap.Symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			funcCount++
			if strings.Contains(sym.Signature, "error") {
				errorFuncs++
			}
		}
	}
	if funcCount > 0 {
		m.ErrorHandlingRatio = float64(errorFuncs) / float64(funcCount)
	}

	// Interface count.
	for _, sym := range snap.Symbols {
		if sym.Kind == parser.KindInterface {
			m.Interfaces++
		}
	}

	// External dependencies.
	for _, imp := range snap.Imports {
		if !isStdlib(imp) {
			m.ExternalDeps++
		}
	}

	return m
}

// isTestFile checks if a file path looks like a test file.
func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/")
}

// isExported checks if a symbol name is exported (starts with uppercase).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// isStdlib checks if an import path looks like a stdlib package.
func isStdlib(path string) bool {
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".")
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestComputeMetrics -v`
Expected: PASS

**Step 5: Lint and commit**

```bash
cd /home/krolik/src/go-code && make lint
git add internal/compare/metrics.go internal/compare/metrics_test.go
git commit -m "feat(compare): add ComputeMetrics — quality signals from RepoSnapshot"
```

---

### Task 4: Implement `match.go` — Symbol Matching (Exact + Fuzzy)

**Files:**
- Create: `internal/compare/match.go`
- Test: `internal/compare/match_test.go` (create)

**Step 1: Write the test**

```go
// internal/compare/match_test.go
package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestMatchExact(t *testing.T) {
	a := []*parser.Symbol{
		{Name: "Serve", Kind: parser.KindFunction, Signature: "func Serve(w http.ResponseWriter, r *http.Request)"},
		{Name: "Config", Kind: parser.KindStruct},
		{Name: "OnlyInA", Kind: parser.KindFunction},
	}
	b := []*parser.Symbol{
		{Name: "Serve", Kind: parser.KindFunction, Signature: "func Serve(ctx context.Context, w http.ResponseWriter)"},
		{Name: "Config", Kind: parser.KindStruct},
		{Name: "OnlyInB", Kind: parser.KindFunction},
	}

	matches := MatchSymbols(a, b, nil)

	exact := 0
	gapsA := 0
	gapsB := 0
	for _, m := range matches {
		switch {
		case m.SymbolA == nil:
			gapsA++
		case m.SymbolB == nil:
			gapsB++
		case m.MatchType == MatchExact:
			exact++
		}
	}

	if exact != 2 {
		t.Errorf("exact matches: got %d, want 2", exact)
	}
	if gapsA != 1 {
		t.Errorf("gaps in A: got %d, want 1 (OnlyInB)", gapsA)
	}
	if gapsB != 1 {
		t.Errorf("gaps in B: got %d, want 1 (OnlyInA)", gapsB)
	}
}

func TestMatchFuzzy(t *testing.T) {
	a := []*parser.Symbol{
		{Name: "HandleRequest", Kind: parser.KindFunction, Signature: "func HandleRequest(w http.ResponseWriter)"},
	}
	b := []*parser.Symbol{
		{Name: "HandleReq", Kind: parser.KindFunction, Signature: "func HandleReq(w http.ResponseWriter)"},
	}

	matches := MatchSymbols(a, b, nil)

	found := false
	for _, m := range matches {
		if m.MatchType == MatchFuzzy && m.SymbolA != nil && m.SymbolB != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected fuzzy match between HandleRequest and HandleReq")
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "ab", 1},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run "TestMatch|TestLevenshtein" -v`
Expected: FAIL — `MatchSymbols`, `levenshtein` not defined.

**Step 3: Implement `match.go`**

```go
package compare

import "github.com/anatolykoptev/go-code/internal/parser"

const (
	fuzzyThreshold = 0.7 // minimum similarity for fuzzy match
)

// LLMClassifier classifies unmatched symbols into semantic categories.
// If nil, semantic matching is skipped.
type LLMClassifier interface {
	ClassifySymbols(symbolsA, symbolsB []*parser.Symbol) ([]SymbolMatch, error)
}

// MatchSymbols matches symbols from two repos: exact → fuzzy → semantic.
// classifier may be nil to skip semantic matching.
func MatchSymbols(symbolsA, symbolsB []*parser.Symbol, classifier LLMClassifier) []SymbolMatch {
	var matches []SymbolMatch
	usedA := make(map[int]bool)
	usedB := make(map[int]bool)

	// Pass 1: Exact match by name + kind.
	for i, a := range symbolsA {
		for j, b := range symbolsB {
			if usedB[j] {
				continue
			}
			if a.Name == b.Name && a.Kind == b.Kind {
				matches = append(matches, SymbolMatch{
					SymbolA:   a,
					SymbolB:   b,
					MatchType: MatchExact,
					Score:     1.0,
				})
				usedA[i] = true
				usedB[j] = true
				break
			}
		}
	}

	// Pass 2: Fuzzy match on remaining by name similarity.
	for i, a := range symbolsA {
		if usedA[i] {
			continue
		}
		bestIdx := -1
		bestScore := 0.0
		for j, b := range symbolsB {
			if usedB[j] {
				continue
			}
			if a.Kind != b.Kind {
				continue
			}
			sim := nameSimilarity(a.Name, b.Name)
			if sim >= fuzzyThreshold && sim > bestScore {
				bestScore = sim
				bestIdx = j
			}
		}
		if bestIdx >= 0 {
			matches = append(matches, SymbolMatch{
				SymbolA:   a,
				SymbolB:   symbolsB[bestIdx],
				MatchType: MatchFuzzy,
				Score:     bestScore,
			})
			usedA[i] = true
			usedB[bestIdx] = true
		}
	}

	// Pass 3: Semantic match via LLM (if classifier provided).
	if classifier != nil {
		var unmatchedA, unmatchedB []*parser.Symbol
		for i, a := range symbolsA {
			if !usedA[i] {
				unmatchedA = append(unmatchedA, a)
			}
		}
		for j, b := range symbolsB {
			if !usedB[j] {
				unmatchedB = append(unmatchedB, b)
			}
		}
		if len(unmatchedA) > 0 && len(unmatchedB) > 0 {
			semantic, err := classifier.ClassifySymbols(unmatchedA, unmatchedB)
			if err == nil {
				for _, sm := range semantic {
					matches = append(matches, sm)
					// Mark as used.
					for i, a := range symbolsA {
						if a == sm.SymbolA {
							usedA[i] = true
						}
					}
					for j, b := range symbolsB {
						if b == sm.SymbolB {
							usedB[j] = true
						}
					}
				}
			}
		}
	}

	// Remaining unmatched → gaps.
	for i, a := range symbolsA {
		if !usedA[i] {
			matches = append(matches, SymbolMatch{SymbolA: a, SymbolB: nil})
		}
	}
	for j, b := range symbolsB {
		if !usedB[j] {
			matches = append(matches, SymbolMatch{SymbolA: nil, SymbolB: b})
		}
	}

	return matches
}

// nameSimilarity returns a 0-1 similarity score between two names.
func nameSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}

	return prev[len(b)]
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run "TestMatch|TestLevenshtein" -v`
Expected: PASS

**Step 5: Lint and commit**

```bash
cd /home/krolik/src/go-code && make lint
git add internal/compare/match.go internal/compare/match_test.go
git commit -m "feat(compare): add MatchSymbols — exact + fuzzy + semantic matching"
```

---

### Task 5: Implement `context.go` — BuildCompareContext

**Files:**
- Create: `internal/compare/context.go`
- Test: `internal/compare/context_test.go` (create)

**Step 1: Write the test**

```go
// internal/compare/context_test.go
package compare

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestBuildCompareContext(t *testing.T) {
	metricsA := RepoMetrics{Files: 10, TotalLines: 500, AvgFuncLines: 25, Interfaces: 2}
	metricsB := RepoMetrics{Files: 15, TotalLines: 800, AvgFuncLines: 15, Interfaces: 5}

	matches := []SymbolMatch{
		{
			SymbolA:   &parser.Symbol{Name: "Serve", Kind: parser.KindFunction, Body: "func Serve() { log.Print(\"serving\") }", File: "/a/server.go"},
			SymbolB:   &parser.Symbol{Name: "Serve", Kind: parser.KindFunction, Body: "func Serve(ctx context.Context) error { return nil }", File: "/b/server.go"},
			MatchType: MatchExact,
			Score:     1.0,
		},
		{
			SymbolA:   nil,
			SymbolB:   &parser.Symbol{Name: "Shutdown", Kind: parser.KindFunction, Body: "func Shutdown() {}", File: "/b/server.go"},
			MatchType: MatchSemantic,
			Category:  "lifecycle",
		},
	}

	ctx := BuildCompareContext(matches, metricsA, metricsB, "compare error handling")

	if !strings.Contains(ctx, "Serve") {
		t.Error("context should contain matched symbol Serve")
	}
	if !strings.Contains(ctx, "Shutdown") {
		t.Error("context should contain gap symbol Shutdown")
	}
	if !strings.Contains(ctx, "compare error handling") {
		t.Error("context should contain query")
	}
	if !strings.Contains(ctx, "avg_func_lines") {
		t.Error("context should contain metrics")
	}
}

func TestBuildCompareContextBudget(t *testing.T) {
	// Large body should be truncated.
	bigBody := strings.Repeat("x", 20000)
	matches := []SymbolMatch{{
		SymbolA:   &parser.Symbol{Name: "Big", Kind: parser.KindFunction, Body: bigBody},
		SymbolB:   &parser.Symbol{Name: "Big", Kind: parser.KindFunction, Body: "func Big() {}"},
		MatchType: MatchExact,
	}}

	ctx := BuildCompareContext(matches, RepoMetrics{}, RepoMetrics{}, "test")
	if len(ctx) > 200000 {
		t.Errorf("context too large: %d chars", len(ctx))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext -v`
Expected: FAIL — `BuildCompareContext` not defined.

**Step 3: Implement `context.go`**

```go
package compare

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxContextChars  = 180_000
	maxSnippetChars  = 3_000
	maxMatchedPairs  = 80
	maxGapSymbols    = 40
)

// BuildCompareContext assembles a structured comparison context for the LLM.
func BuildCompareContext(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string) string {
	var sb strings.Builder

	// Section 1: Query.
	sb.WriteString("## Query\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	// Section 2: Metrics.
	sb.WriteString("## Metrics\n```json\n")
	metricsJSON, _ := json.Marshal(map[string]RepoMetrics{
		"repo_a": metricsA,
		"repo_b": metricsB,
	})
	sb.Write(metricsJSON)
	sb.WriteString("\n```\n\n")

	// Section 3: Matched pairs with code snippets.
	sb.WriteString("## Matched Symbols (side-by-side)\n\n")
	pairCount := 0
	for _, m := range matches {
		if m.IsGap() || pairCount >= maxMatchedPairs {
			continue
		}
		if sb.Len() > maxContextChars {
			break
		}
		writeMatchedPair(&sb, m)
		pairCount++
	}

	// Section 4: Coverage gaps.
	sb.WriteString("## Coverage Gaps\n\n")
	gapCount := 0
	for _, m := range matches {
		if !m.IsGap() || gapCount >= maxGapSymbols {
			continue
		}
		if sb.Len() > maxContextChars {
			break
		}
		writeGap(&sb, m)
		gapCount++
	}

	return sb.String()
}

// writeMatchedPair writes a single matched symbol pair.
func writeMatchedPair(sb *strings.Builder, m SymbolMatch) {
	fmt.Fprintf(sb, "### %s %s [%s, score=%.2f]\n", m.SymbolA.Kind, m.SymbolA.Name, m.MatchType, m.Score)

	if m.Category != "" {
		fmt.Fprintf(sb, "Category: %s\n", m.Category)
	}

	sb.WriteString("**Repo A:**\n```\n")
	sb.WriteString(truncate(m.SymbolA.Body, maxSnippetChars))
	sb.WriteString("\n```\n")

	sb.WriteString("**Repo B:**\n```\n")
	sb.WriteString(truncate(m.SymbolB.Body, maxSnippetChars))
	sb.WriteString("\n```\n\n")
}

// writeGap writes a single coverage gap.
func writeGap(sb *strings.Builder, m SymbolMatch) {
	var sym *parser.Symbol
	missingIn := m.MissingIn()
	if m.SymbolA != nil {
		sym = m.SymbolA
	} else {
		sym = m.SymbolB
	}

	fmt.Fprintf(sb, "### MISSING in %s: %s %s\n", missingIn, sym.Kind, sym.Name)
	if sym.File != "" {
		fmt.Fprintf(sb, "Location: %s\n", sym.File)
	}
	if sym.Body != "" {
		sb.WriteString("```\n")
		sb.WriteString(truncate(sym.Body, maxSnippetChars))
		sb.WriteString("\n```\n")
	}
	sb.WriteString("\n")
}

// truncate limits a string to maxLen chars.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
```

Note: Add `import "github.com/anatolykoptev/go-code/internal/parser"` to context.go (needed for `parser.Symbol` in `writeGap`).

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext -v`
Expected: PASS

**Step 5: Lint and commit**

```bash
cd /home/krolik/src/go-code && make lint
git add internal/compare/context.go internal/compare/context_test.go
git commit -m "feat(compare): add BuildCompareContext — LLM context with matched pairs and gaps"
```

---

### Task 6: Implement `CompareRepos` Orchestrator + LLM System Prompt

**Files:**
- Modify: `internal/compare/compare.go` (add CompareRepos function)
- Modify: `internal/llm/llm.go` (update SystemPromptCodeCompare)
- Test: `internal/compare/compare_test.go` (add integration test)

**Step 1: Write the test**

Add to `internal/compare/compare_test.go`:

```go
func TestCompareReposIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRoot(t)

	// Compare the repo against itself — should get exact matches, no gaps.
	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root,
		RootB: root,
		Query: "compare error handling",
		Opts:  SnapshotOpts{Language: "go"},
	}, nil) // nil LLM = skip LLM analysis
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}
	if result.MatchedSymbols == 0 {
		t.Error("expected matched symbols > 0")
	}
	if result.UnmatchedA != 0 || result.UnmatchedB != 0 {
		t.Errorf("self-compare should have 0 unmatched, got A=%d B=%d", result.UnmatchedA, result.UnmatchedB)
	}
	if result.MetricsA.Files == 0 {
		t.Error("expected metrics to be computed")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestCompareReposIntegration -v`
Expected: FAIL — `CompareRepos`, `CompareInput` not defined.

**Step 3: Add `CompareRepos` to `compare.go` and update LLM prompt**

Add to `internal/compare/compare.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// CompareInput is the input for CompareRepos.
type CompareInput struct {
	RootA string
	RootB string
	Query string
	Opts  SnapshotOpts
}

// CompareRepos orchestrates a full comparison between two repositories.
// llmClient may be nil to skip LLM analysis (useful for testing).
func CompareRepos(ctx context.Context, input CompareInput, llmClient *llm.Client) (*CompareResult, error) {
	// Build snapshots in parallel.
	var snapA, snapB *RepoSnapshot
	var errA, errB error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		snapA, errA = BuildSnapshot(ctx, input.RootA, input.Opts)
	}()
	go func() {
		defer wg.Done()
		snapB, errB = BuildSnapshot(ctx, input.RootB, input.Opts)
	}()
	wg.Wait()

	if errA != nil {
		return nil, fmt.Errorf("snapshot repo_a: %w", errA)
	}
	if errB != nil {
		return nil, fmt.Errorf("snapshot repo_b: %w", errB)
	}

	// Match symbols.
	matches := MatchSymbols(snapA.Symbols, snapB.Symbols, nil)

	// Compute metrics.
	metricsA := ComputeMetrics(snapA)
	metricsB := ComputeMetrics(snapB)

	// Count matches and gaps.
	matched, unmatchedA, unmatchedB := 0, 0, 0
	for _, m := range matches {
		switch {
		case m.SymbolA == nil:
			unmatchedA++
		case m.SymbolB == nil:
			unmatchedB++
		default:
			matched++
		}
	}

	result := &CompareResult{
		RepoA:          snapA.Name,
		RepoB:          snapB.Name,
		Query:          input.Query,
		MetricsA:       metricsA,
		MetricsB:       metricsB,
		MatchedSymbols: matched,
		UnmatchedA:     unmatchedA,
		UnmatchedB:     unmatchedB,
	}

	// LLM analysis (optional).
	if llmClient != nil {
		compareCtx := BuildCompareContext(matches, metricsA, metricsB, input.Query)
		answer, err := llmClient.Complete(ctx, llm.SystemPromptCodeCompare, compareCtx)
		if err != nil {
			return nil, fmt.Errorf("llm compare: %w", err)
		}
		result.Analysis = parseAnalysis(answer)
	}

	return result, nil
}

// parseAnalysis tries to parse LLM response as JSON LLMAnalysis.
// Falls back to wrapping raw text in recommendations.
func parseAnalysis(raw string) LLMAnalysis {
	// Try to extract JSON from markdown code block.
	cleaned := extractJSON(raw)

	var analysis LLMAnalysis
	if err := json.Unmarshal([]byte(cleaned), &analysis); err != nil {
		// Fallback: treat entire response as a recommendation.
		return LLMAnalysis{
			Recommendations: []string{raw},
		}
	}
	return analysis
}

// extractJSON tries to extract a JSON block from markdown-wrapped LLM output.
func extractJSON(s string) string {
	// Look for ```json ... ``` blocks.
	start := strings.Index(s, "```json")
	if start >= 0 {
		s = s[start+7:]
		end := strings.Index(s, "```")
		if end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	// Look for { ... } directly.
	start = strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
```

Also add `"strings"` to the import block.

Update `internal/llm/llm.go` — replace `SystemPromptCodeCompare`:

```go
const SystemPromptCodeCompare = `You are a lead software engineer conducting a comparative code review of two repositories.
Your task is to find the BETTER solution — more modern, more optimized, more scalable, higher quality.

You receive: matched symbol pairs (side-by-side code), coverage gaps, and computed metrics.

Respond with ONLY a JSON object (no markdown, no explanation outside JSON):

{
  "quality": [
    {
      "aspect": "error handling",
      "winner": "repo_a" or "repo_b",
      "reason": "concise explanation with evidence",
      "snippet_a": "relevant code from repo A",
      "snippet_b": "relevant code from repo B"
    }
  ],
  "gaps": [
    {
      "missing_in": "repo_a" or "repo_b",
      "feature": "what is missing",
      "location_b": "file path where it exists",
      "importance": "high" or "medium" or "low"
    }
  ],
  "architecture": [
    {
      "insight": "pattern or architectural decision worth adopting",
      "source": "repo_a" or "repo_b",
      "example": "specific file or function",
      "benefit": "why this improves the codebase"
    }
  ],
  "recommendations": [
    "Actionable recommendation 1",
    "Actionable recommendation 2"
  ]
}

Focus on:
1. Implementation quality — cleaner, more optimized, more modern approach
2. Missing functionality — features one repo has that the other lacks
3. Architecture — package structure, separation of concerns, extensibility, testability
4. Concrete recommendations — specific actions to improve the weaker repo`
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestCompareReposIntegration -v`
Expected: PASS

**Step 5: Lint and commit**

```bash
cd /home/krolik/src/go-code && make lint
git add internal/compare/compare.go internal/compare/compare_test.go internal/llm/llm.go
git commit -m "feat(compare): add CompareRepos orchestrator + quality-focused LLM prompt"
```

---

### Task 7: Wire `code_compare` MCP Tool Handler

**Files:**
- Modify: `cmd/go-code/tool_code_compare.go` (replace stub)
- Modify: `cmd/go-code/register.go` (pass deps)

**Step 1: Replace `tool_code_compare.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeCompareInput is the input schema for the code_compare tool.
type CodeCompareInput struct {
	RepoA string `json:"repo_a" jsonschema_description:"First repository: GitHub slug (owner/repo) or absolute local path"`
	RepoB string `json:"repo_b" jsonschema_description:"Second repository: GitHub slug (owner/repo) or absolute local path"`
	Query string `json:"query" jsonschema_description:"What to compare or what quality aspects to evaluate"`
	Focus string `json:"focus,omitempty" jsonschema_description:"Subdirectory filter for module-level comparison (e.g. internal/auth)"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit comparison to files of this language (e.g. go, python, rust)"`
}

func registerCodeCompare(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "code_compare",
		Description: "Compare two code repositories to find the better implementation. " +
			"Analyzes architecture, code quality, patterns, and identifies missing features. " +
			"Returns JSON with quality verdicts, coverage gaps, architecture insights, " +
			"metrics, and actionable recommendations. Works cross-language.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeCompareInput) (*mcp.CallToolResult, any, error) {
		if input.RepoA == "" || input.RepoB == "" {
			return errResult("repo_a and repo_b are required"), nil, nil
		}
		if input.Query == "" {
			return errResult("query is required"), nil, nil
		}

		rootA, cleanupA, err := resolveRoot(ctx, input.RepoA, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_a: %s", err)), nil, nil
		}
		defer cleanupA()

		rootB, cleanupB, err := resolveRoot(ctx, input.RepoB, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_b: %s", err)), nil, nil
		}
		defer cleanupB()

		result, err := compare.CompareRepos(ctx, compare.CompareInput{
			RootA: rootA,
			RootB: rootB,
			Query: input.Query,
			Opts: compare.SnapshotOpts{
				Focus:    input.Focus,
				Language: input.Language,
			},
		}, deps.LLM)
		if err != nil {
			return errResult(fmt.Sprintf("compare: %s", err)), nil, nil
		}

		output, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal result: %s", err)), nil, nil
		}

		return textResult(string(output)), nil, nil
	})
}
```

**Step 2: Update `register.go` — pass deps to registerCodeCompare**

Change line 40 from:
```go
registerCodeCompare(server, cfg)
```
to:
```go
registerCodeCompare(server, cfg, deps)
```

**Step 3: Build and verify**

Run: `cd /home/krolik/src/go-code && go build ./cmd/go-code/`
Expected: compiles without errors.

**Step 4: Lint and commit**

```bash
cd /home/krolik/src/go-code && make lint
git add cmd/go-code/tool_code_compare.go cmd/go-code/register.go
git commit -m "feat: wire code_compare MCP tool — replace Phase 3 stub with full implementation"
```

---

### Task 8: Full Integration Test + Deploy

**Files:**
- Modify: `internal/compare/compare_test.go` (add cross-repo test)
- No new files

**Step 1: Run all tests**

```bash
cd /home/krolik/src/go-code && go test ./... -v -count=1
```

Expected: all pass.

**Step 2: Run linter**

```bash
cd /home/krolik/src/go-code && make lint
```

Expected: no errors. Fix any issues.

**Step 3: Deploy**

```bash
cd ~/deploy/krolik-server && docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 4: Smoke test via MCP**

Use the `code_compare` tool to compare two small repos and verify JSON output.

**Step 5: Update docs**

- Update `docs/ROADMAP.md`: mark Phase 3.1 and 3.2 as complete
- Update `CLAUDE.md`: update tool count and `code_compare` description
- Commit docs changes

**Step 6: Tag release**

```bash
git tag -a v1.5.0 -m "Phase 3: Comparison Engine — code_compare with quality assessment"
git push origin v1.5.0
```

---

## Task Dependencies

```
Task 1 (types) ──→ Task 2 (snapshot) ──→ Task 6 (orchestrator) ──→ Task 7 (MCP tool) ──→ Task 8 (deploy)
                ──→ Task 3 (metrics) ──↗
                ──→ Task 4 (matching) ──↗
                ──→ Task 5 (context) ──↗
```

Tasks 2, 3, 4, 5 can run in **parallel** after Task 1.
Task 6 depends on all of 2-5.
Task 7 depends on 6.
Task 8 depends on 7.
