# Semantic Search Quality Improvements

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve semantic_search result relevance by filtering noise, adding context to embeddings, and cutting irrelevant hits.

**Architecture:** Four independent improvements to the embeddings pipeline and search handler. Each modifies a single file or adds a small filter. No schema changes needed.

**Tech Stack:** Go, pgvector, jina-code-v2 embeddings (768 dim)

---

### Task 1: Distance threshold — filter irrelevant results

**Problem:** All top-K results are returned regardless of similarity. A query like "JWT validation" in a repo with no auth code returns random functions with distance ~0.9.

**Files:**
- Modify: `internal/embeddings/store.go` (Search method)
- Modify: `cmd/go-code/tool_semantic_search.go` (SemanticSearchInput, handleSemanticSearch)
- Test: `internal/embeddings/store_test.go`

**Step 1: Write failing test**

In `internal/embeddings/store_test.go`, add test `TestSearch_DistanceThreshold`:
- Insert 3 embeddings with known vectors
- Search with a query vector that's close to one (distance ~0.1) and far from others (distance ~0.8)
- Set `MaxDistance: 0.5`
- Assert only the close result is returned

```go
func TestSearch_DistanceThreshold(t *testing.T) {
    // Use a test pool (skip if no DATABASE_URL)
    // Insert vectors: v1 = [1,0,0,...], v2 = [0,1,0,...], v3 = [0,0,1,...]
    // Query = [0.9, 0.1, 0, ...] — close to v1
    // MaxDistance = 0.5
    // Expect: only v1 returned
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/embeddings/ -run TestSearch_DistanceThreshold -v`
Expected: FAIL — `MaxDistance` field doesn't exist

**Step 3: Add `MaxDistance` to SearchOpts and filter in SQL**

In `store.go`, add field to `SearchOpts`:
```go
type SearchOpts struct {
    RepoKey     string
    Language    string
    TopK        int
    MaxDistance  float32 // 0 = no filter; cosine distance threshold (0.0-1.0)
}
```

In `Search()`, add WHERE clause:
```go
if opts.MaxDistance > 0 {
    where = append(where, fmt.Sprintf("embedding <=> $1 < $%d", len(args)+1))
    args = append(args, opts.MaxDistance)
}
```

**Step 4: Wire default threshold in handler**

In `tool_semantic_search.go`, add `MaxDistance` to `SemanticSearchInput`:
```go
MaxDistance float32 `json:"max_distance,omitempty" jsonschema_description:"Maximum cosine distance (0.0-1.0, default 0.75). Lower = stricter matching"`
```

In `handleSemanticSearch`, set default:
```go
maxDist := input.MaxDistance
if maxDist <= 0 {
    maxDist = 0.75
}
```

Pass to `SearchOpts{MaxDistance: maxDist}`.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/embeddings/ -run TestSearch_DistanceThreshold -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/embeddings/store.go internal/embeddings/store_test.go cmd/go-code/tool_semantic_search.go
git commit -m "feat(semantic_search): add distance threshold to filter irrelevant results"
```

---

### Task 2: Filter test files from indexing

**Problem:** `_test.go` files (and equivalents in other languages) are indexed as symbols. Search for "HTTP handler" returns `TestHandleRequest` alongside `HandleRequest`, polluting results.

**Files:**
- Modify: `internal/embeddings/pipeline_helpers.go` (collectSymbols)
- Test: `internal/embeddings/pipeline_helpers_test.go`

**Step 1: Write failing test**

In `pipeline_helpers_test.go`, add `TestCollectSymbols_SkipsTestFiles`:
- Create temp dir with 3 files: `handler.go`, `handler_test.go`, `main.go`
- Each file has a function
- Call `collectSymbols`
- Assert: 2 symbols returned (from handler.go and main.go), 0 from handler_test.go

```go
func TestCollectSymbols_SkipsTestFiles(t *testing.T) {
    dir := t.TempDir()
    writeFile(t, dir, "handler.go", `package main
func Handle() {}`)
    writeFile(t, dir, "handler_test.go", `package main
import "testing"
func TestHandle(t *testing.T) {}`)
    writeFile(t, dir, "main.go", `package main
func main() {}`)

    syms, files, err := collectSymbols(context.Background(), dir)
    require.NoError(t, err)
    assert.Equal(t, 2, len(syms))
    for _, f := range files {
        assert.NotContains(t, f.RelPath, "_test.go")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/embeddings/ -run TestCollectSymbols_SkipsTestFiles -v`
Expected: FAIL — 3 symbols returned

**Step 3: Add test file filter**

In `pipeline_helpers.go`, add filter function and use it in `collectSymbols`:

```go
// isTestFile returns true for test/spec files that should be excluded from indexing.
func isTestFile(relPath string) bool {
    base := filepath.Base(relPath)
    // Go
    if strings.HasSuffix(base, "_test.go") {
        return true
    }
    // Python
    if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
        return true
    }
    // JS/TS
    for _, suffix := range []string{".test.js", ".test.ts", ".test.tsx", ".spec.js", ".spec.ts", ".spec.tsx"} {
        if strings.HasSuffix(base, suffix) {
            return true
        }
    }
    // Rust
    if base == "tests.rs" || strings.Contains(relPath, "/tests/") {
        return true
    }
    return false
}
```

In `collectSymbols`, before parsing:
```go
for _, f := range ir.Files {
    if isTestFile(f.RelPath) {
        continue
    }
    // ... existing parsing code
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/embeddings/ -run TestCollectSymbols_SkipsTestFiles -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/embeddings/pipeline_helpers.go internal/embeddings/pipeline_helpers_test.go
git commit -m "feat(semantic_search): filter test files from embedding index"
```

---

### Task 3: Add file path context to embed text

**Problem:** `buildEmbedText` produces `"go function Handle: func Handle(w, r)\n{body}"`. Two functions named `Handle` in different packages are embedded identically. Adding file path gives the model path context.

**Files:**
- Modify: `internal/embeddings/pipeline_helpers.go` (buildEmbedText signature)
- Modify: `internal/embeddings/pipeline.go` (call site)
- Test: `internal/embeddings/pipeline_helpers_test.go`

**Step 1: Write failing test**

In `pipeline_helpers_test.go`, add `TestBuildEmbedText_IncludesFilePath`:
```go
func TestBuildEmbedText_IncludesFilePath(t *testing.T) {
    sym := &parser.Symbol{
        Language: "go", Kind: parser.KindFunction,
        Name: "Handle", Signature: "func Handle()", Body: "{}",
    }
    text := buildEmbedText(sym, "pkg/api/handler.go")
    assert.Contains(t, text, "pkg/api/handler.go")
    assert.Contains(t, text, "Handle")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/embeddings/ -run TestBuildEmbedText_IncludesFilePath -v`
Expected: FAIL — `buildEmbedText` takes 1 arg

**Step 3: Add file path to embed text**

In `pipeline_helpers.go`:
```go
func buildEmbedText(sym *parser.Symbol, filePath string) string {
    text := fmt.Sprintf("%s %s %s %s: %s\n%s",
        filePath, sym.Language, sym.Kind, sym.Name, sym.Signature, sym.Body)
    if len(text) > maxEmbedText {
        return text[:maxEmbedText]
    }
    return text
}
```

In `pipeline.go` `embedAndUpsert`, update call:
```go
texts[i] = buildEmbedText(e.sym, e.file.RelPath)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/embeddings/ -run TestBuildEmbedText_IncludesFilePath -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/embeddings/pipeline_helpers.go internal/embeddings/pipeline.go internal/embeddings/pipeline_helpers_test.go
git commit -m "feat(semantic_search): include file path in embed text for better context"
```

---

### Task 4: Smart body truncation

**Problem:** `buildEmbedText` truncates at 2000 chars blindly, potentially cutting mid-line or keeping only the function signature for long functions. Better: keep signature + first N meaningful lines, skip blank lines and comments at end.

**Files:**
- Modify: `internal/embeddings/pipeline_helpers.go` (buildEmbedText)
- Test: `internal/embeddings/pipeline_helpers_test.go`

**Step 1: Write failing test**

In `pipeline_helpers_test.go`, add `TestBuildEmbedText_SmartTruncation`:
```go
func TestBuildEmbedText_SmartTruncation(t *testing.T) {
    // Create body with 300 lines of "x := 1\n"
    longBody := strings.Repeat("x := 1\n", 300)
    sym := &parser.Symbol{
        Language: "go", Kind: parser.KindFunction,
        Name: "Big", Signature: "func Big()", Body: longBody,
    }
    text := buildEmbedText(sym, "main.go")
    // Must not exceed maxEmbedText
    assert.LessOrEqual(t, len(text), maxEmbedText)
    // Must end at a line boundary (not mid-line)
    assert.True(t, text[len(text)-1] == '\n' || !strings.Contains(text[len(text)-10:], "\n"),
        "should truncate at line boundary")
    // Must contain the signature
    assert.Contains(t, text, "func Big()")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/embeddings/ -run TestBuildEmbedText_SmartTruncation -v`
Expected: FAIL — truncation is mid-character

**Step 3: Implement line-boundary truncation**

In `pipeline_helpers.go`:
```go
func buildEmbedText(sym *parser.Symbol, filePath string) string {
    header := fmt.Sprintf("%s %s %s %s: %s\n", filePath, sym.Language, sym.Kind, sym.Name, sym.Signature)
    remaining := maxEmbedText - len(header)
    if remaining <= 0 {
        return header[:maxEmbedText]
    }
    body := sym.Body
    if len(body) > remaining {
        // Truncate at last newline within budget.
        cut := strings.LastIndex(body[:remaining], "\n")
        if cut > 0 {
            body = body[:cut+1]
        } else {
            body = body[:remaining]
        }
    }
    return header + body
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/embeddings/ -run TestBuildEmbedText_SmartTruncation -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/embeddings/pipeline_helpers.go internal/embeddings/pipeline_helpers_test.go
git commit -m "feat(semantic_search): smart body truncation at line boundaries"
```

---

### Post-Implementation: Re-index repos

After deploying, existing embeddings use old format (no file path, no distance filter). To benefit from Tasks 2-4, re-index:

```sql
-- Clear old embeddings to force re-index
TRUNCATE code_embeddings;
```

The auto-indexer (`AUTO_INDEX_DIRS`) will re-index on next `semantic_search` call or background scan.
