# Claude Code × go-code Integration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn go-code from a raw MCP server into a first-class Claude Code collaborator: proactive pre-edit risk checks, slash commands, auto-indexed projects, and a working learnings memory that surfaces prior verdicts.

**Architecture:**
- **Client side** (user mac `~/.claude/`): slash commands + PreToolUse/PostToolUse hooks that call the go-code MCP over the existing HTTP endpoint.
- **Server side** (`ssh example`, `$REPO_ROOT/`): enable `AUTO_INDEX_DIRS` for zero-cold-start semantic search; wire the dormant `internal/learnings` package into `understand` (read) and `review_pr_post` (write); add vector similarity to `Nearest`.

**Tech Stack:** bash + jq (hooks), markdown (slash commands), Go 1.24 + pgx + pgvector (server), Apache AGE (existing graph), `make deploy` (docker compose) for rollout.

**Execution mode:** Subagent-driven. Each task is self-contained and has explicit acceptance criteria.

**Cross-session paths:**
- Client: `~/.claude/{commands,hooks,settings.json}` on mac.
- Server: `ssh example` → `cd $REPO_ROOT` → `GOWORK=off make test` → `make deploy`.

---

## Phase 1 — Client-side integration (mac)

Each task below runs in the user's main mac session. No git worktree required — `~/.claude/` is not a repo.

### Task 1: Slash command `/gc:understand`

**Files:**
- Create: `~/.claude/commands/gc-understand.md`

**Step 1: Write the command file**

```markdown
---
description: Deep-dive into a symbol via go-code (aggregates call graph + complexity + prior learnings)
argument-hint: <repo> <symbol> [focus]
---

Use the go-code MCP tool `mcp__go-code__understand` with:
- repo: $1 (if absolute path starting with /, pass as-is; else treat as GitHub slug)
- symbol: $2
- focus: $3 (optional subdir filter)
- include_callers: true

Summarize for the user:
1. Symbol location + signature
2. Direct callers (top 5 by depth)
3. Callees and cross-package fan-out
4. Any prior learnings surfaced (flag, note, PR URL)
5. Risk level for modification

Do NOT restate the full JSON — extract the signal.
```

**Step 2: Verify it is discoverable**

Run: `ls ~/.claude/commands/gc-understand.md`
Expected: file exists.

**Step 3: Commit** — skip; `~/.claude/` is user-global, not a git repo.

---

### Task 2: Slash commands `/gc:impact`, `/gc:pr`, `/gc:graph`, `/gc:rewrite`

**Files:**
- Create: `~/.claude/commands/gc-impact.md`
- Create: `~/.claude/commands/gc-pr.md`
- Create: `~/.claude/commands/gc-graph.md`
- Create: `~/.claude/commands/gc-rewrite.md`

**Step 1: `gc-impact.md`**

```markdown
---
description: Pre-change risk assessment (blast radius + dead-code check)
argument-hint: <repo> <symbol>
---

Call `mcp__go-code__prepare_change` with repo=$1 symbol=$2.
Report: risk tier (low/medium/high), affected-packages count, direct callers, dead-code status.
If tier >= medium, flag the top 3 callers explicitly so the user can review them.
```

**Step 2: `gc-pr.md`**

```markdown
---
description: Review a pull request via go-code differential analysis
argument-hint: <repo> <pr-number>
---

Call `mcp__go-code__review_pr` with repo=$1 pr=$2 depth=2.
Summarize: changed symbols, blast radius, untested changes, policy violations.
Ask the user if they want to post the review (`review_pr_post` with event=COMMENT).
```

**Step 3: `gc-graph.md`**

```markdown
---
description: Query the go-code knowledge graph with a natural-language question
argument-hint: <repo> "<question>"
---

Call `mcp__go-code__code_graph` with repo=$1 query=$2.
Useful questions: "show communities", "find hidden dependencies", "what changed in the graph",
"who calls X", "package with highest fan-in".
```

**Step 4: `gc-rewrite.md`**

```markdown
---
description: AST search-and-replace via go-code rewrite (preview unified diff)
argument-hint: <repo> <language> <pattern> <replacement>
---

Call `mcp__go-code__rewrite` with repo=$1, language=$2, pattern=$3, rewrite=$4, max_results=20.
$WILDCARDS are allowed in both pattern and rewrite.
ALWAYS show the diff to the user before anything is applied.
```

**Step 5: Verify**

Run: `ls ~/.claude/commands/gc-*.md | wc -l`
Expected: `5`.

---

### Task 3: PreToolUse hook — `prepare_change` before Edit on Go files

**Files:**
- Create: `~/.claude/hooks/go-code-prepare-change.sh`
- Modify: `~/.claude/settings.json` (register hook)

**Step 1: Write the hook script**

```bash
#!/usr/bin/env bash
# Before Edit/Write on *.go in a known repo, fetch blast-radius hints via go-code
# and print them to stderr so Claude sees them in its next turn.
set -euo pipefail

input=$(cat)
tool=$(echo "$input" | jq -r '.tool_name // ""')
path=$(echo "$input" | jq -r '.tool_input.file_path // ""')

case "$tool" in Edit|Write|MultiEdit) ;; *) exit 0 ;; esac
[[ "$path" != *.go ]] && exit 0

repo_root=$(cd "$(dirname "$path")" 2>/dev/null && git rev-parse --show-toplevel 2>/dev/null || echo "")
[[ -z "$repo_root" ]] && exit 0

# For v1: only trigger for repos under ~/src/ to avoid surprising hook activity.
[[ "$path" != */src/* ]] && exit 0

old=$(echo "$input" | jq -r '.tool_input.old_string // .tool_input.content // ""')
sym=$(echo "$old" | grep -oE 'func +(\([^)]*\) +)?[A-Za-z_][A-Za-z0-9_]*' | head -1 | awk '{print $NF}' || true)
[[ -z "$sym" ]] && exit 0

token=$(jq -r '.mcpServers."go-code".headers.Authorization' ~/.claude.json 2>/dev/null | sed 's/Bearer //')
[[ -z "$token" || "$token" == "null" ]] && exit 0

body=$(jq -nc --arg repo "$repo_root" --arg sym "$sym" \
  '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:"prepare_change",arguments:{repo:$repo,symbol:$sym}}}')

resp=$(curl -sS -X POST https://your-host.example.com/code/mcp \
  -H "Authorization: Bearer $token" -H "Content-Type: application/json" \
  --max-time 8 -d "$body" 2>/dev/null || true)

[[ -z "$resp" ]] && exit 0
echo "=== go-code prepare_change($sym) ===" >&2
echo "$resp" | jq -r '.result.content[0].text // ""' | head -20 >&2 || true
exit 0
```

**Step 2: Make it executable**

Run: `chmod +x ~/.claude/hooks/go-code-prepare-change.sh`

**Step 3: Register in settings.json**

Add under `hooks.PreToolUse`:

```json
{
  "matcher": "Edit|Write|MultiEdit",
  "hooks": [
    { "type": "command", "command": "~/.claude/hooks/go-code-prepare-change.sh" }
  ]
}
```

**Step 4: Smoke test**

Trigger an Edit on any `*.go` file under `~/src/`. Confirm stderr shows a blast-radius block.

---

### Task 4: PostToolUse hook — self-review after `gh pr create`

**Files:**
- Create: `~/.claude/hooks/go-code-auto-review-pr.sh`
- Modify: `~/.claude/settings.json`

**Step 1: Write the script**

```bash
#!/usr/bin/env bash
# After a successful `gh pr create`, extract the PR URL and call review_pr on it.
set -euo pipefail

input=$(cat)
tool=$(echo "$input" | jq -r '.tool_name // ""')
[[ "$tool" != "Bash" ]] && exit 0

cmd=$(echo "$input" | jq -r '.tool_input.command // ""')
[[ "$cmd" != *"gh pr create"* ]] && exit 0

out=$(echo "$input" | jq -r '.tool_response.output // ""')
url=$(echo "$out" | grep -oE 'https://github.com/[^/]+/[^/]+/pull/[0-9]+' | head -1)
[[ -z "$url" ]] && exit 0

repo=$(echo "$url" | sed -E 's|https://github.com/||; s|/pull/[0-9]+$||')
pr=$(echo "$url" | grep -oE '[0-9]+$')

token=$(jq -r '.mcpServers."go-code".headers.Authorization' ~/.claude.json 2>/dev/null | sed 's/Bearer //')
[[ -z "$token" || "$token" == "null" ]] && exit 0

body=$(jq -nc --arg repo "$repo" --argjson pr "$pr" \
  '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:"review_pr",arguments:{repo:$repo,pr:$pr,depth:2}}}')

resp=$(curl -sS -X POST https://your-host.example.com/code/mcp \
  -H "Authorization: Bearer $token" -H "Content-Type: application/json" \
  --max-time 20 -d "$body" 2>/dev/null || true)

[[ -z "$resp" ]] && exit 0
echo "=== go-code self-review of $url ===" >&2
echo "$resp" | jq -r '.result.content[0].text // ""' | head -40 >&2
exit 0
```

**Step 2:** `chmod +x` and register under `hooks.PostToolUse`:

```json
{
  "matcher": "Bash",
  "hooks": [
    { "type": "command", "command": "~/.claude/hooks/go-code-auto-review-pr.sh" }
  ]
}
```

**Step 3:** Smoke test — create a throwaway PR and confirm the self-review prints to stderr.

---

### Task 5: Update MCP catalog memory

**Files:**
- Modify: `~/.claude/projects/-Users-anatoliikoptev/memory/mcp-tools-catalog.md`

**Step 1:** Replace the **go-code** section with an updated version that highlights:
- Compound tools (`understand`, `prepare_change`) — prefer over manual combinations.
- Hook integrations (`prepare_change` runs automatically before Edit on `*.go` under `~/src/*`).
- Slash commands: `/gc:understand`, `/gc:impact`, `/gc:pr`, `/gc:graph`, `/gc:rewrite`.
- `AUTO_INDEX_DIRS` list (from Phase 2) → semantic_search is zero-cold-start.
- `review_pr_post` persists learnings (from Phase 3) → prior verdicts surface in `understand`.

**Step 2:** No commit — user-global memory.

---

## Phase 2 — Proactive indexing (server)

### Task 6: Add `AUTO_INDEX_DIRS` to deploy env

**Files:**
- Modify: `$REPO_ROOT/deploy/go-code.env.example` (on `ssh example`)

**Step 1: Inspect current env**

Run on example: `grep -E 'AUTO_INDEX|PATH_MAPPING' $REPO_ROOT/deploy/go-code.env.example`
If absent, proceed.

**Step 2: Append**

```
# Proactive indexing — warm caches for our active repos.
AUTO_INDEX_DIRS=/host/src/go-code,/host/src/piter-now,/host/src/memdb,/host/src/go-wowa,/host/src/ox-billing
PATH_MAPPINGS=/home/user:/host
```

**Step 3: Redeploy**

Run: `ssh example "cd $REPO_ROOT && GOWORK=off make deploy"`
Expected: `docker compose ... up -d` completes.

**Step 4: Verify**

Run (after ~60s):
```
ssh example "docker logs go-code --since 2m 2>&1 | grep -iE 'auto.?index|indexed'"
```
Expected: one "indexed /host/src/..." line per AUTO_INDEX_DIRS entry.

---

### Task 7: Cold-start semantic_search smoke test

**Files:** none — verification only.

**Step 1:** From mac, call `mcp__go-code__semantic_search` with:
- `repo: "/home/user/src/piter-now"`
- `query: "publish article"`
- `top_k: 3`

Expected: 3 results in <2s.

**Step 2:** Record p50/p95 latency in `docs/memos/2026-04-17-auto-index-latency.md`.

---

## Phase 3 — Finish the `learnings` store

`internal/learnings/store.go` exports `Upsert` and `Nearest` but **no tool calls them**. Scope: wire into `understand` (read) + `review_pr_post` (write) + add real pgvector similarity in `Nearest`.

### Task 8: Add `learnings.Store` to `analyze.Deps`

**Files:**
- Modify: `$REPO_ROOT/internal/analyze/deps.go`
- Modify: `$REPO_ROOT/cmd/go-code/register.go`
- Modify: `$REPO_ROOT/cmd/go-code/config.go`

**Step 1: Failing test**

Create `$REPO_ROOT/internal/analyze/deps_learnings_test.go`:

```go
package analyze

import "testing"

func TestDeps_HasLearnings(t *testing.T) {
    var d Deps
    _ = d.Learnings // must compile — type *learnings.Store
}
```

**Step 2: Run → fails**

Run: `ssh example "cd $REPO_ROOT && GOWORK=off go test ./internal/analyze/..."`
Expected: `d.Learnings undefined`.

**Step 3: Add the field**

In `internal/analyze/deps.go`:

```go
import "github.com/anatolykoptev/go-code/internal/learnings"

type Deps struct {
    // ...existing fields...
    Learnings *learnings.Store
}
```

**Step 4: Wire construction in `register.go`**

After the existing `deps := analyze.Deps{...}`:

```go
if cfg.LearningsDSN != "" {
    ls, err := learnings.New(context.Background(), cfg.LearningsDSN, nil)
    if err != nil {
        slog.Warn("learnings store disabled", "err", err)
    } else {
        deps.Learnings = ls
    }
}
```

**Step 5: Config field**

In `cmd/go-code/config.go`:

```go
LearningsDSN string // falls back to DATABASE_URL if unset
```

And in the constructor:

```go
c.LearningsDSN = env.String("LEARNINGS_DATABASE_URL", os.Getenv("DATABASE_URL"))
```

**Step 6: Run tests + build**

```
ssh example "cd $REPO_ROOT && GOWORK=off go test ./... && GOWORK=off go build ./..."
```
Expected: pass.

**Step 7: Commit**

```
feat(learnings): wire Store into analyze.Deps
```

---

### Task 9: Surface prior learnings in `understand` output

**Files:**
- Modify: `$REPO_ROOT/cmd/go-code/tool_understand.go`
- Modify: `$REPO_ROOT/internal/compound/understand.go` (extend result with `PriorLearnings []learnings.Record`)

**Step 1: Failing test**

Create `$REPO_ROOT/cmd/go-code/tool_understand_learnings_test.go` — seed a fake Store with one Record for `(repo="r", symbol="Foo")`, call `handleUnderstand`, assert the output contains the Note text under `<prior_learnings>`.

**Step 2: Run → fails** (`PriorLearnings` undefined).

**Step 3: Implement**

In `internal/compound/understand.go`, after building `Result`:

```go
if deps.Learnings != nil {
    recs, _ := deps.Learnings.Nearest(ctx, repoKey, sym.Name, 3)
    result.PriorLearnings = recs
}
```

In `tool_understand.go` renderer, emit `<prior_learnings>` XML block with flag/note/PR URL per record.

**Step 4: Run → passes.** Build + lint.

**Step 5: Commit**

```
feat(understand): surface up to 3 prior learnings per symbol
```

---

### Task 10: Persist verdicts from `review_pr_post`

**Files:**
- Modify: `$REPO_ROOT/cmd/go-code/tool_review_pr_post.go`

**Step 1: Failing test**

In `tool_review_pr_post_learnings_test.go`: on a successful `PostReview` call, assert `Store.Upsert` is invoked once per changed symbol, with verdict derived from `event` (`APPROVE→"good"`, `COMMENT→"neutral"`, `REQUEST_CHANGES→"bad"`).

**Step 2: Implement**

After `g.PostReview(...)` succeeds in `handleReviewPRPost`, loop over `result.ChangedSymbols`:

```go
if deps.Learnings != nil {
    verdict := verdictFromEvent(event)
    for _, s := range result.ChangedSymbols {
        _ = deps.Learnings.Upsert(ctx, learnings.Record{
            Repo: input.Repo, Symbol: s.Name, Verdict: verdict,
            Flag: s.Flag, Note: s.Note, PRURL: url,
        })
    }
}
```

**Step 3: Run tests + build + deploy.**

**Step 4: Commit**

```
feat(review): persist verdicts to learnings store after PR post
```

---

### Task 11: Implement pgvector similarity in `Nearest`

**Files:**
- Modify: `$REPO_ROOT/internal/learnings/store.go`
- Modify: `$REPO_ROOT/internal/learnings/schema.sql`

**Step 1: Inspect schema**

Run: `ssh example "cat $REPO_ROOT/internal/learnings/schema.sql"`
If no vector column/index, add:

```sql
ALTER TABLE review_learnings
  ADD COLUMN IF NOT EXISTS embedding vector(768);
CREATE INDEX IF NOT EXISTS review_learnings_embedding_idx
  ON review_learnings USING hnsw (embedding vector_cosine_ops);
```

**Step 2: Failing test**

Extend `internal/learnings/store_test.go` with `TestStore_NearestByVector` — seed 3 records with known embeddings; query with a 4th; assert the closest comes first.

**Step 3: Extend store**

```go
func (s *Store) NearestVector(ctx context.Context, query string, k int) ([]Record, error)
```
embeds `query` via the `Embedder`, runs `SELECT ... ORDER BY embedding <=> $1 LIMIT $2`.
Keep existing exact-match `Nearest(repo, symbol)` for the fast path.

**Step 4: Test against real pgvector on example**

```
ssh example "cd $REPO_ROOT && GOWORK=off go test ./internal/learnings/... -tags=integration"
```
If the `integration` build tag doesn't exist yet, gate with `t.Skip` when `DATABASE_URL` is empty.

**Step 5: Commit**

```
feat(learnings): add pgvector similarity search
```

---

### Task 12: End-to-end integration test

**Files:**
- Create: `$REPO_ROOT/cmd/go-code/learnings_e2e_test.go`

**Step 1:** Write a test that:
1. Starts the MCP server in-process with a test Postgres (or `t.Skip` when `DATABASE_URL` unset).
2. Calls `review_pr_post` in dry-run against a fixture repo+PR.
3. Verifies at least one row appears in `review_learnings`.
4. Calls `understand` on one of those symbols.
5. Asserts the output contains the prior flag/note.

**Step 2:** Run → passes locally; skips without DB env.

**Step 3: Commit + deploy**

```
test(learnings): e2e verification of review→understand loop
```

---

## Phase 4 — Documentation & release

### Task 13: Update `CLAUDE.md` + `README.md`

**Files:**
- Modify: `$REPO_ROOT/CLAUDE.md` — add env row `LEARNINGS_DATABASE_URL`; one paragraph on the learnings loop.
- Modify: `$REPO_ROOT/README.md` — under Features: "Learnings — prior review verdicts auto-surface in `understand`".

Run `make lint`; commit:

```
docs: document learnings loop + AUTO_INDEX_DIRS
```

---

### Task 14: Release + end-to-end verification from mac

**Files:** none.

**Step 1:** `ssh example "cd $REPO_ROOT && GOWORK=off make deploy"`

**Step 2:** From mac: `/gc:understand $REPO_ROOT handleReviewPRPost`
Expected: output includes `<prior_learnings>` (tag present even if empty).

**Step 3:** Post a dry-run review on an existing PR; re-run step 2; prior learnings count should go up by at least 1.

**Step 4:** Save release memo at `docs/memos/2026-04-17-claude-code-integration.md`.

---

## Dependency graph

- Phase 1 tasks (1–5) are **parallelisable** — no shared files.
- Phase 2 (6–7) is independent of Phase 1.
- Phase 3 is **sequential**: 8 → 9 → 10 → 11 → 12 (shared Go packages).
- Phase 4 (13–14) runs last.

Suggested dispatch: 5 subagents in parallel for Phase 1; 1 subagent for Phase 2; 1 subagent per task for Phase 3 with review between each; 1 subagent for Phase 4.

## Acceptance checklist

- [ ] `/gc:*` slash commands resolvable from mac.
- [ ] PreToolUse hook emits blast-radius to stderr for at least one test edit.
- [ ] PostToolUse hook self-reviews a fresh PR.
- [ ] `docker logs go-code` shows `indexed` lines per AUTO_INDEX_DIRS entry.
- [ ] `mcp__go-code__understand` returns `<prior_learnings>` element.
- [ ] `review_pr_post` insertion observable via `psql -c "SELECT count(*) FROM review_learnings"`.
- [ ] `NearestVector` returns ordered-by-similarity rows.
- [ ] README + CLAUDE.md reference the new behaviour.
