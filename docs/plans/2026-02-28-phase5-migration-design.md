# Phase 5: go-search → go-code Migration

**Date**: 2026-02-28
**Goal**: Move all code-related tools from go-search to go-code. Add production infrastructure (retry, Redis cache, metrics). Clean separation: go-search = web search, go-code = code intelligence.

## Scope

### What moves to go-code
- `github_repo_analyze` quick mode (GitHub Code Search API)
- `github_repo_analyze` issues/PRs mode (GitHub Issues Search API)
- `github_repo_search` → renamed to `repo_search`
- `internal/gitingest/` — not needed (go-code uses tree-sitter, better than gitingest)
- `sources/github.go` functions: SearchGitHubCode, SearchGitHubIssues, SearchGitHubRepos, FetchRepoMeta, FetchREADME — already partially in go-code's `internal/github`

### What stays in go-search
- `smart_search`, `searxng_web_search`, `web_url_read`, `wp_dev_search`
- `hn_search`, `youtube_search`, `hf_model_search`, `hf_dataset_search`
- `job_search`, `freelance_search`, `remote_work_search`, `flight_search`
- `internal/engine/` (search pipeline, LLM, cache, retry, metrics)
- `internal/engine/sources/` (all non-GitHub sources)

### What gets deleted from go-search
- `tool_github_repo_analyze.go`
- `tool_github_repo_search.go`
- `internal/gitingest/` (entire package)
- `sources/github.go` code-specific functions (SearchGitHubCode, SearchGitHubIssues, SearchGitHubRepos, FetchRepoMeta, FetchREADME, ExtractOwnerRepo)

## Architecture

### New packages in go-code

```
internal/
  github/          — EXTEND existing package
    github.go      ← add: SearchGitHubCode, SearchGitHubIssues,
                     SearchGitHubRepos, ExtractOwnerRepo
  search/          — NEW: SearXNG + repo search pipeline
    searxng.go     — SearchSearXNG, FilterByScore, DedupByDomain
    repo_search.go — RepoSearch pipeline (parallel SearXNG + GitHub API + enrich)
  retry/           — NEW: exponential backoff
    retry.go       — RetryHTTP, RetryDo
  metrics/         — NEW: atomic counters
    metrics.go     — LLMCalls, LLMErrors, SearchRequests, CacheHits, etc.
  cache/           — EXTEND: add Redis L2
    cache.go       ← add tieredCache, Redis L2, CacheKey with SHA256
  llm/             — EXTEND: add retry + fallback keys
    llm.go         ← add retry wrapping, fallback API keys, CallLLMRaw
```

### New/modified MCP tools

```
cmd/go-code/
  tool_repo_search.go    — NEW: repo_search (from github_repo_search)
  tool_repo_analyze.go   ← EXTEND: mode=quick, type=pr|issue, repos[], pattern
```

### Dependency direction

```
cmd/go-code → internal/analyze → internal/ingest
                               → internal/parser
                               → internal/llm    → internal/retry
                               → internal/cache
                               → internal/github  → internal/retry
                               → internal/search  → internal/retry
                               → internal/metrics
            → internal/compare
            → internal/callgraph
```

No circular dependencies. retry, metrics, cache are leaf packages.

## Detailed Design

### 1. repo_analyze extensions

Extend `RepoAnalyzeInput`:

```go
type RepoAnalyzeInput struct {
    Repo    string   `json:"repo" ...`
    Query   string   `json:"query" ...`
    Ref     string   `json:"ref,omitempty" ...`
    Focus   string   `json:"focus,omitempty" ...`
    Mode    string   `json:"mode,omitempty" ...`  // add: "quick", "raw"
    Depth   string   `json:"depth,omitempty" ...`
    // NEW fields:
    Type    string   `json:"type,omitempty" ...`    // "pr" or "issue"
    Repos   []string `json:"repos,omitempty" ...`   // multi-repo for quick mode
    Pattern string   `json:"pattern,omitempty" ...`  // file include pattern
}
```

Handler routing:
1. `type=pr|issue` → handleIssuesMode (GitHub Issues API + LLM summary)
2. `mode=quick|raw` → handleQuickMode (GitHub Code Search API ± LLM)
3. local path → existing local mode (tree-sitter)
4. default → existing deep mode (clone + tree-sitter + LLM)

### 2. repo_search (new tool)

```go
type RepoSearchInput struct {
    Query    string `json:"query" ...`
    Language string `json:"language,omitempty" ...`
    Sort     string `json:"sort,omitempty" ...`  // stars, forks, updated
}
```

Pipeline:
1. Parallel: SearXNG search + GitHub Repos API + SearXNG site:github.com
2. LLM query expansion → additional GitHub API searches
3. Dedup by URL + owner/repo
4. Parallel enrich: FetchRepoMeta + FetchREADME (max 12 repos)
5. LLM summarization with GHRepoSearchInstruction
6. FormatOutput with sources

### 3. internal/github extensions

Port from go-search `sources/github.go`:
- `SearchGitHubCode(ctx, query string, repos []string) ([]CodeResult, error)`
- `SearchGitHubIssues(ctx, query string) ([]IssueItem, error)`
- `SearchGitHubRepos(ctx, query, sort string) ([]RepoResult, error)`
- `ExtractOwnerRepo(url string) (owner, repo string, ok bool)`

Already in go-code: FetchRepoMeta, FetchREADME, CloneRepo.

### 4. internal/search (new)

Port from go-search `engine/search.go`:
- `SearchSearXNG(ctx, query, lang, timeRange, engines string) ([]Result, error)`
- `FilterByScore(results []Result, minScore float64, minKeep int) []Result`
- `DedupByDomain(results []Result, maxPerDomain int) []Result`

Types: `Result` (Title, URL, Content, Score).

### 5. internal/retry (new)

Port from go-search `engine/retry.go`:
- `RetryHTTP(ctx, fn func() (*http.Response, error), opts Options) (*http.Response, error)`
- `RetryDo[T any](ctx, fn func() (T, error), opts Options) (T, error)`
- `Options{MaxAttempts, InitialDelay, MaxDelay, RetryOn func(error) bool}`
- Default: 3 attempts, 500ms initial, 5s max, retry on 429/5xx/network errors

### 6. internal/metrics (new)

Port from go-search `engine/metrics.go`:
- Atomic counters: LLMCalls, LLMErrors, SearchRequests, GitClones, CacheHits, CacheMisses
- `TrackOperation(name string, fn func() error) error` — measures duration + error
- `GetMetrics() map[string]int64`

### 7. internal/cache extensions

Add Redis L2 to existing LRU cache:
- `InitCache(ctx, redisURL string, ttl time.Duration)` — initializes Redis client
- `CacheGet`: check L1 (LRU) → miss → check L2 (Redis) → promote to L1
- `CacheSet`: write L1 + L2 (with TTL)
- `CacheKey(parts ...string) string` — SHA256 hash key
- Backward compatible: no REDIS_URL = L1-only (existing behavior)
- Use go-redis/v9

### 8. internal/llm extensions

- Retry: wrap Complete() with RetryDo (2 attempts, 1s backoff)
- Fallback keys: `FallbackAPIKeys []string` in Config
- `CallLLMRaw(ctx, prompt string) (string, error)` — single-prompt convenience

## New Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SEARXNG_URL` | `http://searxng:8888` | SearXNG instance for repo_search |
| `REDIS_URL` | (optional) | Redis for L2 cache, e.g. `redis://redis:6379/6` |
| `LLM_API_KEY_FALLBACK` | (optional) | Fallback LLM API key |
| `GITHUB_SEARCH_REPOS` | (optional) | Default repos for quick mode code search |

## Docker Compose Changes

go-code container needs:
- Network access to `searxng` container (already on same network)
- Network access to `redis` container (already on same network)
- New env vars in `.env`

## Testing Strategy

- Unit tests for each new package (retry, metrics, search, github extensions)
- Integration test: repo_search end-to-end (needs SearXNG running)
- Verify existing tests still pass (no regressions in repo_analyze deep/local)
- Manual verification: call each new mode via MCP

## Rollout Plan

1. Implement all new code in go-code
2. Deploy go-code with new tools
3. Verify new tools work via MCP
4. Delete code from go-search
5. Deploy go-search (fewer tools)
6. Update CLAUDE.md, MEMORY.md, agent configs

## Tool Count After Migration

go-code: 6 → 8 tools (add repo_search, extend repo_analyze)
go-search: 12 → 10 tools (remove github_repo_analyze, github_repo_search)
