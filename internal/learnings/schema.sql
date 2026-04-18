CREATE TABLE IF NOT EXISTS review_learnings (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT    NOT NULL,
    symbol      TEXT    NOT NULL,
    risk_level      TEXT,                          -- low | medium | high (from review_pr impact analysis)
    review_outcome  TEXT,                          -- good | neutral | bad (from review_pr_post event)
    flag        TEXT    NOT NULL,                  -- e.g. "policy:forbidden_import"
    note        TEXT    NOT NULL DEFAULT '',
    pr_url      TEXT    NOT NULL DEFAULT '',
    embedding   vector(768),                       -- jina-code-v2 dim (matches existing stack)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS review_learnings_repo_symbol_idx
    ON review_learnings (repo, symbol);

CREATE INDEX IF NOT EXISTS review_learnings_embedding_hnsw
    ON review_learnings
    USING hnsw (embedding vector_cosine_ops);

-- Migration from the legacy single "verdict" column to orthogonal
-- risk_level + review_outcome columns. Idempotent:
--   * ADD COLUMN IF NOT EXISTS handles re-runs.
--   * The DO block's UPDATEs catch undefined_column so they are safe after
--     DROP COLUMN below removes the legacy column.
--   * DROP COLUMN IF EXISTS is a no-op on fresh installs.
ALTER TABLE review_learnings ADD COLUMN IF NOT EXISTS risk_level TEXT;
ALTER TABLE review_learnings ADD COLUMN IF NOT EXISTS review_outcome TEXT;

DO $$
BEGIN
  UPDATE review_learnings SET risk_level = verdict
    WHERE verdict IN ('low','medium','high') AND risk_level IS NULL;
  UPDATE review_learnings SET review_outcome = verdict
    WHERE verdict IN ('good','neutral','bad') AND review_outcome IS NULL;
EXCEPTION WHEN undefined_column THEN
  NULL;
END $$;

ALTER TABLE review_learnings DROP COLUMN IF EXISTS verdict;
