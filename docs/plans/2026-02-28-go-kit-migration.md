# go-code → go-kit Migration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace go-code's internal/llm, internal/retry, internal/metrics, and env helpers with go-kit v0.1.0 packages — eliminating ~600 LOC of duplicated infrastructure.

**Architecture:** Split go-code's `internal/llm` into two parts: generic LLM client (→ go-kit/llm) and domain-specific prompts/intent (→ new `internal/prompts` package). Replace env helpers in config.go/register.go with go-kit/env. Delete internal/retry (subsumed by go-kit/llm's built-in retry) and internal/metrics (unused in production).

**Tech Stack:** Go 1.24+, `github.com/anatolykoptev/go-kit` v0.1.0

---

## Context

**go-code** is at `$REPO_ROOT/` — a code intelligence MCP server.

**go-kit** is at `/home/user/src/go-kit/` — shared infrastructure module (`github.com/anatolykoptev/go-kit` v0.1.0) with packages: env, llm, retry, metrics, strutil, cache.

**What we're replacing:**

| Internal package | go-kit replacement | Notes |
|---|---|---|
| `env()`, `envInt()`, `envList()` in `config.go` | `env.Str()`, `env.Int()`, `env.List()` | 1:1 mapping |
| `envIntOrDefault()` in `register.go` | `env.Int()` | 1:1 mapping |
| `internal/retry/` (116 LOC) | Not needed — go-kit/llm has built-in retry | Only consumer was internal/llm |
| `internal/metrics/` (65 LOC) | Delete — not used in production code | Only test file referenced it |
| `internal/llm/` (363 LOC, split) | go-kit/llm (Client) + new `internal/prompts` (constants/intent) | `Complete(ctx, system, user)` signature is identical |

**API compatibility notes:**
- `llm.NewClient(Config{})` → `llm.NewClient(baseURL, apiKey, model, ...Option)` — constructor changes
- `llm.Complete(ctx, system, user)` → identical signature, drop-in
- go-kit/llm `defaultMaxTokens=8192` vs go-code's `16384` — pass `WithMaxTokens(16384)`
- go-code's `CompleteRaw(ctx, prompt)` — not called externally, can be dropped
- All `SystemPrompt*` constants and `ClassifyIntent` move to `internal/prompts`

---

### Task 1: Add go-kit dependency and migrate env helpers

**Files:**
- Modify: `go.mod` — add go-kit dependency
- Modify: `cmd/go-code/config.go:104-150` — replace 3 private functions with go-kit/env
- Modify: `cmd/go-code/register.go:76-86` — replace `envIntOrDefault` with `env.Int`

**Step 1: Add go-kit dependency**

```bash
cd $REPO_ROOT
go get github.com/anatolykoptev/go-kit@v0.1.0
```

**Step 2: Modify config.go — replace env helpers with go-kit/env**

In `cmd/go-code/config.go`, replace the imports:

```go
// BEFORE (lines 3-8):
import (
	"os"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
)

// AFTER:
import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-kit/env"
)
```

Replace all calls in `loadConfig()` (lines 82-101):

```go
func loadConfig() Config {
	return Config{
		Port:              env.Str("MCP_PORT", defaultPort),
		LLMURL:            env.Str("LLM_API_BASE", defaultLLMURL),
		LLMAPIKey:         env.Str("LLM_API_KEY", ""),
		LLMModel:          env.Str("LLM_MODEL", defaultLLMModel),
		GithubToken:       env.Str("GITHUB_TOKEN", ""),
		WorkspaceDir:      env.Str("WORKSPACE_DIR", defaultWorkspaceDir),
		SearxngURL:        env.Str("SEARXNG_URL", "http://searxng:8888"),
		RedisURL:          env.Str("REDIS_URL", ""),
		LLMFallbackKeys:   env.List("LLM_API_KEY_FALLBACK", ""),
		GithubSearchRepos: env.List("GITHUB_SEARCH_REPOS", ""),
		PathMappings:      parsePathMappings(env.Str("PATH_MAPPINGS", "")),
		MaxFileBytes:      int64(env.Int("MAX_FILE_KB", defaultMaxFileBytesKB)) * bytesPerKB,
		MaxRepoBytes:      int64(env.Int("MAX_REPO_MB", defaultMaxRepoBytesMB)) * bytesPerMB,
		DatabaseURL:       env.Str("DATABASE_URL", ""),
		GraphTTLLocal:     env.Int("GRAPH_TTL_LOCAL", defaultGraphTTLLocal),
		GraphTTLRemote:    env.Int("GRAPH_TTL_REMOTE", defaultGraphTTLRemote),
		GraphBatchSize:    env.Int("GRAPH_BATCH_SIZE", defaultGraphBatchSize),
	}
}
```

Delete the 3 private helper functions at lines 104-150 (`env`, `envList`, `envInt`).

**Step 3: Modify register.go — replace envIntOrDefault**

In `cmd/go-code/register.go`, add import:

```go
import (
	// ... existing imports ...
	"github.com/anatolykoptev/go-kit/env"
)
```

Replace 4 calls (lines 23-31):

```go
// BEFORE:
parseCacheSize := envIntOrDefault("PARSE_CACHE_SIZE", cache.DefaultParseCacheSize)
llmCacheSize := envIntOrDefault("LLM_CACHE_SIZE", cache.DefaultLLMCacheSize)
llmCacheTTLMin := envIntOrDefault("LLM_CACHE_TTL_MIN", 60)
// ... and line 31:
MaxSize: envIntOrDefault("TOOL_CACHE_SIZE", 200),

// AFTER:
parseCacheSize := env.Int("PARSE_CACHE_SIZE", cache.DefaultParseCacheSize)
llmCacheSize := env.Int("LLM_CACHE_SIZE", cache.DefaultLLMCacheSize)
llmCacheTTLMin := env.Int("LLM_CACHE_TTL_MIN", 60)
// ... and:
MaxSize: env.Int("TOOL_CACHE_SIZE", 200),
```

Delete the `envIntOrDefault` function at lines 76-86.

**Step 4: Run tests**

```bash
go build ./... && go test ./... -count=1
```

Expected: all tests pass. The `config.go` and `register.go` changes are pure refactoring — identical behavior.

**Step 5: Lint**

```bash
golangci-lint run ./...
```

Expected: 0 new issues.

**Step 6: Commit**

```bash
git add go.mod go.sum cmd/go-code/config.go cmd/go-code/register.go
git commit -m "refactor: migrate env helpers to go-kit/env

Replace internal env(), envInt(), envList(), envIntOrDefault() with
go-kit/env.Str(), env.Int(), env.List(). Delete ~50 LOC of helpers."
```

---

### Task 2: Create internal/prompts package

Extract domain-specific LLM prompts, intent classification, and prompt routing from `internal/llm` into a new `internal/prompts` package. This is the preparatory step before switching to go-kit/llm.

**Files:**
- Create: `internal/prompts/prompts.go` — all SystemPrompt* constants
- Create: `internal/prompts/intent.go` — Intent type, ClassifyIntent, SystemPromptForIntent, SystemPromptForDepth
- Create: `internal/prompts/intent_test.go` — copy tests from `internal/llm/intent_test.go` (if exists)

**Step 1: Create internal/prompts/prompts.go**

```go
// Package prompts provides domain-specific LLM system prompts for go-code tools.
package prompts

// SystemPromptQuickSearch is for GitHub code search result summarization.
const SystemPromptQuickSearch = `You are analyzing GitHub code search results. Summarize the relevant code patterns found for the query. Be concise, reference file paths.`

// SystemPromptIssuesAnalysis is for GitHub issues/PRs analysis.
const SystemPromptIssuesAnalysis = `You are analyzing GitHub issues/PRs. Summarize the key findings for the query. Focus on what's most relevant. Be concise.`

// SystemPromptRepoAnalysis is for repository analysis queries.
const SystemPromptRepoAnalysis = `You are a senior software engineer analyzing a code repository.
You have been provided with the repository's file tree, key source files, and parsed symbol information.
Answer the user's question about the codebase accurately and concisely.
Focus on architecture, design decisions, and implementation patterns.
Use code examples from the provided context when relevant.
If you cannot answer from the provided context, say so clearly.`

// SystemPromptCodeCompare is for code comparison queries.
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
      "snippetA": "relevant code from repo A",
      "snippetB": "relevant code from repo B"
    }
  ],
  "gaps": [
    {
      "missingIn": "repo_a" or "repo_b",
      "feature": "what is missing",
      "locationB": "file path where it exists",
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

// SystemPromptDepGraph is for dependency graph analysis.
const SystemPromptDepGraph = `You are a senior software engineer analyzing a dependency graph.
Based on the provided import/dependency data, describe:
1. The overall layering and module structure
2. Any circular dependencies or problematic coupling
3. Hotspot packages (many dependents)
4. Suggestions for improving the dependency structure
Be concise and actionable.`

// SystemPromptOverview is for high-level repository analysis.
const SystemPromptOverview = `You are a senior software engineer providing a high-level overview of a code repository.
Focus on: public API surface, key architectural components, package organization, and design patterns.
Be concise — summarize the architecture, don't enumerate every function.
Use the provided symbol signatures and file tree to identify the main modules and their responsibilities.`

// SystemPromptDeep is for deep repository analysis.
const SystemPromptDeep = `You are a senior software engineer doing deep analysis of a code repository.
Focus on: implementation details, algorithms, error handling, edge cases, and performance characteristics.
Reference specific functions, line numbers, and code patterns.
Explain how components interact at the implementation level, not just the interface level.`

// SystemPromptCallTrace is for call chain narrative generation.
const SystemPromptCallTrace = `You are a senior software engineer explaining an execution path through a codebase.
You receive a call chain trace (JSON tree of function calls).

Explain step-by-step what happens when the entry function is called:
1. What each function does (based on its name and signature)
2. Key decision points and error handling paths
3. External calls that leave the codebase (stdlib, third-party)
4. Cycles or recursive patterns if present

Be concise and focus on the flow, not line-by-line details.
Format as a numbered walkthrough.`

// SystemPromptClassifyGraphQuery classifies a natural-language query into a graph template.
// Contains %s placeholder for template list — use with fmt.Sprintf.
const SystemPromptClassifyGraphQuery = `You are a query classifier for a code knowledge graph.

Given a natural-language question about code, select the best matching template and extract parameters.

Available templates:
%s

Respond with ONLY a JSON object, no explanation:
{"template": "<template_id>", "params": {"param_name": "value"}}

If no template fits, respond:
{"template": "freeform", "params": {}}

Rules:
- Extract symbol/function/package names from the question into params
- Use "freeform" only if the question truly doesn't match any template
- Parameter values should be exact names from the question (case-sensitive)`

// SystemPromptGraphNarrative formats raw graph query results into a narrative.
const SystemPromptGraphNarrative = `You are a senior software engineer explaining code graph query results.
You receive: the original question, the Cypher query used, and the raw results.
Provide a concise narrative answer. Reference file paths and function names.
If results are empty, say so clearly. Do not speculate beyond what the data shows.`

// SystemPromptGenerateCypher generates a read-only Cypher query from natural language.
const SystemPromptGenerateCypher = `You are a Cypher query generator for a code knowledge graph stored in Apache AGE.

Graph schema:
- Vertex labels: Package (name, path, repo), File (path, language, lines), Symbol (name, kind, signature, file, start_line, end_line)
- Edge labels: CONTAINS (Package→File, File→Symbol), CALLS (Symbol→Symbol, line property), IMPORTS (File→Package, alias property)
- kind values: function, method, type, struct, interface, class, const, var, module

Generate a READ-ONLY Cypher query. Do NOT use CREATE, DELETE, SET, MERGE, REMOVE, or DROP.

Respond with ONLY the Cypher query, no explanation.`
```

**Step 2: Create internal/prompts/intent.go**

Copy `internal/llm/intent.go` verbatim, changing only the package declaration and adding the architecture/debug/navigate private prompts:

```go
package prompts

import "strings"

// Intent represents the classified intent of a user query.
type Intent string

const (
	IntentArchitecture Intent = "architecture"
	IntentDebug        Intent = "debug"
	IntentNavigate     Intent = "navigate"
	IntentDependency   Intent = "dependency"
	IntentGeneral      Intent = "general"
)

var intentKeywords = map[Intent][]string{
	IntentArchitecture: {
		"architecture", "design", "pattern", "structure", "module",
		"organized", "layered", "component", "overview", "approach",
		"designed", "architected",
	},
	IntentDebug: {
		"bug", "error", "fail", "crash", "panic", "nil pointer",
		"race condition", "fix", "broken", "wrong", "issue", "cause",
		"debug", "500", "404", "timeout",
	},
	IntentNavigate: {
		"where", "find", "locate", "defined", "definition", "show me",
		"which file", "what file", "contains", "look for", "search for",
	},
	IntentDependency: {
		"import", "depend", "dependency", "graph",
		"packages depend", "module depend", "coupling",
	},
}

// ClassifyIntent determines the intent of a user query by scoring keyword hits.
func ClassifyIntent(query string) Intent {
	if strings.TrimSpace(query) == "" {
		return IntentGeneral
	}

	lower := strings.ToLower(query)
	scores := make(map[Intent]int, len(intentKeywords))

	for intent, keywords := range intentKeywords {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				scores[intent]++
			}
		}
	}

	best := IntentGeneral
	bestScore := 0
	for _, intent := range []Intent{IntentArchitecture, IntentDebug, IntentNavigate, IntentDependency} {
		if s := scores[intent]; s > bestScore {
			bestScore = s
			best = intent
		}
	}

	return best
}

// SystemPromptForIntent returns a system prompt tailored to the classified intent.
func SystemPromptForIntent(intent Intent, depth string) string {
	switch depth {
	case "overview":
		return SystemPromptOverview
	case "deep":
		return SystemPromptDeep
	}

	switch intent {
	case IntentArchitecture:
		return systemPromptArchitecture
	case IntentDebug:
		return systemPromptDebug
	case IntentNavigate:
		return systemPromptNavigate
	case IntentDependency:
		return SystemPromptDepGraph
	default:
		return SystemPromptRepoAnalysis
	}
}

// SystemPromptForDepth returns the system prompt for a given analysis depth.
func SystemPromptForDepth(depth string) string {
	switch depth {
	case "overview":
		return SystemPromptOverview
	case "deep":
		return SystemPromptDeep
	default:
		return SystemPromptRepoAnalysis
	}
}

const systemPromptArchitecture = `You are a senior software architect analyzing a code repository.
Focus on: design patterns, architectural decisions, module boundaries, separation of concerns.
Explain the high-level structure first, then drill into key components.
Reference specific packages and their responsibilities.
Highlight strengths and potential improvements in the architecture.`

const systemPromptDebug = `You are a senior software engineer debugging a code issue.
Focus on: error handling paths, edge cases, race conditions, null/nil checks.
Trace the execution flow from the suspected entry point.
Identify potential root causes and suggest fixes with specific code references.
Prioritize the most likely cause first.`

const systemPromptNavigate = `You are a code navigator helping locate specific code elements.
Focus on: exact file paths, line numbers, function signatures.
Provide the precise location of the requested symbol or pattern.
Show how it connects to related code (callers, callees, types used).
Be direct — answer with locations first, context second.`
```

**Step 3: Run tests to verify the new package compiles**

```bash
go build ./internal/prompts/...
```

Expected: clean build, no errors.

**Step 4: Commit**

```bash
git add internal/prompts/
git commit -m "refactor: extract internal/prompts from internal/llm

Move all SystemPrompt* constants, Intent type, ClassifyIntent, and
SystemPromptForIntent to a new internal/prompts package. Prepares for
migration of LLM client to go-kit/llm."
```

---

### Task 3: Migrate LLM client to go-kit/llm

Replace `internal/llm.Client` with `go-kit/llm.Client` across all consumers. Update constructor, switch prompt imports from `internal/llm` to `internal/prompts`.

**Files to modify (10 files):**
- `cmd/go-code/register.go` — constructor call
- `cmd/go-code/tool_repo_analyze.go` — prompt references
- `cmd/go-code/tool_call_trace.go` — prompt references
- `internal/analyze/analyze.go` — Deps type, imports, prompt references
- `internal/analyze/analyze_test.go` — test helper constructor
- `internal/compare/compare.go` — parameter type, prompt references
- `internal/codegraph/query.go` — parameter type, prompt references
- `internal/codegraph/generate.go` — prompt references
- `internal/codegraph/classify.go` — prompt references

**Step 1: Update register.go — constructor**

Change the `llm` import and constructor call:

```go
// BEFORE:
import (
	// ...
	"github.com/anatolykoptev/go-code/internal/llm"
	// ...
)

deps := analyze.Deps{
	LLM: llm.NewClient(llm.Config{
		BaseURL:      cfg.LLMURL,
		APIKey:       cfg.LLMAPIKey,
		Model:        cfg.LLMModel,
		FallbackKeys: cfg.LLMFallbackKeys,
	}),

// AFTER:
import (
	// ...
	"github.com/anatolykoptev/go-kit/llm"
	// ...
)
// Remove import of "github.com/anatolykoptev/go-code/internal/llm"

deps := analyze.Deps{
	LLM: llm.NewClient(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel,
		llm.WithFallbackKeys(cfg.LLMFallbackKeys),
		llm.WithMaxTokens(16384),
	),
```

Note: `16384` was the default in go-code's internal llm. go-kit defaults to `8192`, so we must set it explicitly. Add a named constant:

```go
const defaultLLMMaxTokens = 16384
```

And use it: `llm.WithMaxTokens(defaultLLMMaxTokens)`.

**Step 2: Update analyze/analyze.go — Deps type and imports**

```go
// BEFORE:
import (
	// ...
	"github.com/anatolykoptev/go-code/internal/llm"
	// ...
)

type Deps struct {
	LLM *llm.Client
	// ...
}

// Uses: llm.ClassifyIntent(query), llm.SystemPromptForIntent(intent, depth)

// AFTER:
import (
	// ...
	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/go-code/internal/prompts"
	// ...
)

type Deps struct {
	LLM *llm.Client  // now go-kit/llm.Client
	// ...
}

// Change: llm.ClassifyIntent → prompts.ClassifyIntent
// Change: llm.SystemPromptForIntent → prompts.SystemPromptForIntent
```

Search `analyze.go` for all `llm.ClassifyIntent` and `llm.SystemPromptForIntent` references and replace with `prompts.ClassifyIntent` and `prompts.SystemPromptForIntent`. The `deps.LLM.Complete(ctx, system, user)` calls remain unchanged (same method signature).

**Step 3: Update analyze/analyze_test.go**

```go
// BEFORE:
llm.NewClient(llm.Config{BaseURL: "...", APIKey: "...", Model: "..."})

// AFTER:
llm.NewClient("...", "...", "...", llm.WithMaxTokens(16384))
```

**Step 4: Update compare/compare.go**

```go
// BEFORE:
import "github.com/anatolykoptev/go-code/internal/llm"
// Uses: *llm.Client param, llm.SystemPromptCodeCompare

// AFTER:
import (
	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/go-code/internal/prompts"
)
// Change: llm.SystemPromptCodeCompare → prompts.SystemPromptCodeCompare
// *llm.Client param type stays the same (now go-kit's Client)
```

**Step 5: Update codegraph/query.go**

```go
// BEFORE:
import "github.com/anatolykoptev/go-code/internal/llm"
// Uses: *llm.Client, llm.SystemPromptGraphNarrative

// AFTER:
import (
	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/go-code/internal/prompts"
)
// Change: llm.SystemPromptGraphNarrative → prompts.SystemPromptGraphNarrative
```

**Step 6: Update codegraph/generate.go**

```go
// BEFORE:
import "github.com/anatolykoptev/go-code/internal/llm"
// Uses: llm.SystemPromptGenerateCypher

// AFTER:
import "github.com/anatolykoptev/go-code/internal/prompts"
// Change: llm.SystemPromptGenerateCypher → prompts.SystemPromptGenerateCypher
// Note: llmCompleter is a LOCAL interface, no llm import needed for it
```

**Step 7: Update codegraph/classify.go**

```go
// BEFORE:
import "github.com/anatolykoptev/go-code/internal/llm"
// Uses: llm.SystemPromptClassifyGraphQuery

// AFTER:
import "github.com/anatolykoptev/go-code/internal/prompts"
// Change: llm.SystemPromptClassifyGraphQuery → prompts.SystemPromptClassifyGraphQuery
```

**Step 8: Update tool_repo_analyze.go**

```go
// BEFORE:
import "github.com/anatolykoptev/go-code/internal/llm"
// Uses: llm.SystemPromptQuickSearch, llm.SystemPromptIssuesAnalysis

// AFTER:
import "github.com/anatolykoptev/go-code/internal/prompts"
// Change: llm.SystemPromptQuickSearch → prompts.SystemPromptQuickSearch
// Change: llm.SystemPromptIssuesAnalysis → prompts.SystemPromptIssuesAnalysis
```

**Step 9: Update tool_call_trace.go**

```go
// BEFORE:
import "github.com/anatolykoptev/go-code/internal/llm"
// Uses: llm.SystemPromptCallTrace

// AFTER:
import "github.com/anatolykoptev/go-code/internal/prompts"
// Change: llm.SystemPromptCallTrace → prompts.SystemPromptCallTrace
```

**Step 10: Build and test**

```bash
go build ./...
go test ./... -count=1
golangci-lint run ./...
```

Expected: all pass. The `Complete(ctx, system, user)` signature is identical between go-code's internal llm and go-kit's llm.

**Step 11: Commit**

```bash
git add cmd/go-code/register.go cmd/go-code/tool_repo_analyze.go cmd/go-code/tool_call_trace.go \
  internal/analyze/analyze.go internal/analyze/analyze_test.go \
  internal/compare/compare.go internal/codegraph/query.go \
  internal/codegraph/generate.go internal/codegraph/classify.go \
  go.mod go.sum
git commit -m "refactor: migrate LLM client to go-kit/llm

Replace internal/llm.Client with go-kit/llm.Client across all consumers.
Prompt constants now come from internal/prompts.
Constructor changed from Config struct to positional args + functional options.
Complete(ctx, system, user) signature is identical — all call sites unchanged."
```

---

### Task 4: Delete old internal packages

Remove `internal/llm/`, `internal/retry/`, and `internal/metrics/` — all now replaced or unused.

**Files:**
- Delete: `internal/llm/llm.go`
- Delete: `internal/llm/intent.go`
- Delete: `internal/llm/intent_test.go` (if exists)
- Delete: `internal/retry/retry.go`
- Delete: `internal/retry/retry_test.go` (if exists)
- Delete: `internal/metrics/metrics.go`
- Delete: `internal/metrics/metrics_test.go`

**Step 1: Verify no remaining imports of old packages**

```bash
grep -rn '"github.com/anatolykoptev/go-code/internal/llm"' --include='*.go' .
grep -rn '"github.com/anatolykoptev/go-code/internal/retry"' --include='*.go' .
grep -rn '"github.com/anatolykoptev/go-code/internal/metrics"' --include='*.go' .
```

Expected: zero matches for all three.

If any matches remain, fix them before proceeding (change to go-kit or internal/prompts imports).

**Step 2: Delete the directories**

```bash
rm -rf internal/llm internal/retry internal/metrics
```

**Step 3: Tidy**

```bash
go mod tidy
```

**Step 4: Build and test**

```bash
go build ./...
go test ./... -count=1
golangci-lint run ./...
```

Expected: all pass.

**Step 5: Commit**

```bash
git add -A
git commit -m "refactor: delete internal/llm, internal/retry, internal/metrics

These packages are fully replaced:
- internal/llm → go-kit/llm (Client) + internal/prompts (constants/intent)
- internal/retry → subsumed by go-kit/llm's built-in retry
- internal/metrics → not used in production code

Removes ~545 LOC of duplicated infrastructure."
```

---

### Task 5: Final verification and deploy test

**Step 1: Full test suite**

```bash
cd $REPO_ROOT
go test ./... -v -count=1
```

Expected: all tests pass.

**Step 2: Full lint**

```bash
golangci-lint run ./...
```

Expected: 0 issues.

**Step 3: Build binary**

```bash
CGO_ENABLED=1 go build -o bin/go-code ./cmd/go-code/
```

Expected: clean build.

**Step 4: Verify go.mod**

```bash
cat go.mod | grep go-kit
```

Expected: `github.com/anatolykoptev/go-kit v0.1.0`

**Step 5: Verify no old packages remain**

```bash
ls internal/llm internal/retry internal/metrics 2>&1
```

Expected: "No such file or directory" for all three.

**Step 6: Check git diff stats**

```bash
git diff --stat HEAD~4
```

Expected: net deletion of ~500+ LOC.
