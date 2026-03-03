# Focus Keyword Fallback — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When `focus` contains spaces (e.g. "mcpserver auth middleware"), treat it as keyword filter instead of path filter — matching against directory/file path components case-insensitively. Currently returns 0 files.

**Architecture:** Single function `isKeywordFocus()` detects keyword mode (contains spaces). New `matchKeywords()` function matches all keywords against relPath. Applied in `handleFile` with zero change to path-based focus behavior. Tool descriptions updated to document both modes.

**Tech Stack:** Go, tree-sitter (unchanged), ingest package

**Root cause:** `focus` is passed to `filepath.Match` and `strings.HasPrefix` — neither matches "mcpserver auth middleware" against any real file path. README is read separately (not through ingest), so it appears while files=0.

---

### Task 1: Add keyword focus matching to ingest

**Files:**
- Modify: `internal/ingest/ingest.go:173-178` (handleFile focus block)
- Test: `internal/ingest/ingest_test.go`

**Step 1: Write the failing test**

Add to `internal/ingest/ingest_test.go`:

```go
// TestIngestRepoFocusKeywords verifies keyword-based focus (spaces = keywords, not path).
func TestIngestRepoFocusKeywords(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cmd", "server", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal", "auth", "middleware.go"), "package auth\n")
	writeFile(t, filepath.Join(root, "internal", "handler", "routes.go"), "package handler\n")
	writeFile(t, filepath.Join(root, "pkg", "models", "user.go"), "package models\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "auth middleware",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	if len(result.Files) == 0 {
		t.Fatal("keyword focus returned 0 files, expected internal/auth/middleware.go")
	}

	for _, f := range result.Files {
		rp := strings.ToLower(f.RelPath)
		if !strings.Contains(rp, "auth") || !strings.Contains(rp, "middleware") {
			t.Errorf("keyword focus: unexpected file %q (should contain auth AND middleware)", f.RelPath)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/ingest/ -run TestIngestRepoFocusKeywords -v`
Expected: FAIL — 0 files returned (current path-based focus can't match "auth middleware")

**Step 3: Implement keyword focus in handleFile**

Replace the focus block in `internal/ingest/ingest.go:173-178`:

```go
	if opts.Focus != "" {
		if isKeywordFocus(opts.Focus) {
			if !matchKeywords(opts.Focus, relPath) {
				return false, nil
			}
		} else {
			matched, _ := filepath.Match(opts.Focus, relPath)
			if !matched && !strings.HasPrefix(relPath, opts.Focus) {
				return false, nil
			}
		}
	}
```

Add two helper functions (before `containsString`):

```go
// isKeywordFocus returns true when focus looks like a keyword query
// rather than a path or glob pattern. Heuristic: contains spaces.
func isKeywordFocus(focus string) bool {
	return strings.Contains(focus, " ")
}

// matchKeywords checks whether ALL space-separated keywords appear
// somewhere in the relPath (case-insensitive). Keywords match against
// directory names and file name components.
func matchKeywords(focus, relPath string) bool {
	lower := strings.ToLower(relPath)
	for _, kw := range strings.Fields(focus) {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}
```

**Step 4: Run tests to verify**

Run: `cd /home/krolik/src/go-code && go test ./internal/ingest/ -run TestIngestRepo -v`
Expected: ALL pass — keyword test passes, existing Focus test still passes

**Step 5: Commit**

```bash
cd /home/krolik/src/go-code
git add internal/ingest/ingest.go internal/ingest/ingest_test.go
git commit -m "feat(ingest): keyword fallback for focus parameter

When focus contains spaces, match all keywords against file path
case-insensitively instead of using path prefix/glob. Fixes explore
returning 0 files when LLM passes semantic focus like 'auth middleware'.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Add more keyword focus edge-case tests

**Files:**
- Test: `internal/ingest/ingest_test.go`

**Step 1: Write edge-case tests**

```go
// TestIngestRepoFocusKeywordsNoMatch verifies keyword focus returns 0 when no file matches all keywords.
func TestIngestRepoFocusKeywordsNoMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "internal", "auth", "login.go"), "package auth\n")
	writeFile(t, filepath.Join(root, "internal", "handler", "middleware.go"), "package handler\n")

	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "auth middleware",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}

	// "auth" is in one path, "middleware" in another — no single file has both.
	if len(result.Files) != 0 {
		names := make([]string, len(result.Files))
		for i, f := range result.Files {
			names[i] = f.RelPath
		}
		t.Errorf("expected 0 files, got %d: %v", len(result.Files), names)
	}
}

// TestIngestRepoFocusPathUnchanged verifies path-based focus still works exactly as before.
func TestIngestRepoFocusPathUnchanged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cmd", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal", "auth", "handler.go"), "package auth\n")

	// Path focus (no spaces) — existing behavior.
	result, err := IngestRepo(context.Background(), IngestOpts{
		Root:  root,
		Focus: "internal",
	})
	if err != nil {
		t.Fatalf("IngestRepo: %v", err)
	}
	if len(result.Files) != 1 {
		t.Errorf("path focus: expected 1 file, got %d", len(result.Files))
	}
	for _, f := range result.Files {
		if !strings.HasPrefix(f.RelPath, "internal") {
			t.Errorf("path focus: file %q not under internal/", f.RelPath)
		}
	}
}
```

**Step 2: Run all ingest tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/ingest/ -v`
Expected: ALL pass

**Step 3: Commit**

```bash
cd /home/krolik/src/go-code
git add internal/ingest/ingest_test.go
git commit -m "test(ingest): edge cases for keyword focus

No-match scenario (keywords split across files) and path-focus
regression test.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Update focus descriptions in all tool schemas

**Files:**
- Modify: `cmd/go-code/tool_explore.go:17`
- Modify: `cmd/go-code/tool_repo_analyze.go:32`
- Modify: `cmd/go-code/tool_call_trace.go:75`
- Modify: `cmd/go-code/tool_impact.go:20`
- Modify: `cmd/go-code/tool_code_health.go:32`
- Modify: `cmd/go-code/tool_dep_graph.go:34`
- Modify: `cmd/go-code/tool_dead_code.go:45` (keep as-is, LLM narrative)

**Step 1: Update each description**

New standard description for path-filter tools (explore, repo_analyze, call_trace, impact, code_health):

```
Subdirectory path to limit scope (e.g. internal/auth, pkg/api), or space-separated keywords to filter by path components (e.g. 'auth middleware')
```

For code_compare (already good, just add keyword note):
```
Subdirectory path filter to limit comparison scope (e.g. internal/auth, pkg/api), or keywords (e.g. 'auth handler'). Use query for topic focus.
```

For dep_graph:
```
Package or subdirectory to focus on (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')
```

Dead_code: keep current description ("Optional focus area for the LLM narrative") — it's not a path filter.

**Step 2: Verify build compiles**

Run: `cd /home/krolik/src/go-code && go build ./cmd/go-code/`
Expected: success

**Step 3: Commit**

```bash
cd /home/krolik/src/go-code
git add cmd/go-code/tool_explore.go cmd/go-code/tool_repo_analyze.go \
  cmd/go-code/tool_call_trace.go cmd/go-code/tool_impact.go \
  cmd/go-code/tool_code_health.go cmd/go-code/tool_dep_graph.go \
  cmd/go-code/tool_code_compare.go
git commit -m "docs(tools): clarify focus param accepts keywords with spaces

All tools now document that focus accepts either a subdirectory path
or space-separated keywords for path-component matching.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Deploy and verify

**Step 1: Run full test suite**

Run: `cd /home/krolik/src/go-code && go test ./... 2>&1 | tail -20`
Expected: all packages PASS

**Step 2: Deploy**

```bash
cd ~/deploy/krolik-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Verify the fix**

```
explore repo=/home/krolik/src/go-billing focus="mcpserver auth middleware"
```

Expected: file_count > 0 (files with "auth" and "middleware" in path should match)

Also verify path-based focus still works:
```
explore repo=/home/krolik/src/go-code focus=internal
```

Expected: only files under `internal/` returned
