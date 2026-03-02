# Compound Explore Tool + Quality-of-Life (P6) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a compound `explore` MCP tool that combines repo overview, graph stats, dead code summary, and dependency highlights into a single call; plus QoL improvements — case-insensitive code_search, code_graph description update, and `file_parse` complexity output.

**Architecture:** New `explore` package orchestrates existing analysis packages (ingest, parser, callgraph, deadcode, codegraph, codesearch) in parallel where possible. The tool returns a structured JSON "dossier" covering architecture, hotspots, dead code, key symbols, and dependency stats. Remaining tasks are small targeted edits to existing tool handlers.

**Tech Stack:** Go 1.26+, existing packages (no new dependencies)

---

### Task 1: Add `explore` MCP tool

**Context:** Users currently need 3-5 separate tool calls to understand a new codebase (repo_analyze overview, dead_code, code_graph stats, dep_graph). The `explore` tool combines these into one call returning a comprehensive dossier. It does NOT use the LLM — it's a fast, structured summary.

**Files:**
- Create: `/path/to/repos/src/go-code/internal/explore/explore.go`
- Create: `/path/to/repos/src/go-code/internal/explore/explore_test.go`
- Create: `/path/to/repos/src/go-code/cmd/go-code/tool_explore.go`
- Modify: `/path/to/repos/src/go-code/cmd/go-code/register.go`

**Step 1: Write the failing test**

Create `/path/to/repos/src/go-code/internal/explore/explore_test.go`:

```go
package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExplore_BasicRepo(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	fmt.Println(helper())
}

func helper() string {
	return "hello"
}

func unused() {}
`)
	writeGoFile(t, dir, "util.go", `package main

func anotherHelper() string {
	return "world"
}
`)

	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount < 2 {
		t.Errorf("expected at least 2 files, got %d", result.FileCount)
	}
	if result.SymbolCount < 3 {
		t.Errorf("expected at least 3 symbols, got %d", result.SymbolCount)
	}
	if len(result.Languages) == 0 {
		t.Error("expected at least one language")
	}
	if result.Languages[0].Name != "go" {
		t.Errorf("expected go language, got %s", result.Languages[0].Name)
	}
}

func TestExplore_DeadCodeSection(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

func main() {}

func deadFunc() {}

func anotherDead() {}
`)

	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatal(err)
	}

	if result.DeadCode == nil {
		t.Fatal("expected dead code section")
	}
	if result.DeadCode.Count < 1 {
		t.Errorf("expected at least 1 dead function, got %d", result.DeadCode.Count)
	}
}

func TestExplore_TopSymbols(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

func main() {
	a()
	b()
}

func a() { c() }
func b() { c() }
func c() {}
`)

	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.TopSymbols) == 0 {
		t.Error("expected at least one top symbol")
	}
}

func TestExplore_LanguageFilter(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

func main() {}
`)
	writeFile(t, dir, "app.py", `def main():
    pass
`)

	result, err := Run(context.Background(), Input{Root: dir, Language: "go"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Languages) != 1 {
		t.Errorf("expected 1 language with filter, got %d", len(result.Languages))
	}
}

func writeGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	writeFile(t, dir, name, content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/explore/ -v`
Expected: FAIL — package does not exist

**Step 3: Implement explore.go**

Create `/path/to/repos/src/go-code/internal/explore/explore.go`:

```go
package explore

import (
	"context"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// Input controls explore parameters.
type Input struct {
	Root     string
	Language string
	Focus    string
}

// Result is the structured explore output.
type Result struct {
	FileCount   int              `json:"file_count"`
	SymbolCount int              `json:"symbol_count"`
	TotalLines  int              `json:"total_lines"`
	Languages   []LanguageStat   `json:"languages"`
	TopSymbols  []SymbolSummary  `json:"top_symbols"`
	DeadCode    *DeadCodeSummary `json:"dead_code,omitempty"`
	Packages    []string         `json:"packages"`
}

// LanguageStat summarises one language in the repo.
type LanguageStat struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Ratio float64 `json:"ratio"`
}

// SymbolSummary is a top symbol by call count.
type SymbolSummary struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	CallCount int    `json:"call_count"`
}

// DeadCodeSummary is a compact dead-code report.
type DeadCodeSummary struct {
	Count   int      `json:"count"`
	Samples []string `json:"samples"`
}

const (
	maxTopSymbols     = 15
	maxDeadSamples    = 10
	maxPackages       = 50
	defaultMaxFileBytes int64 = 512 * 1024
)

// Run performs the explore analysis.
func Run(ctx context.Context, input Input) (*Result, error) {
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Focus:        input.Focus,
		Languages:    langs,
		MaxFileBytes: defaultMaxFileBytes,
	})
	if err != nil {
		return nil, err
	}

	// Parse all files.
	var allSymbols []*parser.Symbol
	langCount := make(map[string]int)
	totalLines := 0

	for _, f := range ir.Files {
		langCount[f.Language]++
		pr := parser.ParseFile(f.Path, f.Language)
		if pr != nil {
			allSymbols = append(allSymbols, pr.Symbols...)
		}
	}

	// Estimate total lines from file sizes.
	for _, f := range ir.Files {
		totalLines += estimateLines(f.Size)
	}

	// Language stats.
	var langStats []LanguageStat
	for lang, count := range langCount {
		langStats = append(langStats, LanguageStat{
			Name:  lang,
			Files: count,
			Ratio: float64(count) / float64(len(ir.Files)),
		})
	}
	sort.Slice(langStats, func(i, j int) bool {
		return langStats[i].Files > langStats[j].Files
	})

	// Build call graph for top symbols and dead code.
	cg := callgraph.Build(allSymbols)

	// Top symbols by incoming call count.
	callCounts := make(map[string]int)
	symByKey := make(map[string]*parser.Symbol)
	for _, sym := range allSymbols {
		key := sym.Name + ":" + sym.File
		symByKey[key] = sym
	}
	for _, edge := range cg.Edges {
		if edge.Callee != nil {
			key := edge.Callee.Name + ":" + edge.Callee.File
			callCounts[key]++
		}
	}

	type symCount struct {
		sym   *parser.Symbol
		count int
	}
	var ranked []symCount
	for key, count := range callCounts {
		if sym, ok := symByKey[key]; ok {
			ranked = append(ranked, symCount{sym: sym, count: count})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].count > ranked[j].count
	})

	var topSymbols []SymbolSummary
	for i, r := range ranked {
		if i >= maxTopSymbols {
			break
		}
		relFile := r.sym.File
		if len(input.Root) > 0 && len(r.sym.File) > len(input.Root) {
			relFile = r.sym.File[len(input.Root)+1:]
		}
		topSymbols = append(topSymbols, SymbolSummary{
			Name:      r.sym.Name,
			Kind:      string(r.sym.Kind),
			File:      relFile,
			CallCount: r.count,
		})
	}

	// Dead code.
	dcResult := deadcode.Analyze(cg, deadcode.Options{})
	var dcSummary *DeadCodeSummary
	if dcResult.DeadCount > 0 {
		samples := make([]string, 0, maxDeadSamples)
		for i, d := range dcResult.DeadSymbols {
			if i >= maxDeadSamples {
				break
			}
			samples = append(samples, d.Name)
		}
		dcSummary = &DeadCodeSummary{
			Count:   dcResult.DeadCount,
			Samples: samples,
		}
	}

	// Packages (unique directories).
	pkgSet := make(map[string]struct{})
	for _, f := range ir.Files {
		dir := f.RelPath
		if idx := lastSlash(dir); idx >= 0 {
			dir = dir[:idx]
		}
		pkgSet[dir] = struct{}{}
	}
	var packages []string
	for p := range pkgSet {
		packages = append(packages, p)
	}
	sort.Strings(packages)
	if len(packages) > maxPackages {
		packages = packages[:maxPackages]
	}

	return &Result{
		FileCount:   len(ir.Files),
		SymbolCount: len(allSymbols),
		TotalLines:  totalLines,
		Languages:   langStats,
		TopSymbols:  topSymbols,
		DeadCode:    dcSummary,
		Packages:    packages,
	}, nil
}

func estimateLines(sizeBytes int64) int {
	const avgBytesPerLine = 35
	if sizeBytes <= 0 {
		return 0
	}
	return int(sizeBytes / avgBytesPerLine)
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/explore/ -v -count=1`
Expected: All PASS

NOTE: If the `callgraph.Build` function signature doesn't exist, look at how `callgraph.BuildFromRepo` works in `tool_dead_code.go`. You may need to use `callgraph.BuildFromRepo` with a `TraceRepoInput` instead, or build the call graph via `callgraph.Build(symbols)`. Read `/path/to/repos/src/go-code/internal/callgraph/` to find the correct function. The `deadcode.Analyze` function takes `*callgraph.CallGraph` — check its signature too.

**Step 5: Create MCP tool**

Create `/path/to/repos/src/go-code/cmd/go-code/tool_explore.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/explore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExploreInput is the input schema for the explore tool.
type ExploreInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory or glob to focus analysis on"`
}

func registerExplore(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "explore",
		Description: "Quick structured overview of a repository. " +
			"Returns file/symbol counts, language breakdown, top symbols by call frequency, " +
			"dead code summary, and package list. " +
			"Use as a first step when encountering an unfamiliar codebase. " +
			"Fast (no LLM calls) — purely static analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ExploreInput) (*mcp.CallToolResult, any, error) {
		return handleExplore(ctx, input, deps)
	})
}

func handleExplore(ctx context.Context, input ExploreInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
	}
	defer cleanup()

	result, err := explore.Run(ctx, explore.Input{
		Root:     root,
		Language: input.Language,
		Focus:    input.Focus,
	})
	if err != nil {
		return errResult(fmt.Sprintf("explore: %s", err)), nil, nil
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
```

**Step 6: Register the tool**

In `/path/to/repos/src/go-code/cmd/go-code/register.go`, add after the last `register*` call:

```go
	registerExplore(server, cfg, deps)
```

**Step 7: Build and verify**

Run: `cd /path/to/repos/src/go-code && go build ./...`
Expected: Success

Run: `cd /path/to/repos/src/go-code && go test ./internal/explore/ -v -count=1`
Expected: All PASS

**Step 8: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/explore/explore.go internal/explore/explore_test.go cmd/go-code/tool_explore.go cmd/go-code/register.go
sudo -u example git commit -m "$(cat <<'EOF'
feat: add explore MCP tool for quick repository overview

Compound tool combining file stats, language breakdown, top symbols
by call frequency, dead code summary, and package list. No LLM calls —
fast static analysis for initial codebase understanding.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Add case-insensitive search to code_search

**Context:** The `code_search` tool currently always searches case-sensitive. Users often need case-insensitive search (e.g. finding TODO/todo/Todo). Add a `case_sensitive` parameter to the MCP input (default true for backwards compat).

**Files:**
- Modify: `/path/to/repos/src/go-code/cmd/go-code/tool_code_search.go`
- Modify: `/path/to/repos/src/go-code/internal/codesearch/search_test.go` (add test if not present)

**Step 1: Write failing test**

The test `TestSearch_CaseInsensitive` may already exist (was added by P5 subagent). Check first. If not:

```go
func TestSearch_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "Hello World\nhello world\nHELLO WORLD\n")

	results, err := Search(context.Background(), SearchInput{
		Root:          dir,
		Pattern:       "hello world",
		CaseSensitive: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 case-insensitive matches, got %d", len(results))
	}
}
```

**Step 2: Update MCP tool input**

In `/path/to/repos/src/go-code/cmd/go-code/tool_code_search.go`, add field to `CodeSearchInput`:

```go
	CaseSensitive *bool `json:"case_sensitive,omitempty" jsonschema_description:"Case-sensitive matching (default: true). Set false for case-insensitive."`
```

Use `*bool` so we can distinguish "not set" (→ true) from "set to false".

In `handleCodeSearch`, update the `CaseSensitive` logic:

```go
	caseSensitive := true
	if input.CaseSensitive != nil {
		caseSensitive = *input.CaseSensitive
	}
```

And pass `caseSensitive` to `codesearch.Search`.

**Step 3: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/codesearch/ -v -count=1`
Expected: All PASS

Run: `cd /path/to/repos/src/go-code && go build ./...`
Expected: Success

**Step 4: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add cmd/go-code/tool_code_search.go internal/codesearch/search_test.go
sudo -u example git commit -m "$(cat <<'EOF'
feat(codesearch): expose case_sensitive parameter in code_search MCP tool

Defaults to true for backwards compat. Set false for case-insensitive
matching (TODO/todo/Todo).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Update code_graph description with new edge types

**Context:** The `code_graph` tool description still says "CONTAINS and CALLS edges" — it's missing INHERITS, IMPLEMENTS, HANDLES, FETCHES, BELONGS_TO, IMPORTS. This causes users to not know they can query type hierarchies or API routes.

**Files:**
- Modify: `/path/to/repos/src/go-code/cmd/go-code/tool_code_graph.go`

**Step 1: Update the description**

In `registerCodeGraph`, change the `Description` field:

```go
		Description: "Query a persistent code knowledge graph backed by Apache AGE. " +
			"Indexes the repository as a property graph with vertices (Package, File, Symbol, Layer, Route) " +
			"and edges (CONTAINS, CALLS, INHERITS, IMPLEMENTS, IMPORTS, HANDLES, FETCHES, BELONGS_TO). " +
			"Answers natural-language questions using Cypher query templates or LLM-generated Cypher. " +
			"Ideal for: call chains, type hierarchies, dependency analysis, dead code detection, " +
			"API route mapping, cross-language connections, and coupling analysis. " +
			"Results include raw graph rows and an LLM narrative.",
```

**Step 2: Build**

Run: `cd /path/to/repos/src/go-code && go build ./...`
Expected: Success

**Step 3: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add cmd/go-code/tool_code_graph.go
sudo -u example git commit -m "$(cat <<'EOF'
fix(code_graph): update tool description with all vertex and edge types

Description now lists all 5 vertex types and 8 edge types. Mentions
type hierarchies, API route mapping, and cross-language connections.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Add complexity to file_parse symbol output

**Context:** The `file_parse` tool returns symbols with name/kind/signature/lines but NOT complexity. Complexity is already computed in `codegraph/graph_build.go` via `symbolComplexity()`. Extract that function to a shared location and use it in `file_parse` output.

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/parser/symbol.go` (or wherever Symbol struct is defined) — add Complexity field
- Modify: `/path/to/repos/src/go-code/internal/codegraph/graph_build.go` — extract symbolComplexity to parser package
- Modify: `/path/to/repos/src/go-code/cmd/go-code/tool_file_parse.go` — include complexity in output

**Step 1: Read current code**

Read:
- `/path/to/repos/src/go-code/internal/parser/types.go` (or wherever Symbol is defined)
- `/path/to/repos/src/go-code/internal/codegraph/graph_build.go` (find `symbolComplexity`)
- `/path/to/repos/src/go-code/cmd/go-code/tool_file_parse.go`

**Step 2: Add Complexity to Symbol**

In the parser package, add `Complexity int` field to the `Symbol` struct.

**Step 3: Move symbolComplexity to parser package**

Create `/path/to/repos/src/go-code/internal/parser/complexity.go`:

```go
package parser

import "strings"

// Complexity estimates cyclomatic complexity from a function body.
// Counts branching keywords: if, else, for, switch, case, select, &&, ||.
func Complexity(body string) int {
	if body == "" {
		return 0
	}
	complexity := 1 // base path
	keywords := []string{" if ", "\tif ", "} else ", " for ", "\tfor ", " switch ", "\tswitch ", " case ", "\tcase ", " select ", "\tselect ", " && ", " || "}
	for _, kw := range keywords {
		complexity += strings.Count(body, kw)
	}
	return complexity
}
```

Update `codegraph/graph_build.go` to call `parser.Complexity(sym.Body)` instead of `symbolComplexity(sym.Body)`, or simply have `symbolComplexity` delegate to `parser.Complexity`.

**Step 4: Populate Complexity in ParseFile**

After symbols are extracted in `parser.ParseFile`, compute complexity for functions/methods:

```go
for _, sym := range result.Symbols {
	if (sym.Kind == KindFunction || sym.Kind == KindMethod) && sym.Body != "" {
		sym.Complexity = Complexity(sym.Body)
	}
}
```

**Step 5: Include in file_parse output**

In `tool_file_parse.go`, if the render format includes symbols, add complexity to the output string. Read the file first to understand the render format.

**Step 6: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1`
Run: `cd /path/to/repos/src/go-code && go build ./...`
Expected: All PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/parser/complexity.go internal/parser/types.go internal/codegraph/graph_build.go cmd/go-code/tool_file_parse.go
sudo -u example git commit -m "$(cat <<'EOF'
feat(parser): add cyclomatic complexity to symbol output

Extract symbolComplexity to parser.Complexity(). Populate on functions
and methods during ParseFile. Include in file_parse tool output.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Lint + Full Test Pass + Deploy

**Files:** None new — validation only.

**Step 1: Run all tests**

```bash
cd /path/to/repos/src/go-code && go test ./... -count=1
```
Expected: All PASS

**Step 2: Build**

```bash
cd /path/to/repos/src/go-code && go build ./...
```

**Step 3: Deploy**

```bash
cd ~/deploy/example-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 4: Health check**

Run: `curl -s http://127.0.0.1:8897/health`
Expected: healthy with latest commit hash

**Step 5: Smoke test**

Test 1 — Verify `explore` tool:
- Initialize MCP session
- Call `explore` with go-code repo
- Verify JSON response contains file_count, languages, top_symbols, dead_code

Test 2 — Verify `code_search` case-insensitive:
- Call `code_search` with `case_sensitive: false` and a mixed-case pattern

**Step 6: Push to origin**

```bash
cd /path/to/repos/src/go-code
sudo -u example git push origin main
```

---

## Summary of Changes

| Task | What | Files | New LOC (approx) |
|------|------|-------|-------------------|
| 1 | `explore` compound MCP tool | explore.go, explore_test.go, tool_explore.go, register.go | +350 |
| 2 | Case-insensitive code_search | tool_code_search.go, search_test.go | +15 |
| 3 | code_graph description update | tool_code_graph.go | +5 |
| 4 | Complexity in file_parse output | complexity.go, types.go, graph_build.go, tool_file_parse.go | +60 |
| 5 | Lint + test + deploy | — | — |
| **Total** | | **~4 new files, ~5 modified** | **~430 LOC** |

## Not in Scope (deferred to P7+)

- Semantic search via embeddings (needs vector DB)
- AST diff integration (smacker/gum)
- SCIP backend for Go type-aware analysis
- Multi-repo cross-service analysis
- Query result caching for code_graph
- Streaming Cypher results for large repos
