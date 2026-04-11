# Diff-Aware Review (v1.19) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add git-integrated change analysis — detect changed symbols, compute differential impact, generate review context with risk guidance via `review_delta` and `review_pr` MCP tools.

**Architecture:** New `internal/review/` package orchestrates: (1) git diff → changed files, (2) parser intersection → changed symbols, (3) existing `impact.Analyze()` per symbol → aggregate blast radius, (4) TESTED_BY graph query → untested flags, (5) risk guidance generation. The `TESTED_BY` edge is added to `internal/codegraph/` graph building. Two new MCP tools in `cmd/go-code/`.

**Tech Stack:** `os/exec` for git commands (no external deps), existing `parser`, `impact`, `callgraph`, `codegraph` packages. Forge API for PR metadata.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/review/diff.go` | Git diff execution + parsing (changed files, line ranges) |
| `internal/review/diff_test.go` | Tests for diff parsing |
| `internal/review/symbols.go` | Intersect diff line ranges with parsed symbols |
| `internal/review/symbols_test.go` | Tests for symbol-diff intersection |
| `internal/review/delta.go` | Delta review orchestrator (pipeline: diff → symbols → impact → risk) |
| `internal/review/delta_test.go` | Tests for delta pipeline |
| `internal/review/risk.go` | Risk guidance generation |
| `internal/review/risk_test.go` | Tests for risk scoring |
| `internal/codegraph/tested_by.go` | TESTED_BY edge extraction + graph building |
| `internal/codegraph/tested_by_test.go` | Tests for TESTED_BY patterns |
| `cmd/go-code/tool_review_delta.go` | MCP tool handler for `review_delta` |
| `cmd/go-code/tool_review_pr.go` | MCP tool handler for `review_pr` |

---

### Task 1: Git Diff Layer

**Files:**
- Create: `internal/review/diff.go`
- Create: `internal/review/diff_test.go`

- [ ] **Step 1: Write failing test for ChangedFiles**

```go
// internal/review/diff_test.go
package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")
	// Initial commit with one file.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	// Second commit: modify + add.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() { run() }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "util.go"), []byte("package main\nfunc run() {}\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "second")
	return dir
}

func TestChangedFiles(t *testing.T) {
	dir := setupGitRepo(t)
	files, err := ChangedFiles(context.Background(), dir, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 changed files, got %d: %v", len(files), files)
	}
}

func TestChangedFilesStagedFallback(t *testing.T) {
	dir := setupGitRepo(t)
	// Stage a new file.
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0o644)
	exec.Command("git", "-C", dir, "add", "new.go").Run()

	files, err := ChangedFiles(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 1 {
		t.Fatal("expected at least 1 staged file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestChanged -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// internal/review/diff.go
package review

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FileDiff describes changes to a single file.
type FileDiff struct {
	Path       string     // relative file path
	Added      int        // lines added
	Removed    int        // lines removed
	LineRanges []LineRange // changed line ranges in the new version
}

// LineRange is an inclusive range of line numbers.
type LineRange struct {
	Start int
	End   int
}

// ChangedFiles returns files changed between base ref and HEAD.
// If base is empty, falls back to staged changes (git diff --cached).
func ChangedFiles(ctx context.Context, repoRoot, base string) ([]FileDiff, error) {
	if base == "" {
		return diffCached(ctx, repoRoot)
	}
	return diffBase(ctx, repoRoot, base)
}

func diffBase(ctx context.Context, root, base string) ([]FileDiff, error) {
	out, err := GitExec(ctx, root, "diff", "--numstat", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	files := parseNumstat(out)

	// Get line ranges from unified diff.
	uniOut, err := GitExec(ctx, root, "diff", "--unified=0", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git diff unified: %w", err)
	}
	rangeMap := parseUnifiedRanges(uniOut)
	for i := range files {
		files[i].LineRanges = rangeMap[files[i].Path]
	}
	return files, nil
}

func diffCached(ctx context.Context, root string) ([]FileDiff, error) {
	out, err := GitExec(ctx, root, "diff", "--cached", "--numstat")
	if err != nil {
		return nil, fmt.Errorf("git diff --cached: %w", err)
	}
	files := parseNumstat(out)

	uniOut, err := GitExec(ctx, root, "diff", "--cached", "--unified=0")
	if err != nil {
		return nil, fmt.Errorf("git diff --cached unified: %w", err)
	}
	rangeMap := parseUnifiedRanges(uniOut)
	for i := range files {
		files[i].LineRanges = rangeMap[files[i].Path]
	}
	return files, nil
}

// GitExec runs a git command in the given directory. Exported for use by tool_review_pr.go.
func GitExec(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func parseNumstat(out string) []FileDiff {
	var files []FileDiff
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		removed, _ := strconv.Atoi(parts[1])
		files = append(files, FileDiff{
			Path:    parts[2],
			Added:   added,
			Removed: removed,
		})
	}
	return files
}

// parseUnifiedRanges extracts changed line ranges from unified diff output.
// Parses @@ -old,len +new,len @@ headers to get new-side ranges.
func parseUnifiedRanges(out string) map[string][]LineRange {
	result := make(map[string][]LineRange)
	var curFile string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			curFile = strings.TrimPrefix(line, "+++ b/")
		} else if strings.HasPrefix(line, "@@") && curFile != "" {
			if r, ok := parseHunkHeader(line); ok {
				result[curFile] = append(result[curFile], r)
			}
		}
	}
	return result
}

// parseHunkHeader parses "@@ -old,len +new,start @@ ..." into a LineRange.
func parseHunkHeader(line string) (LineRange, bool) {
	// Find +N or +N,M
	idx := strings.Index(line, "+")
	if idx < 0 {
		return LineRange{}, false
	}
	rest := line[idx+1:]
	end := strings.Index(rest, " ")
	if end < 0 {
		end = len(rest)
	}
	rest = rest[:end]

	parts := strings.SplitN(rest, ",", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return LineRange{}, false
	}
	length := 1
	if len(parts) > 1 {
		length, _ = strconv.Atoi(parts[1])
	}
	if length == 0 {
		return LineRange{}, false // pure deletion, no new lines
	}
	return LineRange{Start: start, End: start + length - 1}, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestChanged -v`
Expected: PASS

- [ ] **Step 5: Write test for parseUnifiedRanges**

```go
func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		line string
		want LineRange
		ok   bool
	}{
		{"@@ -10,3 +15,4 @@ func foo()", LineRange{15, 18}, true},
		{"@@ -5 +5,2 @@", LineRange{5, 6}, true},
		{"@@ -5,3 +5,0 @@", LineRange{}, false}, // pure deletion
	}
	for _, tt := range tests {
		got, ok := parseHunkHeader(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseHunkHeader(%q) = %v, %v; want %v, %v", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}
```

- [ ] **Step 6: Run all review tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd /home/krolik/src/go-code && git add internal/review/diff.go internal/review/diff_test.go
git commit -m "feat(review): add git diff layer with line range extraction"
```

---

### Task 2: Symbol-Diff Intersection

**Files:**
- Create: `internal/review/symbols.go`
- Create: `internal/review/symbols_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/review/symbols_test.go
package review

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestChangedSymbols(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 5, EndLine: 15},
		{Name: "Bar", Kind: parser.KindFunction, File: "/repo/main.go", StartLine: 20, EndLine: 30},
		{Name: "Baz", Kind: parser.KindFunction, File: "/repo/util.go", StartLine: 1, EndLine: 10},
	}
	diffs := []FileDiff{
		{Path: "main.go", LineRanges: []LineRange{{10, 12}}},     // overlaps Foo
		{Path: "other.go", LineRanges: []LineRange{{1, 5}}},      // no matching symbols
	}

	changed := ChangedSymbols(symbols, diffs, "/repo")
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed symbol, got %d", len(changed))
	}
	if changed[0].Symbol.Name != "Foo" {
		t.Errorf("expected Foo, got %s", changed[0].Symbol.Name)
	}
	if changed[0].ChangeType != ChangeModified {
		t.Errorf("expected modified, got %s", changed[0].ChangeType)
	}
}

func TestChangedSymbolsNewFile(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "New", Kind: parser.KindFunction, File: "/repo/new.go", StartLine: 1, EndLine: 10},
	}
	diffs := []FileDiff{
		{Path: "new.go", Added: 10, Removed: 0, LineRanges: []LineRange{{1, 10}}},
	}

	changed := ChangedSymbols(symbols, diffs, "/repo")
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed symbol, got %d", len(changed))
	}
	if changed[0].ChangeType != ChangeAdded {
		t.Errorf("expected added, got %s", changed[0].ChangeType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestChangedSymbols -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/review/symbols.go
package review

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// NOTE: parser import is required — Symbol type used throughout.

// ChangeType describes how a symbol was modified.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeModified ChangeType = "modified"
	ChangeRemoved  ChangeType = "removed"
)

// ChangedSymbol pairs a symbol with its change type.
type ChangedSymbol struct {
	Symbol     *parser.Symbol
	ChangeType ChangeType
	FileDiff   FileDiff
}

// ChangedSymbols intersects parsed symbols with git diff line ranges.
// repoRoot is the absolute path to the repo root (symbols have absolute File paths).
func ChangedSymbols(symbols []*parser.Symbol, diffs []FileDiff, repoRoot string) []ChangedSymbol {
	// Index symbols by relative file path.
	byFile := make(map[string][]*parser.Symbol)
	for _, sym := range symbols {
		rel := relPath(sym.File, repoRoot)
		byFile[rel] = append(byFile[rel], sym)
	}

	var result []ChangedSymbol
	for _, diff := range diffs {
		fileSymbols := byFile[diff.Path]
		if len(fileSymbols) == 0 {
			continue
		}

		isNewFile := diff.Removed == 0 && diff.Added > 0 && len(diff.LineRanges) == 1

		for _, sym := range fileSymbols {
			if overlaps(sym, diff.LineRanges) {
				ct := ChangeModified
				if isNewFile {
					ct = ChangeAdded
				}
				result = append(result, ChangedSymbol{
					Symbol:     sym,
					ChangeType: ct,
					FileDiff:   diff,
				})
			}
		}
	}
	return result
}

// overlaps checks if any diff line range overlaps with the symbol's span.
func overlaps(sym *parser.Symbol, ranges []LineRange) bool {
	for _, r := range ranges {
		if int(sym.StartLine) <= r.End && r.Start <= int(sym.EndLine) {
			return true
		}
	}
	return false
}

func relPath(absPath, root string) string {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		// Fallback: trim prefix.
		return strings.TrimPrefix(absPath, root+"/")
	}
	return rel
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestChangedSymbols -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-code && git add internal/review/symbols.go internal/review/symbols_test.go
git commit -m "feat(review): add symbol-diff intersection"
```

---

### Task 3: TESTED_BY Edge Extraction

**Files:**
- Create: `internal/codegraph/tested_by.go`
- Create: `internal/codegraph/tested_by_test.go`
- Modify: `internal/codegraph/graph_build.go` (add TESTED_BY edges to `buildGraph`)
- Modify: `internal/codegraph/schema.go` (add TESTED_BY to schema text)

- [ ] **Step 1: Write failing test**

```go
// internal/codegraph/tested_by_test.go
package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestExtractTestedByEdges_Go(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "ProcessOrder", Kind: parser.KindFunction, File: "order.go", Language: "go"},
		{Name: "TestProcessOrder", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "Test_ProcessOrder_empty", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "BenchmarkProcessOrder", Kind: parser.KindFunction, File: "order_test.go", Language: "go"},
		{Name: "Helper", Kind: parser.KindFunction, File: "order.go", Language: "go"},
	}

	edges := ExtractTestedByEdges("", symbols)

	// TestProcessOrder → ProcessOrder, Test_ProcessOrder_empty → ProcessOrder
	// BenchmarkProcessOrder → ProcessOrder
	found := map[string]bool{}
	for _, e := range edges {
		found[e.FromKey+"->"+e.ToKey] = true
	}
	if !found["TestProcessOrder:order_test.go->ProcessOrder:order.go"] {
		t.Error("missing TestProcessOrder -> ProcessOrder")
	}
	if !found["Test_ProcessOrder_empty:order_test.go->ProcessOrder:order.go"] {
		t.Error("missing Test_ProcessOrder_empty -> ProcessOrder")
	}
	if len(edges) < 2 {
		t.Errorf("expected at least 2 edges, got %d", len(edges))
	}
}

func TestExtractTestedByEdges_Python(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "process_order", Kind: parser.KindFunction, File: "order.py", Language: "python"},
		{Name: "test_process_order", Kind: parser.KindFunction, File: "test_order.py", Language: "python"},
		{Name: "TestOrder", Kind: parser.KindType, File: "test_order.py", Language: "python"},
		{Name: "Order", Kind: parser.KindType, File: "order.py", Language: "python"},
	}

	edges := ExtractTestedByEdges("", symbols)
	if len(edges) < 1 {
		t.Fatal("expected at least 1 edge")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -run TestExtractTestedBy -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/codegraph/tested_by.go
package codegraph

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ExtractTestedByEdges creates TESTED_BY edges mapping test functions to tested symbols.
// Patterns:
//   - Go: TestXxx / Test_Xxx → Xxx; BenchmarkXxx → Xxx
//   - Python: test_xxx → xxx; TestXxx → Xxx class
//   - TS/JS: file-level fallback (test file → source file symbols)
func ExtractTestedByEdges(root string, symbols []*parser.Symbol) []edgeData {
	// Index non-test symbols by name and by file.
	byName := make(map[string][]*parser.Symbol)
	byFile := make(map[string][]*parser.Symbol) // source file → symbols
	for _, s := range symbols {
		if isTestSymbol(s) {
			continue
		}
		byName[s.Name] = append(byName[s.Name], s)
		byFile[s.File] = append(byFile[s.File], s)
	}

	var edges []edgeData
	seen := make(map[string]bool)

	for _, s := range symbols {
		if !isTestSymbol(s) {
			continue
		}

		targets := resolveTestTarget(s, byName)

		// File-level fallback: test file → corresponding source file.
		if len(targets) == 0 {
			srcFile := guessSourceFile(s.File, s.Language)
			if srcFile != "" {
				targets = byFile[srcFile]
			}
		}

		for _, tgt := range targets {
			fromKey := s.Name + ":" + relPathOrSelf(s.File, root)
			toKey := tgt.Name + ":" + relPathOrSelf(tgt.File, root)
			key := fromKey + "->" + toKey
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, edgeData{
				FromLabel: "Symbol",
				FromKey:   fromKey,
				ToLabel:   "Symbol",
				ToKey:     toKey,
				EdgeLabel: "TESTED_BY",
				Props:     map[string]string{},
			})
		}
	}

	return edges
}

func resolveTestTarget(test *parser.Symbol, byName map[string][]*parser.Symbol) []*parser.Symbol {
	switch test.Language {
	case "go":
		return resolveGoTest(test, byName)
	case "python":
		return resolvePythonTest(test, byName)
	default:
		return nil
	}
}

func resolveGoTest(test *parser.Symbol, byName map[string][]*parser.Symbol) []*parser.Symbol {
	name := test.Name
	// TestXxx → Xxx, Test_Xxx_suffix → Xxx
	for _, prefix := range []string{"Test_", "Test", "Benchmark"} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		// For Test_Xxx_suffix, take first segment.
		if parts := strings.SplitN(rest, "_", 2); len(parts) > 0 {
			if targets := byName[parts[0]]; len(targets) > 0 {
				return targets
			}
		}
		if targets := byName[rest]; len(targets) > 0 {
			return targets
		}
	}
	return nil
}

func resolvePythonTest(test *parser.Symbol, byName map[string][]*parser.Symbol) []*parser.Symbol {
	name := test.Name
	// test_xxx → xxx
	if strings.HasPrefix(name, "test_") {
		target := strings.TrimPrefix(name, "test_")
		if targets := byName[target]; len(targets) > 0 {
			return targets
		}
	}
	// TestXxx class → Xxx class
	if strings.HasPrefix(name, "Test") {
		target := strings.TrimPrefix(name, "Test")
		if targets := byName[target]; len(targets) > 0 {
			return targets
		}
	}
	return nil
}

func isTestSymbol(s *parser.Symbol) bool {
	if s.Kind != parser.KindFunction && s.Kind != parser.KindMethod && s.Kind != parser.KindType {
		return false
	}
	switch s.Language {
	case "go":
		return strings.HasPrefix(s.Name, "Test") || strings.HasPrefix(s.Name, "Benchmark")
	case "python":
		return strings.HasPrefix(s.Name, "test_") || strings.HasPrefix(s.Name, "Test")
	default:
		return isTestFile(s.File)
	}
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.js") ||
		strings.Contains(base, "_test.")
}

func guessSourceFile(testFile, lang string) string {
	base := filepath.Base(testFile)
	dir := filepath.Dir(testFile)
	switch lang {
	case "go":
		return filepath.Join(dir, strings.TrimSuffix(base, "_test.go")+".go")
	case "python":
		if strings.HasPrefix(base, "test_") {
			return filepath.Join(dir, strings.TrimPrefix(base, "test_"))
		}
	}
	return ""
}

func relPathOrSelf(path, root string) string {
	if root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -run TestExtractTestedBy -v`
Expected: PASS

- [ ] **Step 5: Wire TESTED_BY into graph building**

Add to `internal/codegraph/graph_build.go` — in `buildGraph()`, after the INHERITS/IMPLEMENTS edges block:

```go
// TESTED_BY edges (test Symbol → tested Symbol).
testedByEdges := ExtractTestedByEdges(root, symbols)
edges = append(edges, testedByEdges...)
```

Add to `internal/codegraph/schema.go` — in `GraphSchemaText()`, add after the FETCHES line:

```go
b.WriteString("  - TESTED_BY (Symbol->Symbol) — test function covers production symbol\n")
```

- [ ] **Step 6: Run existing codegraph tests to verify nothing broke**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd /home/krolik/src/go-code && git add internal/codegraph/tested_by.go internal/codegraph/tested_by_test.go internal/codegraph/graph_build.go internal/codegraph/schema.go
git commit -m "feat(codegraph): add TESTED_BY edges for test-to-symbol mapping"
```

---

### Task 4: Risk Guidance Generator

**Files:**
- Create: `internal/review/risk.go`
- Create: `internal/review/risk_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/review/risk_test.go
package review

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestGenerateRiskGuidance(t *testing.T) {
	input := RiskInput{
		ChangedSymbols: []ChangedSymbol{
			{Symbol: &parser.Symbol{Name: "Auth"}, ChangeType: ChangeModified},
		},
		ImpactResults: map[string]*impact.Result{
			"Auth": {Symbol: "Auth", Found: true, TotalAffected: 25, BlastRadius: "high",
				AffectedPackages: []string{"pkg/api", "pkg/middleware", "pkg/handler"}},
		},
		UntestedSymbols: []string{"Auth"},
	}

	guidance := GenerateRiskGuidance(input)
	if guidance.RiskLevel != "high" {
		t.Errorf("expected high risk, got %s", guidance.RiskLevel)
	}
	if len(guidance.Flags) == 0 {
		t.Error("expected at least one flag")
	}
}

func TestGenerateRiskGuidanceLow(t *testing.T) {
	input := RiskInput{
		ChangedSymbols: []ChangedSymbol{
			{Symbol: &parser.Symbol{Name: "helper"}, ChangeType: ChangeModified},
		},
		ImpactResults: map[string]*impact.Result{
			"helper": {Symbol: "helper", Found: true, TotalAffected: 1, BlastRadius: "low"},
		},
	}

	guidance := GenerateRiskGuidance(input)
	if guidance.RiskLevel == "high" {
		t.Error("should not be high risk")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestGenerateRisk -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/review/risk.go
package review

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// Thresholds for risk flags.
const (
	wideBlastThreshold   = 20
	crossPkgThreshold    = 3
	manyChangesThreshold = 10
)

// RiskInput provides data for risk assessment.
type RiskInput struct {
	ChangedSymbols  []ChangedSymbol
	ImpactResults   map[string]*impact.Result // symbol name → impact
	UntestedSymbols []string                  // symbols lacking TESTED_BY edges
	HasInheritance  bool                      // any INHERITS/IMPLEMENTS changes
}

// RiskGuidance is the generated risk assessment.
type RiskGuidance struct {
	RiskLevel      string   // low, medium, high
	RiskScore      float64  // numeric aggregate
	Flags          []string // human-readable risk flags
	Suggestions    []string // actionable suggestions
}

// GenerateRiskGuidance analyzes changes and produces risk guidance.
func GenerateRiskGuidance(input RiskInput) RiskGuidance {
	var flags []string
	var suggestions []string
	var maxRisk float64

	// Wide blast radius.
	for _, ir := range input.ImpactResults {
		if ir.TotalAffected >= wideBlastThreshold {
			flags = append(flags, fmt.Sprintf("Wide blast radius: %s affects %d symbols across %d packages",
				ir.Symbol, ir.TotalAffected, len(ir.AffectedPackages)))
			suggestions = append(suggestions, "Scrutinize all callers and dependents for side effects")
		}
		if ir.RiskScore > maxRisk {
			maxRisk = ir.RiskScore
		}
	}

	// Untested changes.
	if len(input.UntestedSymbols) > 0 {
		flags = append(flags, fmt.Sprintf("Untested changes: %s lack test coverage",
			strings.Join(input.UntestedSymbols, ", ")))
		suggestions = append(suggestions, "Add tests for untested symbols before merging")
	}

	// Cross-package impact.
	totalPkgs := countUniquePkgs(input.ImpactResults)
	if totalPkgs >= crossPkgThreshold {
		flags = append(flags, fmt.Sprintf("Cross-package impact: changes affect %d packages", totalPkgs))
		suggestions = append(suggestions, "Consider splitting into smaller, focused PRs")
	}

	// Inheritance changes.
	if input.HasInheritance {
		flags = append(flags, "Inheritance/interface changes detected")
		suggestions = append(suggestions, "Verify Liskov substitution principle compliance")
	}

	// Many changed symbols.
	if len(input.ChangedSymbols) >= manyChangesThreshold {
		flags = append(flags, fmt.Sprintf("Large changeset: %d symbols modified", len(input.ChangedSymbols)))
	}

	level := classifyRisk(len(flags), maxRisk)

	return RiskGuidance{
		RiskLevel:   level,
		RiskScore:   maxRisk,
		Flags:       flags,
		Suggestions: suggestions,
	}
}

func classifyRisk(flagCount int, maxRiskScore float64) string {
	if flagCount >= 3 || maxRiskScore >= 20 {
		return "high"
	}
	if flagCount >= 1 || maxRiskScore >= 5 {
		return "medium"
	}
	return "low"
}

func countUniquePkgs(results map[string]*impact.Result) int {
	pkgs := make(map[string]bool)
	for _, r := range results {
		for _, p := range r.AffectedPackages {
			pkgs[p] = true
		}
	}
	return len(pkgs)
}

// SymbolNames extracts just the names from ChangedSymbol slice.
func SymbolNames(cs []ChangedSymbol) []string {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Symbol.Name
	}
	return names
}
```

Note: add `_ = parser.KindFunction` import guard or include proper import of parser in risk_test.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestGenerateRisk -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-code && git add internal/review/risk.go internal/review/risk_test.go
git commit -m "feat(review): add risk guidance generator"
```

---

### Task 5: Delta Review Orchestrator

**Files:**
- Create: `internal/review/delta.go`
- Create: `internal/review/delta_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/review/delta_test.go (append to existing test file or create new)
package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDeltaReview(t *testing.T) {
	dir := setupGitRepoWithSymbols(t)
	result, err := DeltaReview(context.Background(), DeltaInput{
		Root:  dir,
		Base:  "HEAD~1",
		Depth: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedFiles) == 0 {
		t.Error("expected changed files")
	}
	if result.Risk.RiskLevel == "" {
		t.Error("expected risk level")
	}
}

func setupGitRepoWithSymbols(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	src := `package main

func ProcessOrder(id int) error {
	return nil
}

func Helper() string {
	return "help"
}
`
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	// Modify ProcessOrder.
	src2 := `package main

func ProcessOrder(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid id")
	}
	return nil
}

func Helper() string {
	return "help"
}
`
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644)
	run("add", ".")
	run("commit", "-m", "validate id")

	return dir
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestDeltaReview -v`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/review/delta.go
package review

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// DeltaInput configures a delta review.
type DeltaInput struct {
	Root     string // repo root (absolute path)
	Base     string // base ref (default "HEAD~1")
	Depth    int    // impact traversal depth (default 2)
	Language string // optional language filter
	OxCodes  *oxcodes.Client
}

// DeltaResult is the output of a delta review.
type DeltaResult struct {
	ChangedFiles    []FileDiff       `json:"changed_files"`
	ChangedSymbols  []ChangedSymbol  `json:"changed_symbols"`
	ImpactedSymbols []ImpactedSymbol `json:"impacted_symbols"`
	UntestedSymbols []string         `json:"untested_symbols,omitempty"`
	Risk            RiskGuidance     `json:"risk"`
	Tier            string           `json:"tier"`
}

// ImpactedSymbol is a downstream symbol affected by a change.
type ImpactedSymbol struct {
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Distance   int     `json:"distance"`
	Confidence float64 `json:"confidence"`
	ChangedBy  string  `json:"changed_by"` // which changed symbol caused this
}

const defaultDeltaDepth = 2

// DeltaReview runs the full delta review pipeline.
func DeltaReview(ctx context.Context, input DeltaInput) (*DeltaResult, error) {
	if input.Base == "" {
		input.Base = "HEAD~1"
	}
	if input.Depth <= 0 {
		input.Depth = defaultDeltaDepth
	}

	// Step 1: Git diff.
	diffs, err := ChangedFiles(ctx, input.Root, input.Base)
	if err != nil {
		return nil, fmt.Errorf("changed files: %w", err)
	}
	if len(diffs) == 0 {
		return &DeltaResult{Risk: RiskGuidance{RiskLevel: "low"}}, nil
	}

	// Step 2: Build call graph for current state.
	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     input.Root,
		Language: input.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("build call graph: %w", err)
	}

	// Step 3: Intersect diffs with symbols.
	changed := ChangedSymbols(cg.Symbols, diffs, input.Root)

	// Step 4: Impact analysis per changed symbol.
	impactResults := make(map[string]*impact.Result)
	var allImpacted []ImpactedSymbol
	for _, cs := range changed {
		ir := impact.Analyze(ctx, cg, cs.Symbol.Name, impact.Options{
			MaxDepth: input.Depth,
			OxCodes:  input.OxCodes,
			Root:     input.Root,
			Language: input.Language,
		})
		if ir.Found {
			impactResults[cs.Symbol.Name] = ir
			for _, a := range ir.DirectCallers {
				allImpacted = append(allImpacted, ImpactedSymbol{
					Name: a.Name, File: a.File, Distance: a.Distance,
					Confidence: a.Confidence, ChangedBy: cs.Symbol.Name,
				})
			}
			for _, a := range ir.TransitiveCallers {
				allImpacted = append(allImpacted, ImpactedSymbol{
					Name: a.Name, File: a.File, Distance: a.Distance,
					Confidence: a.Confidence, ChangedBy: cs.Symbol.Name,
				})
			}
		}
	}

	// Step 5: Deduplicate impacted symbols.
	allImpacted = dedup(allImpacted)

	// Step 6: Local TESTED_BY detection — find changed symbols lacking tests.
	// First pass: naming convention (TestXxx → Xxx).
	testedSet := buildTestedSet(cg.Symbols)
	// Second pass: ox-codes scoped search — find test files that reference changed symbols
	// inside function bodies (catches table-driven tests, helper-wrapped tests, etc.).
	if input.OxCodes != nil {
		enrichTestedSetViaOxCodes(ctx, input.OxCodes, input.Root, changed, testedSet)
	}
	var untestedSymbols []string
	for _, cs := range changed {
		if !testedSet[cs.Symbol.Name] {
			untestedSymbols = append(untestedSymbols, cs.Symbol.Name)
		}
	}

	// Step 7: Risk guidance.
	risk := GenerateRiskGuidance(RiskInput{
		ChangedSymbols:  changed,
		ImpactResults:   impactResults,
		UntestedSymbols: untestedSymbols,
	})

	return &DeltaResult{
		ChangedFiles:    diffs,
		ChangedSymbols:  changed,
		ImpactedSymbols: allImpacted,
		UntestedSymbols: untestedSymbols,
		Risk:            risk,
		Tier:            cg.Tier,
	}, nil
}

// buildTestedSet returns names of symbols that have at least one test.
// Uses same naming conventions as codegraph/tested_by.go but works in-memory.
func buildTestedSet(symbols []*parser.Symbol) map[string]bool {
	tested := make(map[string]bool)
	for _, s := range symbols {
		name := s.Name
		switch s.Language {
		case "go":
			for _, prefix := range []string{"Test_", "Test", "Benchmark"} {
				if strings.HasPrefix(name, prefix) {
					rest := strings.TrimPrefix(name, prefix)
					if parts := strings.SplitN(rest, "_", 2); len(parts) > 0 && parts[0] != "" {
						tested[parts[0]] = true
					}
				}
			}
		case "python":
			if strings.HasPrefix(name, "test_") {
				tested[strings.TrimPrefix(name, "test_")] = true
			} else if strings.HasPrefix(name, "Test") {
				tested[strings.TrimPrefix(name, "Test")] = true
			}
		}
	}
	return tested
}

// enrichTestedSetViaOxCodes uses ox-codes scoped search to find test functions
// that reference changed symbols inside their bodies — catches table-driven tests,
// helper-wrapped calls, and other patterns that naming conventions miss.
func enrichTestedSetViaOxCodes(ctx context.Context, oc *oxcodes.Client, root string, changed []ChangedSymbol, testedSet map[string]bool) {
	for _, cs := range changed {
		if testedSet[cs.Symbol.Name] {
			continue // already known to be tested
		}
		// Search for symbol name inside function bodies in test files.
		resp, err := oc.SearchScoped(ctx, oxcodes.ScopedSearchInput{
			Root:       root,
			Pattern:    cs.Symbol.Name,
			Scope:      "function",
			Language:   cs.Symbol.Language,
			MaxResults: 5,
			ExcludeGlob: "",
		})
		if err != nil || resp == nil {
			continue
		}
		// Check if any match is in a test file.
		for _, m := range resp.Matches {
			if isTestFile(m.File) {
				testedSet[cs.Symbol.Name] = true
				break
			}
		}
	}
}

// isTestFile checks if a file path looks like a test file.
func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.js")
}

func dedup(items []ImpactedSymbol) []ImpactedSymbol {
	seen := make(map[string]bool)
	var out []ImpactedSymbol
	for _, item := range items {
		key := item.Name + ":" + item.File
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -run TestDelta -v`
Expected: PASS

- [ ] **Step 5: Run all review package tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/review/ -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd /home/krolik/src/go-code && git add internal/review/delta.go internal/review/delta_test.go
git commit -m "feat(review): add delta review orchestrator"
```

---

### Task 6: `review_delta` MCP Tool

**Files:**
- Create: `cmd/go-code/tool_review_delta.go`
- Modify: `cmd/go-code/register.go` (add `registerReviewDelta` call)

- [ ] **Step 1: Write MCP tool handler**

```go
// cmd/go-code/tool_review_delta.go
package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/review"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReviewDeltaInput is the input schema for the review_delta tool.
type ReviewDeltaInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full URL, or absolute local host path"`
	Base     string `json:"base,omitempty" jsonschema_description:"Base ref to diff against (commit SHA, branch, tag, HEAD~N). Default: HEAD~1"`
	Depth    int    `json:"depth,omitempty" jsonschema_description:"Impact traversal depth (default 2, max 5)"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
}

const (
	defaultReviewDepth = 2
	maxReviewDepth     = 5
)

func registerReviewDelta(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "review_delta",
		Description: "Analyze changes between two git refs and compute differential impact. " +
			"Returns changed files, changed symbols, impacted downstream symbols, " +
			"untested changes, and risk guidance. " +
			"Ideal for pre-merge review: shows blast radius of a branch's changes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewDeltaInput) (*mcp.CallToolResult, error) {
		return handleReviewDelta(ctx, input, deps)
	})
}

type xmlDeltaResponse struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Tier    string   `xml:"tier,attr,omitempty"`

	ChangedFiles    []xmlChangedFile   `xml:"changed_files>file"`
	ChangedSymbols  []xmlChangedSymbol `xml:"changed_symbols>symbol"`
	ImpactedSymbols []xmlImpacted      `xml:"impacted_symbols>symbol"`
	Untested        []string           `xml:"untested>symbol,omitempty"`
	Risk            xmlRisk            `xml:"risk"`
}

type xmlChangedFile struct {
	Path    string `xml:"path,attr"`
	Added   int    `xml:"added,attr"`
	Removed int    `xml:"removed,attr"`
}

type xmlChangedSymbol struct {
	Name       string `xml:"name,attr"`
	Kind       string `xml:"kind,attr"`
	File       string `xml:"file,attr"`
	ChangeType string `xml:"change,attr"`
}

type xmlImpacted struct {
	Name      string  `xml:"name,attr"`
	File      string  `xml:"file,attr"`
	Distance  int     `xml:"distance,attr"`
	ChangedBy string  `xml:"changed_by,attr"`
	Confidence float64 `xml:"confidence,attr"`
}

type xmlRisk struct {
	Level       string   `xml:"level,attr"`
	Score       float64  `xml:"score,attr"`
	Flags       []string `xml:"flag,omitempty"`
	Suggestions []string `xml:"suggestion,omitempty"`
}

func handleReviewDelta(ctx context.Context, input ReviewDeltaInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	depth := input.Depth
	if depth <= 0 {
		depth = defaultReviewDepth
	}
	if depth > maxReviewDepth {
		depth = maxReviewDepth
	}

	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:     root,
		Base:     input.Base,
		Depth:    depth,
		Language: input.Language,
		OxCodes:  deps.OxCodes,
	})
	if err != nil {
		return errResult(fmt.Sprintf("delta review: %s", err)), nil
	}

	resp := buildDeltaXML(result)
	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return textResult(string(data)), nil
}

func buildDeltaXML(r *review.DeltaResult) xmlDeltaResponse {
	resp := xmlDeltaResponse{
		Tool: "review_delta",
		Tier: r.Tier,
	}

	for _, f := range r.ChangedFiles {
		resp.ChangedFiles = append(resp.ChangedFiles, xmlChangedFile{
			Path: f.Path, Added: f.Added, Removed: f.Removed,
		})
	}
	for _, cs := range r.ChangedSymbols {
		resp.ChangedSymbols = append(resp.ChangedSymbols, xmlChangedSymbol{
			Name: cs.Symbol.Name, Kind: string(cs.Symbol.Kind),
			File: cs.FileDiff.Path, ChangeType: string(cs.ChangeType),
		})
	}
	for _, is := range r.ImpactedSymbols {
		resp.ImpactedSymbols = append(resp.ImpactedSymbols, xmlImpacted{
			Name: is.Name, File: is.File, Distance: is.Distance,
			ChangedBy: is.ChangedBy, Confidence: is.Confidence,
		})
	}
	resp.Untested = r.UntestedSymbols
	resp.Risk = xmlRisk{
		Level: r.Risk.RiskLevel, Score: r.Risk.RiskScore,
		Flags: r.Risk.Flags, Suggestions: r.Risk.Suggestions,
	}

	return resp
}
```

- [ ] **Step 2: Wire into register.go**

In `cmd/go-code/register.go`, add after `registerPrepareChange`:

```go
registerReviewDelta(server, cfg, deps)
```

- [ ] **Step 3: Build and verify compilation**

Run: `cd /home/krolik/src/go-code && go build ./cmd/go-code/`
Expected: SUCCESS (no compile errors)

- [ ] **Step 4: Run full test suite**

Run: `cd /home/krolik/src/go-code && go test ./... -count=1 2>&1 | tail -30`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-code && git add cmd/go-code/tool_review_delta.go cmd/go-code/register.go
git commit -m "feat: add review_delta MCP tool"
```

---

### Task 7: `review_pr` MCP Tool

**Files:**
- Create: `cmd/go-code/tool_review_pr.go`
- Modify: `cmd/go-code/register.go` (add `registerReviewPR` call)

- [ ] **Step 1: Write MCP tool handler**

```go
// cmd/go-code/tool_review_pr.go
package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/review"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReviewPRInput is the input schema for the review_pr tool.
type ReviewPRInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo) or full URL"`
	PR       int    `json:"pr" jsonschema_description:"Pull request number"`
	Depth    int    `json:"depth,omitempty" jsonschema_description:"Impact traversal depth (default 2, max 5)"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit to files of this language"`
}

func registerReviewPR(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "review_pr",
		Description: "Review a pull request: fetches PR metadata and diff, " +
			"then runs differential impact analysis on all changes. " +
			"Returns changed symbols, blast radius, untested code, and risk guidance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewPRInput) (*mcp.CallToolResult, error) {
		return handleReviewPR(ctx, input, deps)
	})
}

func handleReviewPR(ctx context.Context, input ReviewPRInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.PR <= 0 {
		return errResult("pr number is required"), nil
	}

	// Clone the repo.
	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	// Fetch PR ref and determine base.
	base, err := fetchPRBase(ctx, root, input.PR)
	if err != nil {
		// Fallback: use default branch comparison.
		base = "origin/main"
	}

	depth := input.Depth
	if depth <= 0 {
		depth = defaultReviewDepth
	}
	if depth > maxReviewDepth {
		depth = maxReviewDepth
	}

	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:     root,
		Base:     base,
		Depth:    depth,
		Language: input.Language,
		OxCodes:  deps.OxCodes,
	})
	if err != nil {
		return errResult(fmt.Sprintf("review: %s", err)), nil
	}

	resp := buildDeltaXML(result)
	resp.Tool = "review_pr"
	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return textResult(string(data)), nil
}

// fetchPRBase fetches the PR's merge base by fetching the PR ref.
func fetchPRBase(ctx context.Context, root string, prNumber int) (string, error) {
	// Fetch the PR head ref.
	ref := fmt.Sprintf("pull/%d/head", prNumber)
	_, err := review.GitExec(ctx, root, "fetch", "origin", ref)
	if err != nil {
		return "", fmt.Errorf("fetch PR ref: %w", err)
	}
	// Find merge base between FETCH_HEAD and origin/main.
	out, err := review.GitExec(ctx, root, "merge-base", "FETCH_HEAD", "origin/main")
	if err != nil {
		// Try origin/master.
		out, err = review.GitExec(ctx, root, "merge-base", "FETCH_HEAD", "origin/master")
		if err != nil {
			return "", fmt.Errorf("merge-base: %w", err)
		}
	}
	return strings.TrimSpace(out), nil
}
```

Note: export `gitExec` as `GitExec` in `internal/review/diff.go` so `tool_review_pr.go` can use it. Add `strings` to imports.

- [ ] **Step 2: Wire into register.go**

In `cmd/go-code/register.go`, add after `registerReviewDelta`:

```go
registerReviewPR(server, cfg, deps)
```

- [ ] **Step 3: Build and verify**

Run: `cd /home/krolik/src/go-code && go build ./cmd/go-code/`
Expected: SUCCESS

- [ ] **Step 4: Run full test suite**

Run: `cd /home/krolik/src/go-code && go test ./... -count=1 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/krolik/src/go-code && git add cmd/go-code/tool_review_pr.go cmd/go-code/register.go
git commit -m "feat: add review_pr MCP tool"
```

---

### Task 8: Integration Test + Deploy

**Files:**
- Modify: existing test infrastructure

- [ ] **Step 1: Run lint**

Run: `cd /home/krolik/src/go-code && make lint`
Expected: PASS (or fix any issues)

- [ ] **Step 2: Run full test suite**

Run: `cd /home/krolik/src/go-code && go test ./... -count=1 -race`
Expected: PASS

- [ ] **Step 3: Build Docker image**

Run: `cd ~/deploy/krolik-server && docker compose build --no-cache go-code`
Expected: SUCCESS

- [ ] **Step 4: Deploy**

Run: `cd ~/deploy/krolik-server && docker compose up -d --no-deps --force-recreate go-code`
Expected: Container starts, health check passes

- [ ] **Step 5: Verify new tools appear**

Run: `curl -s http://127.0.0.1:8897/mcp -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | python3 -m json.tool | grep -E 'review_delta|review_pr'`
Expected: Both tools listed

- [ ] **Step 6: Commit any remaining fixes and tag**

```bash
cd /home/krolik/src/go-code && git tag v1.19.0
```
