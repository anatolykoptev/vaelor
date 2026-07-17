package designmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"golang.org/x/sync/singleflight"
)

const (
	dimSize  = 1024
	batchSz  = 50
	defaultK = 20
	maxK     = 100
)

const schemaSQL = `CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS design_embeddings (
    brand TEXT NOT NULL, section TEXT NOT NULL,
    file_path TEXT NOT NULL, start_line INT NOT NULL DEFAULT 0,
    body_hash BIGINT NOT NULL DEFAULT 0,
    embedding vector(1024) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (brand, section));
CREATE INDEX IF NOT EXISTS idx_design_emb_hnsw ON design_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)`

// Record holds one design section embedding.
type Record struct {
	Brand     string
	Section   string
	FilePath  string
	StartLine int
	BodyHash  uint64
	Embedding []float32
}

// SearchResult is a single search hit.
type SearchResult struct {
	Brand    string
	Section  string
	FilePath string
	Distance float32
}

// Store manages design embeddings in PostgreSQL with pgvector (1024-dim).
type Store struct {
	pool        *pgxpool.Pool
	schema      schemaQuerier
	schemaGroup singleflight.Group
	schemaDone  atomic.Bool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, schema: pool} }

// EnsureSchema creates the pgvector extension and design_embeddings table if
// needed. After creating the table it attempts to transfer ownership to
// CURRENT_USER (best-effort: warns instead of failing if the role is not the
// table owner — e.g. after a superuser pg_restore left design_embeddings
// owned by the restoring role).
//
// It uses a success-only latch: a transient failure is retried on the next call,
// and concurrent callers are deduplicated by singleflight. On a warm database the
// fast-path catalog checks emit zero CREATE/ALTER DDL.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s.schemaDone.Load() {
		setSchemaReady(1)
		return nil
	}
	_, err, _ := s.schemaGroup.Do("schema", func() (any, error) {
		if s.schemaDone.Load() {
			return nil, nil
		}
		start := time.Now()
		err := s.runEnsureSchema(ctx)
		if err != nil {
			slog.Error("designmd: schema init failed", slog.Any("error", err))
			setSchemaReady(0)
			recordSchemaInit(schemaOutcome(err), time.Since(start))
			return nil, err
		}
		s.schemaDone.Store(true)
		setSchemaReady(1)
		recordSchemaInit("ok", time.Since(start))
		return nil, nil
	})
	return err
}

// Upsert stores design section embeddings.
func (s *Store) Upsert(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	for i := 0; i < len(records); i += batchSz {
		if err := s.upsertBatch(ctx, records[i:min(i+batchSz, len(records))]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) upsertBatch(ctx context.Context, records []Record) error {
	var b strings.Builder
	b.WriteString(`INSERT INTO design_embeddings (brand,section,file_path,start_line,body_hash,embedding,updated_at) VALUES `)
	args := make([]any, 0, len(records)*6)
	for i, r := range records {
		if i > 0 {
			b.WriteByte(',')
		}
		off := i * 6
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,NOW())", off+1, off+2, off+3, off+4, off+5, off+6)
		args = append(args, r.Brand, r.Section, r.FilePath, r.StartLine, int64(r.BodyHash), pgvector.NewVector(r.Embedding))
	}
	b.WriteString(` ON CONFLICT (brand, section) DO UPDATE SET
		file_path=EXCLUDED.file_path, start_line=EXCLUDED.start_line,
		body_hash=EXCLUDED.body_hash, embedding=EXCLUDED.embedding, updated_at=NOW()`)
	_, err := s.pool.Exec(ctx, b.String(), args...)
	return err
}

// Search finds top-K most similar design sections.
func (s *Store) Search(ctx context.Context, query []float32, topK int) ([]SearchResult, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = defaultK
	}
	if topK > maxK {
		topK = maxK
	}
	rows, err := s.pool.Query(ctx,
		`SELECT brand, section, file_path, embedding <=> $1 AS distance
		 FROM design_embeddings ORDER BY distance LIMIT $2`,
		pgvector.NewVector(query), topK)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Brand, &r.Section, &r.FilePath, &r.Distance); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetHashes returns brand:section → body_hash for change detection.
func (s *Store) GetHashes(ctx context.Context) (map[string]uint64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT brand, section, body_hash FROM design_embeddings")
	if err != nil {
		return nil, fmt.Errorf("query hashes: %w", err)
	}
	defer rows.Close()
	result := make(map[string]uint64)
	for rows.Next() {
		var brand, section string
		var hash int64
		if err := rows.Scan(&brand, &section, &hash); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result[brand+":"+section] = uint64(hash)
	}
	return result, rows.Err()
}
