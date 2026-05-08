# AUTO_INDEX_DIRS cold-start latency (2026-04-17)

**Context:** Task 7 of the Claude Code × go-code integration plan. Verifying that `AUTO_INDEX_DIRS` delivers zero-cold-start semantic_search for our active repos.

**Runtime config:** `~/deploy/my-server/compose/search.yml:269` sets `AUTO_INDEX_DIRS=/host/src` (broader than the plan proposed — covers everything under `/home/user/src/`). `PATH_MAPPINGS=/home/user:/host` already on line 259.

## Observation

Latency is bimodal, not a smooth p50/p95 distribution:

- **Cold (first query per repo)**: returns `<status>indexing</status>` with a 30–60s retry hint. The async indexer kicks off in the background. Measured completion time for piter-now: ~90s (query at 03:19:35 → `background index complete indexed=0 skipped=1256 total=1265`).
- **Warm (subsequent queries)**: <1s, top-k results returned directly.

Example warm call (piter-now, query "publish article", top_k=3): 3 Python-scripted Directus helpers returned at distance 0.59–0.62.

## Implication

`AUTO_INDEX_DIRS=/host/src` does NOT eagerly index repos at container startup; indexing is triggered per-repo on first semantic_search and cached thereafter. The env var scopes WHICH repos are allowed to be auto-indexed, not WHEN.

For true zero-cold-start we would need either (a) an explicit warmup step after container start, or (b) change the indexer to walk AUTO_INDEX_DIRS on boot.

## Recommendation

Track (a) as a follow-up if the cold-start delay becomes user-visible. For now, post-restart usage naturally warms the caches within a few queries per active repo.
