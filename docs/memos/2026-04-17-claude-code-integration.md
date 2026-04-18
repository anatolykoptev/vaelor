# Release memo — Claude Code × go-code integration

**Date:** 2026-04-17
**Branch merged:** `feat/claude-code-integration` → `main` (merge `f981a47`, tail `c22ef6c`)
**Deployed:** 2026-04-18 04:26 UTC, container `go-code` healthy at 04:26:26 UTC

## What shipped

### Server-side (this repo)

1. **Learnings loop end-to-end.**
   - `internal/learnings/` — store wired into `analyze.Deps` (`LEARNINGS_DATABASE_URL`, falls back to `DATABASE_URL`).
   - `review_pr_post` → `Store.Upsert` per changed symbol on successful PostReview (non-dry-run).
   - `review_pr` → `Store.Upsert` with `risk_level` on impact-analysis completion (pre-existing writer, now aligned to the new schema).
   - `understand` → `Store.Nearest(repo, symbol, 3)` + emits `prior_learnings` field (`omitempty`).
   - `Store.NearestVector(ctx, query, k)` for pgvector similarity (opt-in when an `Embedder` is configured).

2. **Schema alignment.** `review_learnings` lost the ambiguous `verdict` column and gained two orthogonal nullable columns:
   - `risk_level text` — `low|medium|high` (from `review_pr` impact analysis).
   - `review_outcome text` — `good|neutral|bad` (from `review_pr_post` event mapping: APPROVE/REQUEST_CHANGES/else).
   - Migration is idempotent (`ADD COLUMN IF NOT EXISTS` + guarded UPDATE + `DROP COLUMN IF EXISTS`). Ran automatically on container start against both `gocode` and `memos` DBs — verified by `\d review_learnings`.

3. **Tests.** Unit-level (`internal/compound/understand_learnings_test.go`, `internal/learnings/store_test.go`, `cmd/go-code/tool_review_pr_post_learnings_test.go`) + an integration-gated e2e (`cmd/go-code/review_understand_learnings_e2e_test.go`) that round-trips three outcomes through a real pgvector. All green under both skip and real-DB paths.

### Client-side (user mac `~/.claude/`)

5 slash commands + 2 hooks:

- `/gc-understand` → `mcp__go-code__understand` with `include_callers`.
- `/gc-impact` → `mcp__go-code__prepare_change`.
- `/gc-pr` → `mcp__go-code__review_pr`.
- `/gc-graph` → `mcp__go-code__code_graph` (NL → Cypher).
- `/gc-rewrite` → `mcp__go-code__rewrite`.
- `PreToolUse` hook (`~/.claude/hooks/go-code-prepare-change.sh`) — on `Edit|Write|MultiEdit` of `*.go` under `$HOME/src/`, extracts the first `func` name from `old_string`, calls `prepare_change`, echoes blast-radius to stderr. Non-blocking (`trap 'exit 0' ERR`).
- `PostToolUse` hook (`~/.claude/hooks/go-code-auto-review-pr.sh`) — on `Bash(gh pr create)`, pulls the PR URL from the tool response and runs `review_pr` (depth=2), echoes review to stderr.
- `mcp-tools-catalog.md` now surfaces the compound tools, slash commands, hooks, and auto-indexing.

## End-to-end verification

- From mac: `mcp__go-code__understand(repo="/home/krolik/src/go-code", symbol="handleReviewPRPost")` returned a JSON result including `prior_learnings: [{risk_level:"medium", review_outcome:"good", flag:"style", …}]` after seeding one test row directly via `psql`. Confirms Store reads plumb through `Deps → compound.Understand → fetchPriorLearnings → Record → JSON`.
- Test seed cleaned up post-verification (3 pre-existing rows from `foo/bar`/`Svc.DoThing` retained).

## Known behavior corrections

- **Auto-indexing is NOT lazy.** Container log on startup: `autoindex: indexing repos sequentially repos=42` followed by sequential `autoindex: done repo=code_XXXX indexed=N skipped=M` lines. The previous `docs/memos/2026-04-17-auto-index-latency.md` claim of "lazy per-repo on first query" was wrong — that latency memo needs correction. The compose-level `AUTO_INDEX_DIRS=/host/src` sweep runs at boot.

## Follow-ups

1. `tool_review_pr.go` and `tool_review_pr_post.go` still hold two `Upsert` paths that both open pgx pools. Consolidation worth a small refactor (share `deps.Learnings`).
2. `store.Nearest` scans into `*string` for the two nullable columns — callers currently deref unconditionally. Guarded everywhere today but worth a helper.
3. Deploy uses `--no-cache` every time (~4 min builds) — harmless but the Makefile target could accept `--cache-from=<image>` for faster iteration.
4. The MCP catalog memory still frames learnings as "Phase 3 upcoming" — update to past tense.

## Commits on `main` delivered by this initiative

```
c22ef6c refactor(learnings): split verdict into risk_level + review_outcome
028a666 docs(memos): document verdict-vocabulary conflict follow-up    ← stale, see c22ef6c; retained in history
f981a47 merge: Claude Code × go-code integration (learnings loop + client hooks)
894b410 docs: document learnings loop + AUTO_INDEX_DIRS
d0d57bc test(learnings): e2e verification of review→understand loop
9d65b6c feat(learnings): add pgvector similarity search
28bcb30 feat(review): persist verdicts to learnings store after PR post
25fa748 feat(understand): surface up to 3 prior learnings per symbol
6acb1d3 feat(learnings): wire Store into analyze.Deps
ddcf9c3 docs: add plan for Claude Code × go-code integration
```
