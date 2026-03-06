package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

const (
	defaultTopK = 20
	maxTopK     = 100
	dimSize     = 768
	batchSize   = 50
	fieldsPerRec = 8
)

const schemaSQL = `CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS code_embeddings (
    repo_key TEXT NOT NULL, file_path TEXT NOT NULL, symbol_name TEXT NOT NULL,
    symbol_kind TEXT NOT NULL, language TEXT NOT NULL DEFAULT '',
    start_line INT NOT NULL DEFAULT 0, body_hash BIGINT NOT NULL DEFAULT 0,
    embedding vector(768) NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_key, file_path, symbol_name));
CREATE INDEX IF NOT EXISTS idx_code_embeddings_repo ON code_embeddings (repo_key);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_hnsw ON code_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)`

// EmbeddingRecord holds a single symbol embedding for storage.
type EmbeddingRecord struct {
	RepoKey    string
	FilePath   string
	SymbolName string
	SymbolKind string // function, method, class, etc.
	Language   string
	StartLine  int
	BodyHash   uint64 // for change detection
	Embedding  []float32
}

// SearchOpts controls search filtering and result count.
type SearchOpts struct {
	RepoKey  string // optional filter
	Language string // optional filter
	TopK     int    // default 20, max 100
}

// SearchResult is a single semantic search hit.
type SearchResult struct {
	RepoKey    string
	FilePath   string
	SymbolName string
	SymbolKind string
	Language   string
	StartLine  int
	Distance   float32 // cosine distance (lower = more similar)
}

// Store manages vector embeddings in PostgreSQL with pgvector.
type Store struct {
	pool    *pgxpool.Pool
	once    sync.Once
	initErr error
}

// NewStore creates a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// EnsureSchema creates the pgvector extension and embeddings table if needed.
func (s *Store) EnsureSchema(ctx context.Context) error {
	s.once.Do(func() {
		_, s.initErr = s.pool.Exec(ctx, schemaSQL)
		if s.initErr != nil {
			slog.Error("embeddings: schema init failed", slog.Any("error", s.initErr))
		}
	})
	return s.initErr
}

// Upsert stores embeddings for symbols using multi-row INSERT with ON CONFLICT.
func (s *Store) Upsert(ctx context.Context, records []EmbeddingRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	for i := 0; i < len(records); i += batchSize {
		if err := s.upsertBatch(ctx, records[i:min(i+batchSize, len(records))]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) upsertBatch(ctx context.Context, records []EmbeddingRecord) error {
	var b strings.Builder
	b.WriteString(`INSERT INTO code_embeddings
		(repo_key,file_path,symbol_name,symbol_kind,language,start_line,body_hash,embedding,updated_at) VALUES `)
	args := make([]any, 0, len(records)*fieldsPerRec)
	for i, r := range records {
		if i > 0 {
			b.WriteByte(',')
		}
		off := i * fieldsPerRec
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,NOW())",
			off+1, off+2, off+3, off+4, off+5, off+6, off+7, off+8)
		args = append(args, r.RepoKey, r.FilePath, r.SymbolName, r.SymbolKind,
			r.Language, r.StartLine, int64(r.BodyHash), pgvector.NewVector(r.Embedding))
	}
	b.WriteString(` ON CONFLICT (repo_key, file_path, symbol_name) DO UPDATE SET
		symbol_kind=EXCLUDED.symbol_kind, language=EXCLUDED.language,
		start_line=EXCLUDED.start_line, body_hash=EXCLUDED.body_hash,
		embedding=EXCLUDED.embedding, updated_at=NOW()`)
	_, err := s.pool.Exec(ctx, b.String(), args...)
	return err
}

// Search finds the top-K most similar symbols to the query embedding using cosine distance.
func (s *Store) Search(ctx context.Context, query []float32, opts SearchOpts) ([]SearchResult, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	topK := max(min(opts.TopK, maxTopK), 1)
	if opts.TopK <= 0 {
		topK = defaultTopK
	}
	var where []string
	args := []any{pgvector.NewVector(query), topK}
	if opts.RepoKey != "" {
		where = append(where, fmt.Sprintf("repo_key=$%d", len(args)+1))
		args = append(args, opts.RepoKey)
	}
	if opts.Language != "" {
		where = append(where, fmt.Sprintf("language=$%d", len(args)+1))
		args = append(args, opts.Language)
	}
	q := `SELECT repo_key,file_path,symbol_name,symbol_kind,language,start_line,
		embedding <=> $1 AS distance FROM code_embeddings`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY distance LIMIT $2"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("embeddings search: %w", err)
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.RepoKey, &r.FilePath, &r.SymbolName,
			&r.SymbolKind, &r.Language, &r.StartLine, &r.Distance); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// DeleteRepo removes all embeddings for a given repo_key.
func (s *Store) DeleteRepo(ctx context.Context, repoKey string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, "DELETE FROM code_embeddings WHERE repo_key=$1", repoKey)
	return err
}

// Stats returns embedding counts per repo.
func (s *Store) Stats(ctx context.Context) (map[string]int, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT repo_key,COUNT(*) FROM code_embeddings GROUP BY repo_key")
	if err != nil {
		return nil, fmt.Errorf("embeddings stats: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, fmt.Errorf("scan stats: %w", err)
		}
		result[key] = count
	}
	return result, rows.Err()
}
