# API Consistency Improvements — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 5 parameter naming inconsistencies in go-code MCP tools that cause LLM errors.

**Architecture:** Additive aliases on existing tool input structs + error message improvement in go-mcpserver. All changes backward-compatible, no breaking changes.

**Tech Stack:** Go 1.26, go-mcpserver (vendored), mcp-go-sdk v1.4.0

---

### Task 1: `code_search` — add `path` alias for `file_glob`

**Files:**
- Modify: `cmd/go-code/tool_code_search.go:15-26` (CodeSearchInput struct)
- Modify: `cmd/go-code/tool_code_search.go:63-73` (handleCodeSearch handler)

**Step 1: Add `Path` field to `CodeSearchInput`**

In `cmd/go-code/tool_code_search.go`, add after `FileGlob` field (line ~20):

```go
type CodeSearchInput struct {
	Repo          string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Pattern       string `json:"pattern,omitempty" jsonschema_description:"Search pattern (literal string or regex). Use pattern or query."`
	Query         string `json:"query,omitempty" jsonschema_description:"Alias for pattern — use either query or pattern"`
	IsRegex       bool   `json:"is_regex,omitempty" jsonschema_description:"Treat pattern as regular expression (default: literal)"`
	FileGlob      string `json:"file_glob,omitempty" jsonschema_description:"File glob filter (e.g. '*.go', 'internal/auth/*.go')"`
	Path          string `json:"path,omitempty" jsonschema_description:"Directory path filter — alias for file_glob (e.g. 'internal/query'). Converted to file_glob automatically."`
	Language      string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python, typescript)"`
	ContextLines  int    `json:"context_lines,omitempty" jsonschema_description:"Number of context lines before/after each match (default: 2)"`
	MaxResults    int    `json:"max_results,omitempty" jsonschema_description:"Maximum number of matches to return (default: 50, max: 200)"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty" jsonschema_description:"Case-sensitive matching (default: true). Set false for case-insensitive."`
	ExcludeGlob   string `json:"exclude_glob,omitempty" jsonschema_description:"Comma-separated glob patterns to exclude files (e.g. 'docs/*,vendor/*'). Matches against relative paths."`
}
```

**Step 2: Add path→file_glob conversion in handler**

In `handleCodeSearch`, after the `query→pattern` alias block (line ~68-70), add:

```go
	// Allow "path" as alias for "file_glob" — LLMs often use path instead.
	if input.Path != "" && input.FileGlob == "" {
		input.FileGlob = input.Path + "/**"
	}
```

**Step 3: Build and verify**

Run: `cd /path/to/repos/src/go-code && make build`
Expected: compiles without errors.

**Step 4: Commit**

```bash
git add cmd/go-code/tool_code_search.go
git commit -m "feat(code_search): add path alias for file_glob"
```

---

### Task 2: `dep_graph` — add `depth` alias for `max_depth`

**Files:**
- Modify: `cmd/go-code/tool_dep_graph.go:24-46` (DepGraphInput struct)
- Modify: `cmd/go-code/tool_dep_graph.go:60-79` (handler body)
- Modify: `cmd/go-code/tool_dep_graph.go:53-59` (tool description)

**Step 1: Add `Depth` field, keep `MaxDepth` as deprecated alias**

Replace `DepGraphInput` struct:

```go
type DepGraphInput struct {
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	Type string `json:"type,omitempty" jsonschema_description:"Graph type: imports | packages | modules | calls (default: packages)"`

	Format string `json:"format,omitempty" jsonschema_description:"Output format: json | dot | mermaid | summary (default: mermaid)"`

	Focus string `json:"focus,omitempty" jsonschema_description:"Package or subdirectory to focus on (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`

	// Depth limits graph traversal depth from focused node.
	Depth int `json:"depth,omitempty" jsonschema_description:"Max traversal depth from focus node (default: 3, 0=unlimited)"`

	// MaxDepth is a deprecated alias for Depth.
	MaxDepth int `json:"max_depth,omitempty" jsonschema_description:"Deprecated: use depth instead"`

	IncludeStdlib bool `json:"include_stdlib,omitempty" jsonschema_description:"Include standard library imports in graph. Default false (stdlib excluded)."`

	CrossLanguage bool `json:"cross_language,omitempty" jsonschema_description:"Include cross-language API route connections between layers"`
}
```

**Step 2: Add max_depth→depth fallback in handler**

In the handler, after `repo` validation (line ~62), add:

```go
		// Support deprecated max_depth alias.
		if input.MaxDepth > 0 && input.Depth == 0 {
			input.Depth = input.MaxDepth
		}
```

Then pass `input.Depth` instead of `input.MaxDepth` to `analyze.BuildDepGraph`:

```go
		graph, err := analyze.BuildDepGraph(ctx, analyze.DepGraphInput{
			Root:          root,
			Type:          input.Type,
			Format:        input.Format,
			Focus:         input.Focus,
			MaxDepth:      input.Depth, // internal struct keeps MaxDepth name
			IncludeStdlib: input.IncludeStdlib,
			CrossLanguage: input.CrossLanguage,
		})
```

Note: `analyze.DepGraphInput.MaxDepth` keeps its name (internal, not exposed to LLMs).

**Step 3: Build and verify**

Run: `cd /path/to/repos/src/go-code && make build`

**Step 4: Commit**

```bash
git add cmd/go-code/tool_dep_graph.go
git commit -m "feat(dep_graph): add depth alias for max_depth"
```

---

### Task 3: `symbol_search` — add `symbol` alias for `query`

**Files:**
- Modify: `cmd/go-code/tool_symbol_search.go:15-33` (SymbolSearchInput struct)
- Modify: `cmd/go-code/tool_symbol_search.go:69-75` (handler validation)

**Step 1: Add `Symbol` field to `SymbolSearchInput`**

Add after `Query` field:

```go
type SymbolSearchInput struct {
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	Query string `json:"query,omitempty" jsonschema_description:"Symbol name or pattern to search (supports wildcards: Auth* or *Handler)"`

	// Symbol is an alias for Query — matches call_trace/impact_analysis naming.
	Symbol string `json:"symbol,omitempty" jsonschema_description:"Alias for query — symbol name or pattern (supports wildcards: Auth* or *Handler)"`

	Kind string `json:"kind,omitempty" jsonschema_description:"Filter by kind: function | method | type | struct | interface | const | var (default: all)"`

	Language string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python)"`

	IncludeBody bool `json:"include_body,omitempty" jsonschema_description:"Include the full source body in results (default: false, only signatures)"`

	Limit int `json:"limit,omitempty" jsonschema_description:"Maximum number of results to return. Default 100, max 500."`
}
```

Note: `Query` becomes `omitempty` since `Symbol` can be used instead.

**Step 2: Add symbol→query alias in handler**

Replace the `query` required check:

```go
		// Allow "symbol" as alias for "query" — matches call_trace/impact_analysis naming.
		if input.Symbol != "" && input.Query == "" {
			input.Query = input.Symbol
		}
		if input.Repo == "" {
			return errResult("repo is required"), nil
		}
		if input.Query == "" {
			return errResult("query (or symbol) is required"), nil
		}
```

**Step 3: Build and verify**

Run: `cd /path/to/repos/src/go-code && make build`

**Step 4: Commit**

```bash
git add cmd/go-code/tool_symbol_search.go
git commit -m "feat(symbol_search): add symbol alias for query"
```

---

### Task 4: `file_parse` — add `repo` + `ref` support

**Files:**
- Modify: `cmd/go-code/tool_file_parse.go` (full rewrite of registration)
- Modify: `cmd/go-code/register.go:59` (pass deps to registerFileParse)

**Step 1: Update `register.go` to pass `deps`**

Change line 59:

```go
	registerFileParse(server, cfg, deps)
```

**Step 2: Rewrite `tool_file_parse.go`**

Replace the registration function. Key changes:
- Add `Repo` + `Ref` fields to `FileParseInput`
- Switch from `mcp.AddTool` to `mcpserver.AddTool`
- Add `deps analyze.Deps` parameter
- Use `resolveRoot()` when `repo` is present
- Keep existing local-path behavior when `repo` is absent

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const outputFormatAST = "ast"

// FileParseInput is the input schema for the file_parse tool.
type FileParseInput struct {
	// Repo is optional: GitHub slug or URL. When set, path is relative to the repo root.
	Repo string `json:"repo,omitempty" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path. When set, path is relative to repo root."`

	// Ref is the branch, tag, or commit SHA (only used with repo).
	Ref string `json:"ref,omitempty" jsonschema_description:"Branch, tag, or commit SHA (default: HEAD). Only used with repo."`

	// Path is the file path. Absolute for local files, or relative to repo root when repo is set.
	Path string `json:"path" jsonschema_description:"File path: absolute for local files, or relative to repo root when repo is set"`

	// Language overrides auto-detection.
	Language string `json:"language,omitempty" jsonschema_description:"Language override (go/python/typescript/rust/java/c/cpp). Auto-detected if omitted."`

	// OutputFormat controls what is returned.
	OutputFormat string `json:"output_format,omitempty" jsonschema_description:"Output format: ast (raw tree) | symbols (functions types vars) (default: symbols)"`
}

func registerFileParse(server *mcp.Server, cfg Config, deps analyze.Deps) {
	maxBytes := cfg.MaxFileBytes

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "file_parse",
		Description: "Parse a single source file using tree-sitter and return its AST or symbol table. " +
			"Supports Go, Python, TypeScript, JavaScript, Rust, Java, C, C++. " +
			"Accepts a repo parameter (GitHub slug or URL) to parse files from remote repositories. " +
			"Use output_format=symbols to get a structured list of functions, types, and variables. " +
			"Use output_format=ast to get the raw syntax tree for deep analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input FileParseInput) (*mcp.CallToolResult, error) {
		if input.Path == "" {
			return errResult("path is required"), nil
		}

		var filePath string
		if input.Repo != "" {
			// Remote or local repo — resolve root, then join with path.
			root, cleanup, err := resolveRoot(ctx, input.Repo, input.Ref, deps)
			if err != nil {
				return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
			}
			defer cleanup()
			filePath = filepath.Join(root, input.Path)
		} else {
			// Local file path — apply path mappings.
			filePath = rewritePath(input.Path, cfg.PathMappings)
		}

		fi, err := os.Stat(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("stat file: %s", err)), nil
		}
		if fi.Size() > maxBytes {
			return errResult(fmt.Sprintf("file too large: %d bytes (max %d)", fi.Size(), maxBytes)), nil
		}

		source, err := os.ReadFile(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("read file: %s", err)), nil
		}

		includeBody := input.OutputFormat == outputFormatAST
		pr, err := parser.ParseFile(filePath, source, parser.ParseOpts{
			Language:       input.Language,
			IncludeBody:    includeBody,
			IncludeImports: true,
		})
		if err != nil {
			return errResult(fmt.Sprintf("parse file: %s", err)), nil
		}

		return textResult(formatParseResult(pr, input.OutputFormat)), nil
	})
}
```

Keep `formatParseResult`, `formatSymbolsTable`, `formatSymbolsAST` unchanged.

**Step 3: Build and verify**

Run: `cd /path/to/repos/src/go-code && make build`

**Step 4: Commit**

```bash
git add cmd/go-code/tool_file_parse.go cmd/go-code/register.go
git commit -m "feat(file_parse): add repo and ref params for remote file parsing"
```

---

### Task 5: Better validation errors in go-mcpserver

**Files:**
- Modify: `vendor/github.com/anatolykoptev/go-mcpserver/lenient.go:50-51`

**Step 1: Improve validation error in AddTool**

Replace the validation error handling block (lines 50-51):

```go
		if err := resolved.Validate(&m); err != nil {
			msg := err.Error()
			// Enhance "unexpected additional properties" with valid property list.
			if strings.Contains(msg, "unexpected additional properties") {
				var validProps []string
				if schema.Properties != nil {
					for k := range schema.Properties {
						validProps = append(validProps, k)
					}
				}
				if len(validProps) > 0 {
					sort.Strings(validProps)
					msg += fmt.Sprintf(". Valid parameters: %s", strings.Join(validProps, ", "))
				}
			}
			return toolError(fmt.Sprintf("validating arguments: %v", msg)), nil
		}
```

Add `"sort"` to imports.

**Step 2: Build and verify**

Run: `cd /path/to/repos/src/go-code && make build`

**Step 3: Commit**

```bash
git add vendor/github.com/anatolykoptev/go-mcpserver/lenient.go
git commit -m "fix(mcpserver): include valid params in validation errors"
```

---

### Task 6: Integration test — smoke test all aliases

**Files:**
- Create: `cmd/go-code/tool_aliases_test.go`

**Step 1: Write test file**

```go
package main

import (
	"testing"
)

func TestCodeSearchInput_PathAlias(t *testing.T) {
	input := CodeSearchInput{
		Repo:    "owner/repo",
		Pattern: "func main",
		Path:    "internal/query",
	}
	if input.Path != "" && input.FileGlob == "" {
		input.FileGlob = input.Path + "/**"
	}
	if input.FileGlob != "internal/query/**" {
		t.Errorf("expected file_glob=internal/query/**, got %s", input.FileGlob)
	}
}

func TestDepGraphInput_DepthAlias(t *testing.T) {
	input := DepGraphInput{MaxDepth: 7}
	if input.MaxDepth > 0 && input.Depth == 0 {
		input.Depth = input.MaxDepth
	}
	if input.Depth != 7 {
		t.Errorf("expected depth=7, got %d", input.Depth)
	}
}

func TestDepGraphInput_DepthTakesPrecedence(t *testing.T) {
	input := DepGraphInput{Depth: 3, MaxDepth: 7}
	if input.MaxDepth > 0 && input.Depth == 0 {
		input.Depth = input.MaxDepth
	}
	if input.Depth != 3 {
		t.Errorf("expected depth=3, got %d", input.Depth)
	}
}

func TestSymbolSearchInput_SymbolAlias(t *testing.T) {
	input := SymbolSearchInput{
		Repo:   "owner/repo",
		Symbol: "HandleRequest",
	}
	if input.Symbol != "" && input.Query == "" {
		input.Query = input.Symbol
	}
	if input.Query != "HandleRequest" {
		t.Errorf("expected query=HandleRequest, got %s", input.Query)
	}
}

func TestFileParseInput_RepoField(t *testing.T) {
	input := FileParseInput{
		Repo: "owner/repo",
		Path: "internal/query/ranking.go",
	}
	if input.Repo == "" {
		t.Error("expected repo to be set")
	}
	if input.Path == "" {
		t.Error("expected path to be set")
	}
}
```

**Step 2: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./cmd/go-code/ -run TestCodeSearch\|TestDepGraph\|TestSymbol\|TestFileParse -v`
Expected: all PASS.

**Step 3: Commit**

```bash
git add cmd/go-code/tool_aliases_test.go
git commit -m "test: add alias parameter unit tests"
```

---

### Task 7: Deploy and verify

**Step 1: Build Docker image**

Run: `cd ~/deploy/example-server && docker compose build --no-cache go-code`

**Step 2: Restart service**

Run: `docker compose up -d --no-deps --force-recreate go-code`

**Step 3: Verify tools respond**

Test with curl or MCP client that `file_parse` accepts `repo`, `code_search` accepts `path`, etc.

**Step 4: Final commit tag (optional)**

```bash
cd /path/to/repos/src/go-code && git tag v1.16.0
```
