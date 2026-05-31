-- =============================================================================
-- ONE-SHOT OPERATOR MIGRATION — do NOT execute automatically.
--
-- Purpose: recover data stranded in ag_catalog.code_{embeddings,repo_state,
--          health_cache} due to the search_path leak fixed in PR #173, then
--          drop the stale ag_catalog copies.
--
-- Background: Before PR #173, acquireAGE dirtied the pool connection with
--   SET search_path TO ag_catalog, "$user", public
--   and no AfterRelease reset. Any subsequent bare DML on code_repo_state /
--   code_embeddings / code_health_cache resolved against ag_catalog instead of
--   public. Over time: ~24 repos have embeddings ONLY in ag_catalog (~857 MB);
--   ~10 repos have ag_catalog.code_repo_state rows (1 newer than public);
--   code_health_cache has scattered rows in both schemas.
--
-- Pre-conditions (VERIFY ALL before running):
--   1. SR-A (DISCARD ALL AfterRelease hook, PR #173) is DEPLOYED and running.
--   2. gocode_schema_drift_total is NO LONGER climbing
--      (rate(gocode_schema_drift_total[5m]) == 0 for >10 min on :9897/metrics).
--      If it is still rising, new rows are still leaking — fix SR-A deploy first.
--   3. No active indexing jobs (check go-code logs / :9897/metrics).
--   4. public.* are the authoritative tables after SR-A — have a backup.
--
-- =====  RUN ORDER — TWO SEPARATE INVOCATIONS, DIFFERENT ROLES  ================
--   PART A (backfill): safe, additive, idempotent. Run as the app role:
--       psql "$DATABASE_URL"  -f this_file   (only PART A is uncommented)
--       — every statement is schema-qualified, so it is also safe as -U memos.
--   PART B (DROP): DESTRUCTIVE + IRREVERSIBLE. ag_catalog.code_{repo_state,
--       health_cache} are owned by `memos` (not gocode_app) — DROP IF EXISTS
--       does NOT bypass the ownership check, so PART B MUST run as the owner:
--       psql -U memos -d gocode    (paste PART B only, after PART A verified)
--
-- NOTE: backfill is NOT wrapped in a single transaction with the DROP. The two
--   are independent, separately-gated steps. Each PART A section is individually
--   idempotent (newer-wins upsert / ON CONFLICT DO NOTHING) and runs autocommit
--   so the 818 MB embeddings copy does not hold a long transaction / WAL bloat /
--   ROW-EXCLUSIVE lock on public.code_embeddings for its whole duration.
--
-- statement_timeout: the postgres container sets statement_timeout=30s on the
--   command line; the 110k-row HNSW-indexed embeddings INSERT exceeds it and
--   would be CANCELED. PART A raises it to 0 (unlimited) for this session only.
-- =============================================================================


-- #############################################################################
-- ##  PART A — BACKFILL  (run as gocode_app via $DATABASE_URL; autocommit)    ##
-- #############################################################################

SET statement_timeout = 0;   -- session-scoped; the HNSW copy needs >30s

-- -------------------------------------------------------------------------
-- SECTION 1: public.code_repo_state ← ag_catalog.code_repo_state (newer-wins)
--   ON CONFLICT DO UPDATE only when the ag_catalog row is genuinely newer by
--   indexed_at; head_sha + indexed_at move together (no SHA/timestamp split).
-- -------------------------------------------------------------------------
DO $$
DECLARE backfilled int := 0;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE c.relname = 'code_repo_state' AND n.nspname = 'ag_catalog') THEN
        RAISE NOTICE 'ag_catalog.code_repo_state absent — nothing to backfill';
        RETURN;
    END IF;
    INSERT INTO public.code_repo_state (repo_key, head_sha, indexed_at)
    SELECT repo_key, head_sha, indexed_at FROM ag_catalog.code_repo_state
    ON CONFLICT (repo_key) DO UPDATE
        SET head_sha = EXCLUDED.head_sha, indexed_at = EXCLUDED.indexed_at
        WHERE EXCLUDED.indexed_at > public.code_repo_state.indexed_at;
    GET DIAGNOSTICS backfilled = ROW_COUNT;
    RAISE NOTICE 'code_repo_state: % rows inserted-or-updated (newer-wins)', backfilled;
END $$;

-- -------------------------------------------------------------------------
-- SECTION 2: public.code_embeddings ← ag_catalog.code_embeddings
--   SCOPED to repos that exist ONLY in ag_catalog. We do NOT gap-fill repos
--   already present in public: their ag rows are pre-#173 symbols that public's
--   later re-index may have intentionally dropped (deleted/renamed) — inserting
--   them would re-pollute fresh repos with stale symbols. The ~24 ag-only repos
--   are the real recovery target; everything else self-heals on next index.
-- -------------------------------------------------------------------------
DO $$
DECLARE inserted int := 0;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE c.relname = 'code_embeddings' AND n.nspname = 'ag_catalog') THEN
        RAISE NOTICE 'ag_catalog.code_embeddings absent — nothing to backfill';
        RETURN;
    END IF;
    INSERT INTO public.code_embeddings
        (repo_key, file_path, symbol_name, symbol_kind, language,
         start_line, body_hash, embedding, updated_at)
    SELECT repo_key, file_path, symbol_name, symbol_kind, language,
           start_line, body_hash, embedding, updated_at
    FROM ag_catalog.code_embeddings
    WHERE repo_key NOT IN (SELECT DISTINCT repo_key FROM public.code_embeddings)
    ON CONFLICT (repo_key, file_path, symbol_name) DO NOTHING;
    GET DIAGNOSTICS inserted = ROW_COUNT;
    RAISE NOTICE 'code_embeddings: % rows inserted (ag-only repos)', inserted;
END $$;

-- -------------------------------------------------------------------------
-- SECTION 3: public.code_health_cache ← ag_catalog.code_health_cache
--   Low-stakes (regenerated on next code_health call). public wins; ag fills
--   gaps. SELECT * is acceptable here — both schemas verified column-identical
--   and the cache is disposable.
-- -------------------------------------------------------------------------
DO $$
DECLARE inserted int := 0;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE c.relname = 'code_health_cache' AND n.nspname = 'ag_catalog') THEN
        RAISE NOTICE 'ag_catalog.code_health_cache absent — nothing to backfill';
        RETURN;
    END IF;
    INSERT INTO public.code_health_cache
        SELECT * FROM ag_catalog.code_health_cache
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS inserted = ROW_COUNT;
    RAISE NOTICE 'code_health_cache: % rows inserted', inserted;
END $$;

-- ===== VERIFY PART A before PART B =====
--   SELECT COUNT(*) FROM public.code_repo_state;   -- expect +~10 vs pre-run
--   SELECT COUNT(DISTINCT repo_key) FROM public.code_embeddings;  -- +~24 repos
--   Spot-check semantic_search on a previously ag-only repo → returns results.


-- #############################################################################
-- ##  PART B — DROP ORPHANS   (DESTRUCTIVE — run as -U memos, OWNER)          ##
-- ##  Run ONLY after PART A verified AND gocode_schema_drift_total flat >10m.  ##
-- ##  ag_catalog.code_{repo_state,health_cache} are owned by `memos`; DROP     ##
-- ##  IF EXISTS does not bypass ownership → must run as memos.                 ##
-- ##                                                                           ##
-- ##  DO NOT touch the 4 by-design AGE tables:                                 ##
-- ##    ag_catalog.code_graph_meta, code_file_mtimes,                          ##
-- ##    ag_catalog.code_graph_snapshots, code_dead_code_scores                 ##
-- ##  DO NOT drop any ag_catalog.ag_* object (AGE internal state).             ##
-- ##  Uncomment the three lines below to execute:                              ##
-- #############################################################################
--
-- DROP TABLE IF EXISTS ag_catalog.code_repo_state;
-- DROP TABLE IF EXISTS ag_catalog.code_embeddings;
-- DROP TABLE IF EXISTS ag_catalog.code_health_cache;
--
-- Verify after: 0 rows expected →
--   SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
--   WHERE c.relname IN ('code_repo_state','code_embeddings','code_health_cache')
--     AND n.nspname = 'ag_catalog';

-- =============================================================================
-- Post-run checklist:
--   [ ] gocode_schema_drift_total flat (==0 rate) for all 3 tables >10 min
--   [ ] semantic_search returns results for 2-3 formerly ag-only repos
--   [ ] public.code_repo_state count matches pre-migration + ag-only delta
--   [ ] No new "schema_drift" ERRORs in go-code logs
-- =============================================================================
