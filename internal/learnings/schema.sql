CREATE TABLE IF NOT EXISTS review_learnings (
    id          BIGSERIAL PRIMARY KEY,
    repo        TEXT    NOT NULL,
    symbol      TEXT    NOT NULL,
    verdict     TEXT    NOT NULL,          -- low | medium | high
    flag        TEXT    NOT NULL,          -- e.g. "policy:forbidden_import"
    note        TEXT    NOT NULL DEFAULT '',
    pr_url      TEXT    NOT NULL DEFAULT '',
    embedding   vector(768),               -- jina-code-v2 dim (matches existing stack)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS review_learnings_repo_symbol_idx
    ON review_learnings (repo, symbol);

CREATE INDEX IF NOT EXISTS review_learnings_embedding_hnsw
    ON review_learnings
    USING hnsw (embedding vector_cosine_ops);
