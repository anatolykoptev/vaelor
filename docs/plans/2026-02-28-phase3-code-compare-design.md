# Phase 3: Comparison Engine — Design

## Mission

Find the **better solution**. `code_compare` is not a diff tool — it's a quality assessment engine that identifies superior implementations, missing features, and architectural patterns worth adopting. Works cross-language (Go vs Rust, Python vs TypeScript).

## Architecture

### Package Layout

```
internal/compare/
  compare.go      — CompareRepos() orchestrator
  snapshot.go      — BuildSnapshot(): ingest+parse → RepoSnapshot
  match.go         — MatchSymbols(): exact + fuzzy + semantic matching
  metrics.go       — ComputeMetrics(): quality-oriented metrics
  context.go       — BuildCompareContext(): diff + quality signals for LLM
```

### Data Flow

```
CodeCompareInput {RepoA, RepoB, Query, Focus, Language}
    ↓
CompareRepos(ctx, input, llmClient, caches)
    ↓
┌─ BuildSnapshot(RepoA) ─┐   (parallel goroutines)
└─ BuildSnapshot(RepoB) ─┘
    ↓
MatchSymbols(snapshotA, snapshotB) → []SymbolMatch
    ↓
ComputeMetrics(snapshotA), ComputeMetrics(snapshotB)
    ↓
BuildCompareContext(matches, metricsA, metricsB, query) → LLM prompt
    ↓
llm.Complete(systemPrompt, compareContext) → JSON analysis
    ↓
CompareResult {Analysis, MetricsA, MetricsB, MatchedCount, ...}
```

## Symbol Matching

Three levels, applied in order. Each level works only on remaining unmatched symbols.

| Level | When | How |
|-------|------|-----|
| Exact | Same language, same names | `name + kind` → pair |
| Fuzzy | Same language, renames | Levenshtein on signatures, threshold 0.7 |
| Semantic | Cross-language or unlike names | LLM groups symbols by purpose ("routing", "auth", "storage") |

### SymbolMatch Type

```go
type SymbolMatch struct {
    SymbolA   *parser.Symbol   // nil = gap (missing in A)
    SymbolB   *parser.Symbol   // nil = gap (missing in B)
    MatchType MatchType        // exact, fuzzy, semantic
    Category  string           // "routing", "error-handling", etc.
    Score     float64          // confidence 0-1
}
```

Coverage gaps: `SymbolMatch` where `SymbolA == nil` means repo B has something A doesn't.

## Quality Metrics (Computed by Code)

| Metric | What it Shows |
|--------|---------------|
| Avg/Max function lines | Decomposition, readability |
| Cyclomatic complexity (approx) | Logic simplicity |
| Test file ratio | Test coverage |
| Doc comment ratio | API documentation |
| External deps count | Dependency footprint |
| Error handling ratio | % functions with error return (Go) |
| Interface count | Abstractions, extensibility |
| Pkg/module depth | Package nesting depth |

## LLM Analysis

### System Prompt Layers

1. **Implementation Quality** — which implementation is cleaner, more optimized, more modern. Concrete examples with snippets.
2. **Coverage Gaps** — what exists in one repo but is absent in the other. What's worth adding.
3. **Architecture & Patterns** — package structure, separation of concerns, extensibility, testability. What to adopt.
4. **Verdict** — actionable recommendations: specific actions to improve the target repo.

### Output Format — JSON

```json
{
  "repo_a": "owner/repo-a",
  "repo_b": "owner/repo-b",
  "query": "compare auth implementation",
  "metrics": {
    "repo_a": {
      "files": 45, "total_lines": 3200,
      "avg_func_lines": 42, "max_func_lines": 180,
      "test_ratio": 0.12, "doc_ratio": 0.30,
      "error_handling_ratio": 0.65,
      "interfaces": 3, "external_deps": 28
    },
    "repo_b": {}
  },
  "analysis": {
    "quality": [
      {
        "aspect": "error handling",
        "winner": "repo_b",
        "reason": "Wraps all errors with context via fmt.Errorf",
        "snippet_a": "return err",
        "snippet_b": "return fmt.Errorf(\"connect db: %w\", err)"
      }
    ],
    "gaps": [
      {
        "missing_in": "repo_a",
        "feature": "graceful shutdown",
        "location_b": "pkg/server/shutdown.go",
        "importance": "high"
      }
    ],
    "architecture": [
      {
        "insight": "Interface-based dependency injection",
        "source": "repo_b",
        "example": "Storage interface in pkg/storage/storage.go",
        "benefit": "testability, swappable backends"
      }
    ],
    "recommendations": [
      "Add error wrapping with context across all packages",
      "Extract Storage interface for DB layer decoupling",
      "Add graceful shutdown handler"
    ]
  },
  "matched_symbols": 34,
  "unmatched_a": 5,
  "unmatched_b": 12
}
```

Fallback: if LLM returns invalid JSON, wrap raw text in `{"raw": "..."}`.

## Module-Level Comparison (3.3)

Same pipeline, but `Focus` parameter restricts ingest + matching to a specific path prefix (e.g., `internal/auth`). No separate implementation — just a filter on BuildSnapshot.

## Tool Input (MCP)

```go
type CodeCompareInput struct {
    RepoA    string  // GitHub slug or local path (required)
    RepoB    string  // GitHub slug or local path (required)
    Query    string  // What to compare / what to look for (required)
    Focus    string  // Package/directory filter (optional, for module-level)
    Language string  // Language filter (optional)
}
```

## Dependencies

- Reuses: `internal/ingest`, `internal/parser`, `internal/cache`, `internal/llm`
- New: `internal/compare/` (5 files)
- Modified: `cmd/go-code/tool_code_compare.go` (replace stub)
