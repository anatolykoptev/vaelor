# Reusable Content-Focus Fallback — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract `contentFilter` from `internal/explore/focus.go` into a shared `internal/ingest` package, then add the same content-based fallback to `compare.BuildSnapshot` so that `code_compare` doesn't return empty/abstract results when `focus` keywords don't match file paths.

**Architecture:** Move content-filter logic to `internal/ingest/focus.go` (new file). The explore package switches to calling `ingest.ContentFilter`. The compare package adds the same fallback pattern: when path-based focus returns 0 files, re-ingest without focus, parse all files, then filter by symbol/import/call content. `RepoSnapshot.FocusMode` field added so callers know a fallback was used.

**Tech Stack:** Go, tree-sitter parser (unchanged), ingest + compare + explore packages

---

### Task 1: Create `internal/ingest/focus.go` with shared content filter

**Files:**
- Create: `internal/ingest/focus.go`
- Test: `internal/ingest/focus_test.go`

**Step 1: Write the failing test**

Create `internal/ingest/focus_test.go`:

```go
package ingest

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestContentFilter_MatchesBySymbolName(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "NewLLMClient", File: "/repo/llm/client.go"},
		{Name: "HandleHTTP", File: "/repo/http/handler.go"},
	}

	matched := ContentFilter("llm", symbols, nil, nil)

	if !matched["/repo/llm/client.go"] {
		t.Error("expected llm/client.go to match keyword 'llm'")
	}
	if matched["/repo/http/handler.go"] {
		t.Error("http/handler.go should not match keyword 'llm'")
	}
}

func TestContentFilter_MatchesByImport(t *testing.T) {
	imports := map[string][]string{
		"/repo/main.go": {"github.com/prometheus/client_golang/prometheus"},
		"/repo/util.go": {"fmt", "strings"},
	}

	matched := ContentFilter("prometheus", nil, imports, nil)

	if !matched["/repo/main.go"] {
		t.Error("expected main.go to match via prometheus import")
	}
	if matched["/repo/util.go"] {
		t.Error("util.go should not match keyword 'prometheus'")
	}
}

func TestContentFilter_MatchesByCallSite(t *testing.T) {
	calls := []parser.CallSite{
		{Name: "Retry", Receiver: "backoff", File: "/repo/retry.go"},
		{Name: "Println", Receiver: "fmt", File: "/repo/main.go"},
	}

	matched := ContentFilter("retry", nil, nil, calls)

	if !matched["/repo/retry.go"] {
		t.Error("expected retry.go to match via call site name 'Retry'")
	}
	if matched["/repo/main.go"] {
		t.Error("main.go should not match keyword 'retry'")
	}
}

func TestContentFilter_ORLogic(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "NewLLMClient", File: "/repo/llm.go"},
		{Name: "RetryWithBackoff", File: "/repo/retry.go"},
		{Name: "HandleHTTP", File: "/repo/http.go"},
	}

	matched := ContentFilter("llm retry", symbols, nil, nil)

	if !matched["/repo/llm.go"] {
		t.Error("llm.go should match keyword 'llm'")
	}
	if !matched["/repo/retry.go"] {
		t.Error("retry.go should match keyword 'retry'")
	}
	if matched["/repo/http.go"] {
		t.Error("http.go should not match either keyword")
	}
}

func TestContentFilter_EmptyFocus(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "Foo", File: "/repo/foo.go"},
	}
	matched := ContentFilter("", symbols, nil, nil)
	if matched != nil {
		t.Error("empty focus should return nil")
	}
}

func TestContentFilter_CaseInsensitive(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "NewMetricsCollector", File: "/repo/metrics.go"},
	}

	matched := ContentFilter("METRICS", symbols, nil, nil)
	if !matched["/repo/metrics.go"] {
		t.Error("should match case-insensitively")
	}
}

func TestFilterFiles_ByMatchSet(t *testing.T) {
	files := []*File{
		{Path: "/repo/a.go", RelPath: "a.go"},
		{Path: "/repo/b.go", RelPath: "b.go"},
		{Path: "/repo/c.go", RelPath: "c.go"},
	}
	matched := map[string]bool{"/repo/a.go": true, "/repo/c.go": true}

	result := FilterFiles(files, matched)
	if len(result) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result))
	}
	paths := map[string]bool{}
	for _, f := range result {
		paths[f.Path] = true
	}
	if !paths["/repo/a.go"] || !paths["/repo/c.go"] {
		t.Errorf("unexpected files: %v", result)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/ingest/ -run "TestContentFilter|TestFilterFiles_ByMatchSet" -v`
Expected: FAIL — `ContentFilter` and `FilterFiles` not defined

**Step 3: Write `internal/ingest/focus.go`**

```go
package ingest

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ContentFilter returns file paths where ANY keyword from focus matches
// a symbol name, import path, or call site (case-insensitive, OR logic).
// Returns nil when focus is empty or has no keywords.
func ContentFilter(focus string, symbols []*parser.Symbol, imports map[string][]string, calls []parser.CallSite) map[string]bool {
	keywords := strings.Fields(strings.ToLower(focus))
	if len(keywords) == 0 {
		return nil
	}

	symsByFile := groupSymbolsByFile(symbols)
	callsByFile := groupCallsByFile(calls)

	allFiles := make(map[string]struct{})
	for path := range symsByFile {
		allFiles[path] = struct{}{}
	}
	for path := range imports {
		allFiles[path] = struct{}{}
	}
	for path := range callsByFile {
		allFiles[path] = struct{}{}
	}

	matched := make(map[string]bool)
	for path := range allFiles {
		if fileMatchesAnyKeyword(symsByFile[path], imports[path], callsByFile[path], keywords) {
			matched[path] = true
		}
	}
	return matched
}

// FilterFiles returns only files whose absolute path is in the matched set.
func FilterFiles(files []*File, matched map[string]bool) []*File {
	if len(matched) == 0 {
		return nil
	}
	out := make([]*File, 0, len(matched))
	for _, f := range files {
		if matched[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

func groupSymbolsByFile(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		m[s.File] = append(m[s.File], s)
	}
	return m
}

func groupCallsByFile(calls []parser.CallSite) map[string][]parser.CallSite {
	m := make(map[string][]parser.CallSite)
	for _, c := range calls {
		m[c.File] = append(m[c.File], c)
	}
	return m
}

func fileMatchesAnyKeyword(syms []*parser.Symbol, imps []string, fileCalls []parser.CallSite, keywords []string) bool {
	for _, kw := range keywords {
		if kwInSymbols(syms, kw) || kwInImports(imps, kw) || kwInCalls(fileCalls, kw) {
			return true
		}
	}
	return false
}

func kwInSymbols(syms []*parser.Symbol, kw string) bool {
	for _, s := range syms {
		if strings.Contains(strings.ToLower(s.Name), kw) {
			return true
		}
	}
	return false
}

func kwInImports(imps []string, kw string) bool {
	for _, imp := range imps {
		if strings.Contains(strings.ToLower(imp), kw) {
			return true
		}
	}
	return false
}

func kwInCalls(calls []parser.CallSite, kw string) bool {
	for _, c := range calls {
		if strings.Contains(strings.ToLower(c.Name), kw) || strings.Contains(strings.ToLower(c.Receiver), kw) {
			return true
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/ingest/ -run "TestContentFilter|TestFilterFiles_ByMatchSet" -v`
Expected: ALL pass

**Step 5: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/ingest/focus.go internal/ingest/focus_test.go
git commit -m "feat(ingest): extract reusable ContentFilter for content-based focus

Shared module that matches focus keywords against symbol names, import
paths, and call sites (OR logic, case-insensitive). Moved from explore
package so compare can reuse the same fallback logic.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Migrate `explore` to use shared `ingest.ContentFilter`

**Files:**
- Delete: `internal/explore/focus.go` (contents moved to ingest)
- Modify: `internal/explore/explore.go:98-118` (switch to ingest calls)

**Step 1: Update `explore.go` to use `ingest.ContentFilter` and `ingest.FilterFiles`**

In `internal/explore/explore.go`, replace lines 96-118:

```go
	var focusMode string

	// Content-based fallback: when focus matches no file paths,
	// re-ingest all files and filter by symbol names, imports, and calls.
	if len(ir.Files) == 0 && input.Focus != "" {
		irAll, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
			Root:         input.Root,
			Languages:    langs,
			MaxFileBytes: maxFileBytes,
		})
		if err != nil {
			return nil, err
		}

		prAll, err := parseAllFiles(ctx, irAll.Files)
		if err != nil {
			return nil, err
		}

		matched := ingest.ContentFilter(input.Focus, prAll.symbols, prAll.imports, prAll.calls)
		ir.Files = ingest.FilterFiles(irAll.Files, matched)
		focusMode = "content"
	}
```

Then delete `internal/explore/focus.go` entirely.

**Step 2: Run explore tests to verify no regression**

Run: `cd /path/to/repos/src/go-code && go test ./internal/explore/ -v`
Expected: ALL pass

**Step 3: Run full test suite**

Run: `cd /path/to/repos/src/go-code && go test ./... 2>&1 | tail -20`
Expected: ALL pass

**Step 4: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/explore/explore.go
git rm internal/explore/focus.go
git commit -m "refactor(explore): switch to shared ingest.ContentFilter

Remove duplicate contentFilter from explore package, now imported
from ingest. Zero behavior change — same OR-logic keyword matching.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Add content-based fallback to `compare.BuildSnapshot`

**Files:**
- Modify: `internal/compare/snapshot.go:46-65` (BuildSnapshot function)
- Modify: `internal/compare/compare.go:124-128` (RepoSnapshot — add FocusMode field)
- Test: `internal/compare/snapshot_test.go`

**Step 1: Write the failing test**

Add to `internal/compare/snapshot_test.go`:

```go
func TestBuildSnapshot_ContentFallback(t *testing.T) {
	root := findRepoRoot(t)

	// "CompareRepos parser" won't match any file path, but should match
	// symbol names like CompareRepos, ParseFile, etc. via content fallback.
	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Focus: "CompareRepos parser",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot content fallback: %v", err)
	}

	if snap.FileCount == 0 {
		t.Error("content fallback: FileCount = 0, expected files matching symbol names")
	}

	if snap.FocusMode != "content" {
		t.Errorf("FocusMode = %q, want %q", snap.FocusMode, "content")
	}
}

func TestBuildSnapshot_PathFocusNoFallback(t *testing.T) {
	root := findRepoRoot(t)

	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Focus: "internal/parser",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot path focus: %v", err)
	}

	if snap.FileCount == 0 {
		t.Error("path focus should match files under internal/parser")
	}

	if snap.FocusMode != "" {
		t.Errorf("FocusMode = %q, want empty (path focus should not trigger fallback)", snap.FocusMode)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run "TestBuildSnapshot_ContentFallback|TestBuildSnapshot_PathFocusNoFallback" -v`
Expected: FAIL — `FocusMode` field doesn't exist, no content fallback

**Step 3: Add `FocusMode` to `RepoSnapshot`**

In `internal/compare/compare.go`, add field to `RepoSnapshot` struct (after `Root`):

```go
	// FocusMode is "content" when the fallback content filter was used
	// instead of path-based focus. Empty means path-based or no focus.
	FocusMode string `json:"focusMode,omitempty"`
```

**Step 4: Add content fallback to `BuildSnapshot`**

Replace `BuildSnapshot` in `internal/compare/snapshot.go`:

```go
func BuildSnapshot(ctx context.Context, root string, opts SnapshotOpts) (*RepoSnapshot, error) {
	var langs []string
	if opts.Language != "" {
		langs = []string{opts.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		Focus:        opts.Focus,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}

	var focusMode string

	// Content-based fallback: when focus matches no file paths,
	// re-ingest all files and filter by symbol names, imports, and calls.
	if len(ir.Files) == 0 && opts.Focus != "" {
		ir, focusMode, err = contentFallback(ctx, root, langs, opts.Focus)
		if err != nil {
			return nil, err
		}
	}

	parsed := parseSnapshotFiles(ctx, ir.Files)
	snap := buildSnapshotResult(root, ir, parsed)
	snap.FocusMode = focusMode

	return snap, nil
}

// contentFallback re-ingests the entire repo and filters files by symbol,
// import, and call-site content matching the focus keywords.
func contentFallback(ctx context.Context, root string, langs []string, focus string) (*ingest.IngestResult, string, error) {
	irAll, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		Languages:    langs,
		MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		return nil, "", fmt.Errorf("ingest repo (fallback): %w", err)
	}

	symbols, imports, calls := parseForContentFilter(ctx, irAll.Files)
	matched := ingest.ContentFilter(focus, symbols, imports, calls)
	irAll.Files = ingest.FilterFiles(irAll.Files, matched)

	return irAll, "content", nil
}

// parseForContentFilter does a lightweight parse to extract symbols, imports,
// and calls for content-based filtering. Bodies are not needed.
func parseForContentFilter(ctx context.Context, files []*ingest.File) ([]*parser.Symbol, map[string][]string, []parser.CallSite) {
	var allSymbols []*parser.Symbol
	imports := make(map[string][]string, len(files))
	var allCalls []parser.CallSite

	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		source, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		opts := parser.ParseOpts{
			Language:       f.Language,
			IncludeBody:    false,
			IncludeImports: true,
		}
		pr, err := parser.ParseFile(f.Path, source, opts)
		if err != nil {
			continue
		}
		allSymbols = append(allSymbols, pr.Symbols...)
		if len(pr.Imports) > 0 {
			imports[f.Path] = pr.Imports
		}
		calls, _ := parser.ExtractCalls(f.Path, source, opts)
		allCalls = append(allCalls, calls...)
	}

	return allSymbols, imports, allCalls
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestBuildSnapshot -v`
Expected: ALL pass including new content fallback tests

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/compare/snapshot.go internal/compare/compare.go internal/compare/snapshot_test.go
git commit -m "feat(compare): content-based fallback for focus in BuildSnapshot

When focus keywords don't match any file paths, re-ingests the full
repo and filters by symbol names, imports, and call sites using
shared ingest.ContentFilter (OR logic). Fixes code_compare returning
abstract LLM results when focus contains semantic terms like
'llm, metrics, retry'.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Run full test suite and deploy

**Step 1: Run full test suite**

Run: `cd /path/to/repos/src/go-code && go test ./... 2>&1 | tail -30`
Expected: ALL packages pass

**Step 2: Deploy**

```bash
cd ~/deploy/example-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Verify health**

```bash
curl http://127.0.0.1:8897/health
```

**Step 4: Verify the fix with go-code MCP**

Test content fallback works in code_compare:
```
code_compare repo_a=/path/to/repos/src/go-kit repo_b=/path/to/repos/src/go-engine focus="llm metrics retry" query="Compare llm, metrics, and retry packages"
```

Expected: matchedSymbols > 0, real file-level analysis with symbol bodies, not abstract LLM fluff.

Test path focus still works:
```
code_compare repo_a=/path/to/repos/src/go-kit repo_b=/path/to/repos/src/go-engine focus="pkg/llm"
```

Expected: only files under `pkg/llm/` compared.

**Step 5: Commit and push**

```bash
cd /path/to/repos/src/go-code
git push origin HEAD
```
