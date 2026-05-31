-- =============================================================================
-- ONE-SHOT OPERATOR MIGRATION — do NOT execute automatically.
--
-- Purpose: recover data stranded in ag_catalog.code_{embeddings,repo_state,
--          health_cache} due to the search_path leak fixed in PR #173, then
--          drop the stale ag_catalog copies.
--
-- Background: Before PR #173, acquireAGE dirtied the pool connection with
--   SET search_path TO ag_catalog, "$user", public
--   and no AfterRelease reset. Any subsequent bare DML (INSERT/UPDATE/SELECT
--   on code_repo_state, code_embeddings, code_health_cache) resolved those
--   names against ag_catalog instead of public. Over time:
--     - 24+ repos have embeddings ONLY in ag_catalog.code_embeddings (~857 MB)
--     - Some repos have ag_catalog.code_repo_state rows newer than public
--     - code_health_cache has scattered rows in both schemas
--
-- Pre-conditions (VERIFY ALL before running):
--   1. SR-A (DISCARD ALL AfterRelease hook) is deployed and running.
--   2. gocode_schema_drift_total counter is NO LONGER climbing
--      (prometheus: rate(gocode_schema_drift_total[5m]) == 0 for >10 min).
--      If it is still rising, new rows are still being written to ag_catalog —
--      running this migration early will not help and may race with live writes.
--   3. No active indexing jobs are running (check go-code logs / /metrics).
--   4. You have a recent backup or can restore from the public.* tables
--      (these are the authoritative tables after SR-A ships).
--
-- Run as: psql "$DATABASE_URL" -f 20260531_backfill_ag_catalog_leak.sql
--
-- Estimated runtime: depends on embedding count. 857 MB of embeddings is
--   roughly 1–2 min on typical Postgres hardware. Run in a transaction so
--   the backfill and the DROP are atomic — if the DROP fails the backfill
--   rolls back and no data is lost.
-- =============================================================================

BEGIN;

-- -------------------------------------------------------------------------
-- SECTION 1: Backfill public.code_repo_state from ag_catalog.code_repo_state
--
-- Strategy: take the NEWER row (by indexed_at) per repo_key.
--   - Most repos have the same sha in both schemas.
--   - At least 1 repo has ag_catalog.indexed_at > public.indexed_at (operator
--     confirmed), meaning the ag_catalog copy is the most recent successful index.
--   - INSERT … ON CONFLICT DO UPDATE SET … WHERE EXCLUDED.indexed_at > indexed_at
--     upserts only if the ag_catalog row is genuinely newer.
-- -------------------------------------------------------------------------

DO $$
DECLARE
    r record;
    backfilled int := 0;
BEGIN
    -- Only attempt if the source table exists.
    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = 'code_repo_state' AND n.nspname = 'ag_catalog'
    ) THEN
        RAISE NOTICE 'ag_catalog.code_repo_state does not exist — nothing to backfill';
        RETURN;
    END IF;

    FOR r IN SELECT repo_key, head_sha, indexed_at FROM ag_catalog.code_repo_state LOOP
        INSERT INTO public.code_repo_state (repo_key, head_sha, indexed_at)
        VALUES (r.repo_key, r.head_sha, r.indexed_at)
        ON CONFLICT (repo_key) DO UPDATE
            SET head_sha   = EXCLUDED.head_sha,
                indexed_at = EXCLUDED.indexed_at
            WHERE EXCLUDED.indexed_at > public.code_repo_state.indexed_at;
        backfilled := backfilled + 1;
    END LOOP;

    RAISE NOTICE 'code_repo_state: processed % ag_catalog rows (newer-wins upsert)', backfilled;
END $$;

-- -------------------------------------------------------------------------
-- SECTION 2: Backfill public.code_embeddings from ag_catalog.code_embeddings
--
-- Strategy: INSERT … ON CONFLICT DO NOTHING — public rows are authoritative
-- (created/updated by embeddings.Store.Upsert which uses schema-qualified DML
-- post SR-B). The ag_catalog rows are the only copy for ~24 repos that were
-- exclusively indexed before PR #173. Inserting them fills the gap.
-- -------------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = 'code_embeddings' AND n.nspname = 'ag_catalog'
    ) THEN
        RAISE NOTICE 'ag_catalog.code_embeddings does not exist — nothing to backfill';
        RETURN;
    END IF;

    -- Ensure pgvector extension is present (required for the vector column).
    CREATE EXTENSION IF NOT EXISTS vector;

    INSERT INTO public.code_embeddings
        (repo_key, file_path, symbol_name, symbol_kind, language,
         start_line, body_hash, embedding, updated_at)
    SELECT
        repo_key, file_path, symbol_name, symbol_kind, language,
        start_line, body_hash, embedding, updated_at
    FROM ag_catalog.code_embeddings
    ON CONFLICT (repo_key, file_path, symbol_name) DO NOTHING;

    RAISE NOTICE 'code_embeddings: backfill from ag_catalog complete (ON CONFLICT DO NOTHING)';
END $$;

-- -------------------------------------------------------------------------
-- SECTION 3: Backfill public.code_health_cache from ag_catalog.code_health_cache
--
-- Strategy: INSERT … ON CONFLICT DO NOTHING — public rows win; ag_catalog rows
-- fill gaps. Health cache is regenerated on the next code_health call so this
-- is low-stakes; backfill only to avoid stale "no data" responses.
-- -------------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = 'code_health_cache' AND n.nspname = 'ag_catalog'
    ) THEN
        RAISE NOTICE 'ag_catalog.code_health_cache does not exist — nothing to backfill';
        RETURN;
    END IF;

    INSERT INTO public.code_health_cache
        SELECT * FROM ag_catalog.code_health_cache
    ON CONFLICT DO NOTHING;

    RAISE NOTICE 'code_health_cache: backfill from ag_catalog complete';
END $$;

-- -------------------------------------------------------------------------
-- SECTION 4: DROP the stale ag_catalog copies
--
-- OPERATOR ACK REQUIRED: these DROPs are DESTRUCTIVE and IRREVERSIBLE.
-- Only run after:
--   a) Sections 1–3 committed successfully (check NOTICE output above).
--   b) gocode_schema_drift_total counter is no longer climbing.
--   c) You have verified SELECT COUNT(*) on public.* tables matches expectations.
--
-- Do NOT touch these 4 by-design ag_catalog tables (they are AGE bookkeeping):
--   ag_catalog.code_graph_meta
--   ag_catalog.code_file_mtimes
--   ag_catalog.code_graph_snapshots
--   ag_catalog.code_dead_code_scores
-- Do NOT drop any ag_catalog.ag_* objects (AGE internal state).
-- -------------------------------------------------------------------------

-- Uncomment to execute drops — intentionally commented out as a safety gate.
-- Remove the comment block below only after verifying sections 1–3 succeeded.

/*
DROP TABLE IF EXISTS ag_catalog.code_repo_state;
DROP TABLE IF EXISTS ag_catalog.code_embeddings;
DROP TABLE IF EXISTS ag_catalog.code_health_cache;
*/

-- After uncommenting and running, verify with:
--   SELECT COUNT(*) FROM pg_class c
--   JOIN pg_namespace n ON n.oid = c.relnamespace
--   WHERE c.relname IN ('code_repo_state','code_embeddings','code_health_cache')
--     AND n.nspname = 'ag_catalog';
-- Expected: 0 rows.

COMMIT;

-- =============================================================================
-- Post-run checklist:
--   [ ] gocode_schema_drift_total == 0 for all three tables for >10 min post-run
--   [ ] Semantic search still returns results (spot-check 2–3 repos that were
--       ag_catalog-only, e.g. semantic_search on a recently-indexed repo)
--   [ ] code_repo_state row counts match pre-migration total
--       (SELECT COUNT(*) FROM public.code_repo_state)
--   [ ] No new ERRORs in go-code logs matching "schema_drift"
-- =============================================================================
