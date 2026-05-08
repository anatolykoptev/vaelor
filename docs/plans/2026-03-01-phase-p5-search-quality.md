# Code Search + Graph Intelligence (P5) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `code_search` MCP tool for regex/literal code search within repos, improve graph query quality with schema-aware classifier and example-enriched freeform Cypher, and reduce dead_code false positives with framework-aware heuristics.

**Architecture:** New `search` package for grep-like code search with file type filtering and context lines. Classifier prompt enriched with schema text so the LLM correctly routes INHERITS/IMPLEMENTS queries. Deadcode package enhanced with HTTP handler detection patterns (Chi, Gin, Echo, net/http) and interface-method exclusion.

**Tech Stack:** Go 1.26+, tree-sitter (existing), MCP SDK (existing), regex

---

### Task 1: Add `code_search` MCP tool

**Context:** Currently go-code has `symbol_search` (symbols by name pattern) and `repo_analyze` (deep analysis), but no tool to search for arbitrary code patterns (string literals, regex, error messages, TODO comments). This is the most-requested missing capability — users need grep-like search within repositories accessible via MCP.

**Files:**
- Create: `$REPO_ROOT/internal/codesearch/search.go`
- Create: `$REPO_ROOT/internal/codesearch/search_test.go`
- Create: `$REPO_ROOT/cmd/go-code/tool_code_search.go`
- Modify: `$REPO_ROOT/cmd/go-code/register.go` (add registration)

**Step 1: Write the failing test**

Create `$REPO_ROOT/internal/codesearch/search_test.go`:

```go
package codesearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSearch_LiteralMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n")
	writeFile(t, dir, "util.go", "package main\n\nfunc helper() string {\n\treturn \"hello world\"\n}\n")

	results, err := Search(context.Background(), SearchInput{
		Root:    dir,
		Pattern: "hello world",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(results))
	}
}

func TestSearch_RegexMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc handleUserCreate() {}\nfunc handleUserDelete() {}\nfunc otherFunc() {}\n")

	results, err := Search(context.Background(), SearchInput{
		Root:    dir,
		Pattern: "func handle[A-Z]\\w+",
		IsRegex: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(results))
	}
}

func TestSearch_FileFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "TODO: fix this\n")
	writeFile(t, dir, "readme.md", "TODO: update docs\n")
	writeFile(t, dir, "test.py", "# TODO: add tests\n")

	results, err := Search(context.Background(), SearchInput{
		Root:     dir,
		Pattern:  "TODO",
		FileGlob: "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 match (only .go), got %d", len(results))
	}
}

func TestSearch_ContextLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "line1\nline2\nMATCH\nline4\nline5\n")

	results, err := Search(context.Background(), SearchInput{
		Root:         dir,
		Pattern:      "MATCH",
		ContextLines: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatal("expected 1 match")
	}
	if len(results[0].Context) != 3 {
		t.Errorf("expected 3 context lines (before + match + after), got %d", len(results[0].Context))
	}
}

func TestSearch_MaxResults(t *testing.T) {
	dir := t.TempDir()
	content := ""
	for i := range 20 {
		content += "match_line\n"
		_ = i
	}
	writeFile(t, dir, "main.go", content)

	results, err := Search(context.Background(), SearchInput{
		Root:       dir,
		Pattern:    "match_line",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 matches (capped), got %d", len(results))
	}
}

func TestSearch_Empty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "nothing here\n")

	results, err := Search(context.Background(), SearchInput{
		Root:    dir,
		Pattern: "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(results))
	}
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

Run: `cd $REPO_ROOT && go test ./internal/codesearch/ -v`
Expected: FAIL — package does not exist

**Step 3: Implement search.go**

Create `$REPO_ROOT/internal/codesearch/search.go`:

```go
package codesearch

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

const (
	defaultMaxResults  = 100
	defaultMaxFileSize = 512 * 1024
)

// SearchInput controls what to search for and where.
type SearchInput struct {
	Root         string // repo root directory
	Pattern      string // search pattern (literal or regex)
	IsRegex      bool   // treat pattern as regex
	FileGlob     string // optional file glob filter (e.g. "*.go")
	Language     string // optional language filter
	ContextLines int    // lines of context before/after match (default 0)
	MaxResults   int    // max matches to return (default 100)
	CaseSensitive bool  // default true
}

// SearchMatch is a single match found in a file.
type SearchMatch struct {
	File    string   `json:"file"`    // relative path
	Line    int      `json:"line"`    // 1-based line number
	Text    string   `json:"text"`    // matched line content
	Context []string `json:"context"` // surrounding lines (if ContextLines > 0)
}

// Search performs a grep-like code search across all source files in root.
func Search(ctx context.Context, input SearchInput) ([]SearchMatch, error) {
	if input.MaxResults <= 0 {
		input.MaxResults = defaultMaxResults
	}

	re, err := buildPattern(input.Pattern, input.IsRegex, input.CaseSensitive)
	if err != nil {
		return nil, err
	}

	// Collect files to search.
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		MaxFileBytes: defaultMaxFileSize,
		Languages:    langs,
	})
	if err != nil {
		return nil, err
	}

	var matches []SearchMatch
	for _, f := range ir.Files {
		if ctx.Err() != nil {
			break
		}
		if input.FileGlob != "" {
			matched, _ := filepath.Match(input.FileGlob, filepath.Base(f.Path))
			if !matched {
				continue
			}
		}

		fileMatches := searchFile(f.Path, f.RelPath, re, input.ContextLines)
		for _, m := range fileMatches {
			matches = append(matches, m)
			if len(matches) >= input.MaxResults {
				return matches, nil
			}
		}
	}

	return matches, nil
}

func buildPattern(pattern string, isRegex, caseSensitive bool) (*regexp.Regexp, error) {
	if !isRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}

func searchFile(absPath, relPath string, re *regexp.Regexp, contextLines int) []SearchMatch {
	file, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var allLines []string
	var matchLineNums []int

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		allLines = append(allLines, line)
		if re.MatchString(line) {
			matchLineNums = append(matchLineNums, lineNum)
		}
	}

	var matches []SearchMatch
	for _, ln := range matchLineNums {
		m := SearchMatch{
			File: relPath,
			Line: ln,
			Text: allLines[ln-1],
		}
		if contextLines > 0 {
			start := ln - 1 - contextLines
			if start < 0 {
				start = 0
			}
			end := ln + contextLines
			if end > len(allLines) {
				end = len(allLines)
			}
			m.Context = allLines[start:end]
		}
		matches = append(matches, m)
	}

	return matches
}
```

**Step 4: Run tests to verify they pass**

Run: `cd $REPO_ROOT && go test ./internal/codesearch/ -v -count=1`
Expected: All PASS

**Step 5: Create MCP tool**

Create `$REPO_ROOT/cmd/go-code/tool_code_search.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeSearchInput is the input schema for the code_search tool.
type CodeSearchInput struct {
	Repo         string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Pattern      string `json:"pattern" jsonschema_description:"Search pattern (literal string or regex)"`
	IsRegex      bool   `json:"is_regex,omitempty" jsonschema_description:"Treat pattern as regular expression (default: literal)"`
	FileGlob     string `json:"file_glob,omitempty" jsonschema_description:"File glob filter (e.g. '*.go', '*.py')"`
	Language     string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python, typescript)"`
	ContextLines int    `json:"context_lines,omitempty" jsonschema_description:"Number of context lines before/after each match (default: 2)"`
	MaxResults   int    `json:"max_results,omitempty" jsonschema_description:"Maximum number of matches to return (default: 50, max: 200)"`
}

func registerCodeSearch(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "code_search",
		Description: "Search for code patterns within a repository. " +
			"Supports literal strings and regular expressions. " +
			"Returns matching lines with file paths, line numbers, and surrounding context. " +
			"Use for finding: TODO comments, error messages, function calls, string literals, " +
			"API endpoints, configuration patterns, or any text pattern in source code.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeSearchInput) (*mcp.CallToolResult, any, error) {
		return handleCodeSearch(ctx, input, deps)
	})
}

func handleCodeSearch(ctx context.Context, input CodeSearchInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}
	if input.Pattern == "" {
		return errResult("pattern is required"), nil, nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
	}
	defer cleanup()

	contextLines := input.ContextLines
	if contextLines <= 0 {
		contextLines = 2
	}
	if contextLines > 10 {
		contextLines = 10
	}
	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}

	matches, err := codesearch.Search(ctx, codesearch.SearchInput{
		Root:          root,
		Pattern:       input.Pattern,
		IsRegex:       input.IsRegex,
		FileGlob:      input.FileGlob,
		Language:      input.Language,
		ContextLines:  contextLines,
		MaxResults:    maxResults,
		CaseSensitive: true,
	})
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil, nil
	}

	type searchOutput struct {
		Pattern    string                  `json:"pattern"`
		IsRegex    bool                    `json:"is_regex"`
		MatchCount int                     `json:"match_count"`
		Matches    []codesearch.SearchMatch `json:"matches"`
	}

	output := searchOutput{
		Pattern:    input.Pattern,
		IsRegex:    input.IsRegex,
		MatchCount: len(matches),
		Matches:    matches,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
```

**Step 6: Register the tool**

In `$REPO_ROOT/cmd/go-code/register.go`, find the registration block and add:

```go
	registerCodeSearch(server, cfg, deps)
```

Add it after the existing `registerDeadCode` (or whichever is last). You'll need to read the file first to find the exact insertion point.

**Step 7: Build and verify**

Run: `cd $REPO_ROOT && go build ./...`
Expected: Success

Run: `cd $REPO_ROOT && go test ./internal/codesearch/ -v -count=1`
Expected: All PASS

**Step 8: Commit**

```bash
cd $REPO_ROOT
sudo -u $USER git add internal/codesearch/search.go internal/codesearch/search_test.go cmd/go-code/tool_code_search.go cmd/go-code/register.go
sudo -u $USER git commit -m "$(cat <<'EOF'
feat: add code_search MCP tool for grep-like code search

New codesearch package with regex/literal pattern matching, file glob
filtering, language filtering, and configurable context lines.
MCP tool supports GitHub repos and local paths.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Schema-aware classifier for graph queries

**Context:** The classifier prompt (`SystemPromptClassifyGraphQuery`) lists templates but doesn't include the graph schema. When queries mention INHERITS/IMPLEMENTS edges, the LLM doesn't know these exist and misroutes to `freeform`. Adding schema text and example queries to the classifier prompt improves template selection accuracy.

**Files:**
- Modify: `$REPO_ROOT/internal/codegraph/classify.go`
- Modify: `$REPO_ROOT/internal/prompts/prompts.go`
- Create: `$REPO_ROOT/internal/codegraph/classify_test.go` (add classification tests if doesn't exist, or extend)

**Step 1: Write failing test**

Add to or create `$REPO_ROOT/internal/codegraph/classify_test.go`:

```go
func TestClassify_SchemaAwarePrompt(t *testing.T) {
	// Verify the classifier system prompt now includes schema text.
	prompt := classifierSystemPrompt()
	if !strings.Contains(prompt, "INHERITS") {
		t.Error("classifier prompt should contain INHERITS edge from schema")
	}
	if !strings.Contains(prompt, "IMPLEMENTS") {
		t.Error("classifier prompt should contain IMPLEMENTS edge from schema")
	}
	if !strings.Contains(prompt, "Symbol") {
		t.Error("classifier prompt should contain Symbol vertex from schema")
	}
}
```

**Step 2: Update classifier prompt**

In `$REPO_ROOT/internal/prompts/prompts.go`, update `SystemPromptClassifyGraphQuery`:

Change the `%s` placeholder to accept two format args — templates list AND schema text:

```go
const SystemPromptClassifyGraphQuery = `You are a query classifier for a code knowledge graph.

Given a natural-language question about code, select the best matching template and extract parameters.

Graph schema:
%s

Available templates:
%s

Respond with ONLY a JSON object, no explanation:
{"template": "<template_id>", "params": {"param_name": "value"}}

If no template fits, respond:
{"template": "freeform", "params": {}}

Rules:
- Extract symbol/function/package names from the question into params
- Use "freeform" only if the question truly doesn't match any template
- Parameter values should be exact names from the question (case-sensitive)
- For type hierarchy questions (extends, implements, embeds), prefer inherits/implementations/type_hierarchy/subtypes templates
- For complexity questions, prefer complex_symbols or hotspots templates
- For PageRank/importance questions, prefer important_symbols template`
```

**Step 3: Update Classify to inject schema**

In `$REPO_ROOT/internal/codegraph/classify.go`, update the `Classify` function:

```go
func Classify(ctx context.Context, client llmCompleter, query string) (*Classification, error) {
	systemPrompt := classifierSystemPrompt()
	raw, err := client.Complete(ctx, systemPrompt, query)
	// ... rest unchanged
}

// classifierSystemPrompt builds the classifier prompt with schema and template list.
func classifierSystemPrompt() string {
	return fmt.Sprintf(prompts.SystemPromptClassifyGraphQuery, GraphSchemaText(), TemplateList())
}
```

**Step 4: Run tests**

Run: `cd $REPO_ROOT && go build ./...`
Expected: Success

Run: `cd $REPO_ROOT && go test ./internal/codegraph/ -run TestClassify -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
cd $REPO_ROOT
sudo -u $USER git add internal/codegraph/classify.go internal/codegraph/classify_test.go internal/prompts/prompts.go
sudo -u $USER git commit -m "$(cat <<'EOF'
feat(codegraph): inject graph schema into classifier prompt

Classifier LLM now sees vertex labels, edge labels, and their properties.
Improves routing accuracy for INHERITS/IMPLEMENTS/PageRank queries.
Added example-based routing hints for type hierarchy templates.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Deadcode framework-aware heuristics

**Context:** The current dead_code analysis has high false-positive rate for HTTP handler functions (registered via Chi/Gin/Echo routers), interface method implementations, and wire-injected functions. These functions have zero CALLS edges but ARE used via reflection/routing registration. Adding framework detection heuristics reduces noise.

**Files:**
- Modify: `$REPO_ROOT/internal/deadcode/deadcode.go`
- Modify: `$REPO_ROOT/internal/deadcode/deadcode_test.go`

**Step 1: Write failing tests**

Add to `$REPO_ROOT/internal/deadcode/deadcode_test.go`:

```go
func TestAnalyze_HTTPHandlerNotDead(t *testing.T) {
	// HTTP handler registered via router should not be flagged as dead.
	symbols := []*parser.Symbol{
		{Name: "handleUserCreate", Kind: parser.KindFunction, File: "/app/handlers.go", StartLine: 10, EndLine: 20,
			Signature: "func handleUserCreate(w http.ResponseWriter, r *http.Request)"},
		{Name: "handleHealth", Kind: parser.KindFunction, File: "/app/handlers.go", StartLine: 30, EndLine: 35,
			Signature: "func handleHealth(w http.ResponseWriter, r *http.Request)"},
		{Name: "reallyDead", Kind: parser.KindFunction, File: "/app/util.go", StartLine: 1, EndLine: 5,
			Signature: "func reallyDead()"},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: nil}

	result := Analyze(cg, Options{})
	// handleUserCreate and handleHealth have HTTP handler signatures → excluded
	// reallyDead has no callers and no handler signature → dead
	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead function (reallyDead), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s (%s)", d.Name, d.Confidence)
		}
	}
}

func TestAnalyze_InterfaceMethodNotDead(t *testing.T) {
	// Method on a type that implements an interface should be low confidence.
	symbols := []*parser.Symbol{
		{Name: "ServeHTTP", Kind: parser.KindMethod, File: "/app/server.go", StartLine: 10, EndLine: 20,
			Signature: "func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)"},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: nil}

	result := Analyze(cg, Options{})
	// ServeHTTP is a well-known interface method → should be excluded or low confidence
	for _, d := range result.DeadSymbols {
		if d.Name == "ServeHTTP" {
			t.Error("ServeHTTP should not be flagged as dead (interface method)")
		}
	}
}
```

**Step 2: Implement framework heuristics**

In `$REPO_ROOT/internal/deadcode/deadcode.go`, add heuristic functions:

```go
// httpHandlerSignatures are patterns that identify HTTP handler functions.
var httpHandlerPatterns = []string{
	"http.ResponseWriter",
	"*http.Request",
	"gin.Context",
	"echo.Context",
	"fiber.Ctx",
	"chi.Router",
}

// wellKnownInterfaceMethods are method names commonly required by interfaces.
var wellKnownInterfaceMethods = map[string]bool{
	"ServeHTTP":     true,
	"String":        true,
	"Error":         true,
	"MarshalJSON":   true,
	"UnmarshalJSON": true,
	"Close":         true,
	"Read":          true,
	"Write":         true,
	"Len":           true,
	"Less":          true,
	"Swap":          true,
}

// isHTTPHandler checks if a symbol's signature indicates it's an HTTP handler.
func isHTTPHandler(sym *parser.Symbol) bool {
	sig := sym.Signature
	for _, pattern := range httpHandlerPatterns {
		if strings.Contains(sig, pattern) {
			return true
		}
	}
	return false
}

// isWellKnownInterfaceMethod checks if the function name matches a well-known interface method.
func isWellKnownInterfaceMethod(sym *parser.Symbol) bool {
	return sym.Kind == parser.KindMethod && wellKnownInterfaceMethods[sym.Name]
}
```

Then update the `Analyze` function to skip these in the dead code list (or mark them with `Confidence: "excluded"`). Read the existing `Analyze` function first to understand its structure, then add the checks in the filtering logic where dead symbols are identified.

**Step 3: Run tests**

Run: `cd $REPO_ROOT && go test ./internal/deadcode/ -v -count=1`
Expected: All PASS

**Step 4: Commit**

```bash
cd $REPO_ROOT
sudo -u $USER git add internal/deadcode/deadcode.go internal/deadcode/deadcode_test.go
sudo -u $USER git commit -m "$(cat <<'EOF'
feat(deadcode): add framework-aware heuristics to reduce false positives

Skip HTTP handlers (net/http, Gin, Echo, Fiber, Chi signature patterns).
Skip well-known interface methods (ServeHTTP, String, Error, MarshalJSON,
Read, Write, Close, sort interface methods).
Reduces false positive rate for web applications.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Enrich freeform Cypher generation with examples

**Context:** When the classifier falls back to `freeform`, the `GenerateCypher` function uses `SystemPromptGenerateCypher` which has the schema but no example queries. Adding concrete Cypher examples for the newer edge types (INHERITS, IMPLEMENTS) and property names (complexity, pagerank, lines) helps the LLM generate correct queries. Also, Apache AGE doesn't support `|` pipe syntax in edge types — the examples teach the LLM to use `WHERE type(r)` instead.

**Files:**
- Modify: `$REPO_ROOT/internal/prompts/prompts.go`
- Modify: `$REPO_ROOT/internal/codegraph/generate_test.go` (add test)

**Step 1: Write failing test**

Add to `$REPO_ROOT/internal/codegraph/generate_test.go`:

```go
func TestCypherSystemPrompt_ContainsExamples(t *testing.T) {
	prompt := cypherSystemPrompt()
	// Should contain AGE-compatible examples showing WHERE type(r) pattern
	if !strings.Contains(prompt, "type(r)") {
		t.Error("freeform prompt should contain type(r) AGE-compatible example")
	}
	if !strings.Contains(prompt, "INHERITS") {
		t.Error("freeform prompt should mention INHERITS edge")
	}
	if !strings.Contains(prompt, "pagerank") {
		t.Error("freeform prompt should mention pagerank property")
	}
}
```

**Step 2: Update the Cypher generation prompt**

In `$REPO_ROOT/internal/prompts/prompts.go`, update `SystemPromptGenerateCypher`:

```go
const SystemPromptGenerateCypher = `You are a Cypher query generator for a code knowledge graph stored in Apache AGE.

Graph schema:
%s

IMPORTANT Apache AGE constraints:
- Do NOT use [:TYPE1|TYPE2] pipe syntax — AGE does not support it
- Instead use: MATCH ()-[r]->() WHERE type(r) = 'TYPE1' OR type(r) = 'TYPE2'
- Variable-length paths work with single types: [:CALLS*1..5]
- OPTIONAL MATCH is supported
- Use single quotes for string values in WHERE clauses

Example queries:
- Find callers: MATCH (caller:Symbol)-[:CALLS]->(target:Symbol {name: 'handleRequest'}) RETURN caller
- Type parents: MATCH (child:Symbol {name: 'Dog'})-[r]->(parent:Symbol) WHERE type(r) = 'INHERITS' OR type(r) = 'IMPLEMENTS' RETURN parent.name, parent.file, type(r) AS relation
- Complex functions: MATCH (s:Symbol) WHERE s.kind IN ['function', 'method'] AND s.complexity IS NOT NULL RETURN s.name, s.file, s.complexity ORDER BY s.complexity DESC LIMIT 10
- Important symbols: MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL RETURN s.name, s.kind, s.file, s.pagerank ORDER BY s.pagerank DESC LIMIT 20
- Call chain: MATCH path = shortestPath((a:Symbol {name: 'main'})-[:CALLS*..10]->(b:Symbol {name: 'query'})) RETURN path

Generate a READ-ONLY Cypher query. Do NOT use CREATE, DELETE, SET, MERGE, REMOVE, or DROP.

Respond with ONLY the Cypher query, no explanation.`
```

**Step 3: Run tests**

Run: `cd $REPO_ROOT && go build ./...`
Expected: Success

Run: `cd $REPO_ROOT && go test ./internal/codegraph/ -run TestCypherSystemPrompt -v -count=1`
Expected: PASS

**Step 4: Commit**

```bash
cd $REPO_ROOT
sudo -u $USER git add internal/prompts/prompts.go internal/codegraph/generate_test.go
sudo -u $USER git commit -m "$(cat <<'EOF'
feat(codegraph): add example queries and AGE constraints to freeform prompt

Freeform Cypher generation now includes concrete examples for INHERITS,
IMPLEMENTS, complexity, and pagerank queries. Documents AGE limitation
(no pipe syntax) with correct WHERE type(r) alternative.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Lint + Full Test Pass + Deploy

**Files:** None new — validation only.

**Step 1: Run all tests**

```bash
cd $REPO_ROOT && go test ./internal/codesearch/ -v -count=1
cd $REPO_ROOT && go test ./internal/codegraph/ -v -count=1
cd $REPO_ROOT && go test ./internal/deadcode/ -v -count=1
cd $REPO_ROOT && go test ./... -count=1
```
Expected: All PASS

**Step 2: Build**

```bash
cd $REPO_ROOT && go build ./...
```

**Step 3: Deploy**

```bash
cd ~/deploy/my-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 4: Health check**

Run: `curl -s http://127.0.0.1:8897/health`
Expected: healthy with latest commit hash

**Step 5: Smoke test**

Test 1 — Verify `code_search` tool:
- Initialize MCP session
- Call `code_search` with pattern "func main" on go-code repo
- Verify matches returned with file paths and context

Test 2 — Verify improved classifier:
- Call `code_graph` with "what types inherit from Result?"
- Verify it routes to `implementations` template (not freeform)

Test 3 — Verify deadcode improvements:
- Call `dead_code` on go-code repo
- Verify HTTP handlers are not in the dead list

**Step 6: Push to origin**

```bash
cd $REPO_ROOT
sudo -u $USER git push origin main
```

---

## Summary of Changes

| Task | What | Files | New LOC (approx) |
|------|------|-------|-------------------|
| 1 | `code_search` MCP tool | search.go, search_test.go, tool_code_search.go, register.go | +300 |
| 2 | Schema-aware classifier | classify.go, classify_test.go, prompts.go | +30 |
| 3 | Deadcode framework heuristics | deadcode.go, deadcode_test.go | +80 |
| 4 | Freeform Cypher examples | prompts.go, generate_test.go | +30 |
| 5 | Lint + test + deploy | - | - |
| **Total** | | **~5 new files, ~4 modified** | **~440 LOC** |

## Not in Scope (deferred to P6+)

- Semantic search via embeddings (needs vector DB)
- Identifier-level reference graph (Aider-style personalized PageRank)
- AST diff integration (smacker/gum)
- Compound `explore` tool (combines multiple analyses)
- Incremental indexing exposure via MCP parameter
- SCIP backend for Go type-aware analysis
