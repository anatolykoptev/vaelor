package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/go-code/internal/pgutil"
	pgvector "github.com/pgvector/pgvector-go"
)

const (
	defaultTopK  = 20
	maxTopK      = 100
	dimSize      = 768  // jina-code-v2 dense embedding dimension
	sparseDim    = 30522 // splade-v3-distilbert BERT-base WordPiece vocab size (P1: column DDL, P3: HNSW index deferred — sparsevec HNSW unsupported in pgvector 0.8.2)
	batchSize    = 50
	fieldsPerRec = 8
)

// schemaSQL creates the pgvector extension and the two public-schema data tables.
// SR-B: all table references are schema-qualified (public.*) so they resolve
// correctly regardless of the connection's search_path — belt-and-suspenders
// alongside the SR-A pool AfterRelease hook that resets search_path on release.
const schemaSQL = `CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS public.code_embeddings (
    repo_key TEXT NOT NULL, file_path TEXT NOT NULL, symbol_name TEXT NOT NULL,
    symbol_kind TEXT NOT NULL, language TEXT NOT NULL DEFAULT '',
    start_line INT NOT NULL DEFAULT 0, body_hash BIGINT NOT NULL DEFAULT 0,
    embedding vector(768) NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_key, file_path, symbol_name));
CREATE INDEX IF NOT EXISTS idx_code_embeddings_repo ON public.code_embeddings (repo_key);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_hnsw ON public.code_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
ALTER TABLE public.code_embeddings
    ADD COLUMN IF NOT EXISTS sparse_embedding sparsevec(30522);
CREATE TABLE IF NOT EXISTS public.code_repo_state (
    repo_key TEXT PRIMARY KEY,
    head_sha TEXT NOT NULL,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`

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
	RepoKey     string  // optional filter
	Language    string  // optional filter
	TopK        int     // default 20, max 100
	MaxDistance float32 // 0 = no filter; cosine distance threshold (0.0-1.0)
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
	Source     string  // "semantic", "keyword", "hybrid", "graph" — set by caller
	PageRank   float32 // structural importance from graph analysis (0 if not available)
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
// After creating tables it attempts to transfer ownership to CURRENT_USER
// (best-effort: warns instead of failing if the role is not the table owner).
func (s *Store) EnsureSchema(ctx context.Context) error {
	s.once.Do(func() {
		_, s.initErr = s.pool.Exec(ctx, schemaSQL)
		if s.initErr != nil {
			slog.Error("embeddings: schema init failed", slog.Any("error", s.initErr))
			return
		}
		// Best-effort ownership transfer so the connected role can TRUNCATE
		// code_embeddings on reindex without needing explicit grants from an admin.
		for _, tbl := range []string{"public.code_embeddings", "public.code_repo_state"} {
			pgutil.TransferOwnership(ctx, s.pool, "embeddings", tbl)
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
	b.WriteString(`INSERT INTO public.code_embeddings
		(repo_key,file_path,symbol_name,symbol_kind,language,start_line,body_hash,embedding,updated_at) VALUES `)
	args := make([]any, 0, len(records)*fieldsPerRec)
	for i, r := range records {
		if i > 0 {
			b.WriteByte(',')
		}
		off := i * fieldsPerRec
		b.WriteByte('(')
		for j := 1; j <= fieldsPerRec; j++ {
			if j > 1 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d", off+j)
		}
		b.WriteString(",NOW())")
		args = append(args, r.RepoKey, r.FilePath, r.SymbolName, r.SymbolKind,
			r.Language, r.StartLine, int64(r.BodyHash), pgvector.NewVector(r.Embedding))
	}
	b.WriteString(` ON CONFLICT (repo_key, file_path, symbol_name) DO UPDATE SET
		symbol_kind=EXCLUDED.symbol_kind, language=EXCLUDED.language,
		start_line=EXCLUDED.start_line, body_hash=EXCLUDED.body_hash,
		embedding=EXCLUDED.embedding, updated_at=NOW()`) // table is schema-qualified above
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
	if opts.MaxDistance > 0 {
		where = append(where, fmt.Sprintf("embedding <=> $1 < $%d", len(args)+1))
		args = append(args, opts.MaxDistance)
	}
	q := `SELECT repo_key,file_path,symbol_name,symbol_kind,language,start_line,
		embedding <=> $1 AS distance FROM public.code_embeddings`
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
	_, err := s.pool.Exec(ctx, "DELETE FROM public.code_embeddings WHERE repo_key=$1", repoKey)
	return err
}

// SymbolIdentity holds the identity fields for a single indexed symbol.
// Used by incremental update paths (Pipeline.IndexFile) to compute diffs:
// symbols present in DB but absent from new parse → DELETE;
// symbols whose BodyHash changed → re-embed.
type SymbolIdentity struct {
	SymbolName string
	BodyHash   uint64 // matches EmbeddingRecord.BodyHash (BIGINT in DB)
	StartLine  int
}

// GetSymbolsForFile returns the symbol identity rows currently indexed for a
// specific file in a repo. Used by incremental update paths (Pipeline.IndexFile)
// to compute the diff: symbols present in DB but no longer in the file source
// must be DELETEd; symbols whose BodyHash changed must be re-embedded.
//
// Returns rows sorted by symbol_name for determinism.
func (s *Store) GetSymbolsForFile(ctx context.Context, repoKey, filePath string) ([]SymbolIdentity, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT symbol_name, body_hash, start_line
		 FROM public.code_embeddings
		 WHERE repo_key = $1 AND file_path = $2
		 ORDER BY symbol_name`,
		repoKey, filePath)
	if err != nil {
		return nil, fmt.Errorf("get symbols for file: %w", err)
	}
	defer rows.Close()

	var result []SymbolIdentity
	for rows.Next() {
		var si SymbolIdentity
		var bodyHash int64
		if err := rows.Scan(&si.SymbolName, &bodyHash, &si.StartLine); err != nil {
			return nil, fmt.Errorf("scan symbol identity: %w", err)
		}
		si.BodyHash = uint64(bodyHash)
		result = append(result, si)
	}
	return result, rows.Err()
}

// DeleteSymbolsForFile removes index rows for symbols in (repoKey, filePath)
// EXCEPT those whose names appear in keepSymbolNames. Used to reconcile a file
// after re-parsing: symbols missing from the new parse (deleted / renamed)
// must be evicted. Returns rows-affected count.
//
// If keepSymbolNames is empty, all symbols for the file are deleted (use this
// for IndexFile when the file is removed from the source tree).
func (s *Store) DeleteSymbolsForFile(ctx context.Context, repoKey, filePath string, keepSymbolNames []string) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}

	if len(keepSymbolNames) == 0 {
		ct, err := s.pool.Exec(ctx,
			`DELETE FROM public.code_embeddings WHERE repo_key = $1 AND file_path = $2`,
			repoKey, filePath)
		if err != nil {
			return 0, fmt.Errorf("delete symbols for file: %w", err)
		}
		return ct.RowsAffected(), nil
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM public.code_embeddings
		 WHERE repo_key = $1 AND file_path = $2
		   AND symbol_name != ALL($3::text[])`,
		repoKey, filePath, keepSymbolNames)
	if err != nil {
		return 0, fmt.Errorf("delete symbols for file: %w", err)
	}
	return ct.RowsAffected(), nil
}

// Stats returns embedding counts per repo.
func (s *Store) Stats(ctx context.Context) (map[string]int, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT repo_key,COUNT(*) FROM public.code_embeddings GROUP BY repo_key")
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
