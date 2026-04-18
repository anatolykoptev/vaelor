# Follow-up: verdict-vocabulary conflict in `review_learnings`

**Date:** 2026-04-17  
**Context:** Discovered during Task 10 of the Claude Code × go-code integration branch. Documented here so it is not forgotten.

## Problem

Two tools now write to `review_learnings.verdict`, using incompatible vocabularies:

| Writer | Verdict values | Semantics |
|---|---|---|
| `cmd/go-code/tool_review_pr.go` (pre-existing, pre-`feat/claude-code-integration`) | `low`, `medium`, `high` | Risk level derived from impact analysis |
| `cmd/go-code/tool_review_pr_post.go` (Task 10, 2026-04-17) | `good`, `neutral`, `bad` | Review outcome mapped from `APPROVE`/`REQUEST_CHANGES`/else |

They model different attributes of a changed symbol and happen to land in the same column.

## Impact

- `understand` emits whatever string is stored — mixed rows in `prior_learnings` will display both vocabularies side by side. Human-readable, but not aggregable.
- No data corruption. No crashes.
- Downstream analytics that group by verdict break semantically.

## Proposed fix (next iteration)

Split into two orthogonal columns:

```sql
ALTER TABLE review_learnings
  ADD COLUMN IF NOT EXISTS risk_level text,      -- low|medium|high (from review_pr)
  ADD COLUMN IF NOT EXISTS review_outcome text;  -- good|neutral|bad (from review_pr_post)
-- Backfill: parse existing `verdict` values into the appropriate column based on vocabulary detection.
-- Drop `verdict` once both writers are migrated.
```

Then:
- `tool_review_pr.go` writes to `risk_level`.
- `tool_review_pr_post.go` writes to `review_outcome`.
- `Store.Nearest` and `Record` expose both.
- `understand` renderer emits whichever is populated (or both).

## Why not do it in this branch

Scope: the current branch ships the read/write plumbing for learnings. Splitting the column is a schema + backfill + dual-write migration that deserves its own review cycle. Ship the loop first, fix the vocabulary separately — both writers functionally work today.

## Tracking

Add as a TODO on the owner of `review_pr` vs `review_pr_post`. Reference this memo from the next PR that touches either file.
