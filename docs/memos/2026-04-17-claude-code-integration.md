# Claude Code × go-code Integration — Release Memo

**Date:** 2026-04-17
**Plan:** [`docs/plans/2026-04-17-claude-code-integration.md`](../plans/2026-04-17-claude-code-integration.md)
**Scope:** Turn go-code from a raw MCP server into a proactive Claude Code collaborator.

## What shipped

### Server (`/home/krolik/src/go-code`)

| Commit | Task | Summary |
|---|---|---|
| `6acb1d3` | 8 | Wire `*learnings.Store` into `analyze.Deps`; `LEARNINGS_DATABASE_URL` env (falls back to `DATABASE_URL`) |
| `25fa748` | 9 | `understand` calls `Store.Nearest(repo, symbol, 3)` and emits matches as `prior_learnings` (omitted when empty) |
| `28bcb30` | 10 | `review_pr_post` persists one `learnings.Record` per changed symbol on successful non-dry-run post |
| `9d65b6c` | 11 | `Store.NearestVector(ctx, query, k)` with pgvector HNSW `vector_cosine_ops`; gated on embedder |
| `d0d57bc` | 12 | End-to-end test: review→understand loop, skipped without `DATABASE_URL` |
| `894b410` | 13 | CLAUDE.md + README document the loop + `AUTO_INDEX_DIRS` |
| `c22ef6c` | post-plan | **Refactor**: split `verdict` into orthogonal `risk_level` (`review_pr`, low/med/high) + `review_outcome` (`review_pr_post`, good/neutral/bad). Avoids vocabulary collision when a PR is both reviewed and posted. |

### Indexing (`~/deploy/krolik-server/compose/search.yml`)

```yaml
AUTO_INDEX_DIRS=/host/src
PATH_MAPPINGS=/home/krolik:/host
```

Lazy per-repo indexing on first semantic query, not eager at boot. Latency baseline: `docs/memos/2026-04-17-auto-index-latency.md`.

### Client (`/home/krolik/.claude/` — Linux adaptation of the plan's mac paths)

**Slash commands** (all take `$1` = absolute path or GitHub slug):
- `/gc:understand <repo> <symbol> [focus]` — deep-dive, calls `understand`
- `/gc:impact <repo> <symbol>` — blast radius, calls `prepare_change`
- `/gc:pr <repo> <pr>` — differential review, calls `review_pr`
- `/gc:graph <repo> "<question>"` — Apache AGE NL query, calls `code_graph`
- `/gc:rewrite <repo> <lang> <pattern> <replacement>` — AST refactor preview

**Hooks** (registered in `~/.claude/settings.json`):
- PreToolUse `Edit|Write|MultiEdit` → `go-code-prepare-change.sh` for any `*.go` under `/home/krolik/src/**`. Extracts the target function name via regex over `old_string`, calls `prepare_change`, emits JSON `hookSpecificOutput.additionalContext` so Claude sees blast radius before touching the file. Verified live.
- PostToolUse `Bash` → `go-code-auto-review-pr.sh`. Self-filters on `gh pr create` substring in the command, parses the PR URL from `tool_response.output`, calls `review_pr`, injects the review as additionalContext.

## Deviations from the plan

1. **Client paths**: plan assumed mac (`/Users/anatoliikoptev/.claude/`) — we shipped to Linux (`/home/krolik/.claude/`). Same structure, different prefix.
2. **MCP endpoint**: plan used `https://mcp.krolik.run/code/mcp` with Bearer from `.claude.json`. Linux host is loopback-only, so hooks POST to `http://127.0.0.1:8897/mcp` without auth headers.
3. **SSE response**: MCP over Streamable-HTTP returns `event: message\ndata: {...}\n`. Plan's `curl | jq` would silently swallow this. Hooks now extract the `data:` line before parsing.
4. **Hook context channel**: plan wrote to stderr with `exit 0`. Claude Code harness ignores this unless you exit 2 (blocking). Hooks now emit valid `{"hookSpecificOutput":{"hookEventName":"...","additionalContext":"..."}}` on stdout.
5. **`AUTO_INDEX_DIRS` location**: plan put it in `deploy/go-code.env`. It actually lives in the shared `~/deploy/krolik-server/compose/search.yml`. Same runtime effect.
6. **MCP catalog memory (Task 5)**: no direct analogue of mac's `mcp-tools-catalog.md` here. Added a one-line pointer in `~/.claude/projects/-home-krolik/memory/MEMORY.md` instead.

## Verification

- `mcp__go-code__understand /home/krolik/src/go-code handleReviewPRPost` → returns symbol+callees+callers, complexity=13, no error — Deps wiring healthy.
- `mcp__go-code__prepare_change` via raw curl → full blast-radius JSON with `impact.direct_callers` — compound aggregator live.
- PreToolUse hook: performed a real noop Edit on `helpers.go:wrapCDATA` twice, each time Claude Code injected the blast-radius as `PreToolUse:Edit hook additional context` with 2 callers (`convertTraceNodes`, `handleCallTrace`). Hook fires, MCP answers, harness forwards.

## Outstanding / follow-ups

- `prior_learnings` field on `understand` output hasn't been observed yet in prod — correct per spec (omitted when `Nearest` returns empty). Will surface organically after the first successful `review_pr_post` writes a row.
- Task 5 mac catalog update is out of scope on this host.
