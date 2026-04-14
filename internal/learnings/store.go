// Package learnings persists review findings so future reviews on the same
// repo/symbol can reference prior verdicts.
package learnings

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Embedder abstracts the embedding client; pass nil to disable vector updates
// and fall back to exact (repo, symbol) lookups.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Record is a single learning.
type Record struct {
	Repo, Symbol, Verdict, Flag, Note, PRURL string
}

// Store persists learnings in Postgres (pgvector).
type Store struct {
	pool *pgxpool.Pool
	emb  Embedder
}

// New opens a pool. Caller must call Close.
func New(ctx context.Context, dsn string, emb Embedder) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	return &Store{pool: pool, emb: emb}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Upsert inserts a learning. If an embedder is configured, the flag+note is
// embedded for future similarity search.
func (s *Store) Upsert(ctx context.Context, r Record) error {
	var emb []float32
	if s.emb != nil {
		v, err := s.emb.Embed(ctx, r.Flag+": "+r.Note)
		if err == nil {
			emb = v
		}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO review_learnings (repo, symbol, verdict, flag, note, pr_url, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, r.Repo, r.Symbol, r.Verdict, r.Flag, r.Note, r.PRURL, vectorArg(emb))
	return err
}

// Nearest returns up to k prior learnings for the same (repo, symbol).
// (Vector search is added later; exact match is enough for v1.)
func (s *Store) Nearest(ctx context.Context, repo, symbol string, k int) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT repo, symbol, verdict, flag, note, pr_url
		FROM review_learnings
		WHERE repo = $1 AND symbol = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, repo, symbol, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Repo, &r.Symbol, &r.Verdict, &r.Flag, &r.Note, &r.PRURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// vectorArg formats a Go slice into pgvector literal or NULL.
func vectorArg(v []float32) any {
	if len(v) == 0 {
		return nil
	}
	s := "["
	for i, x := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%f", x)
	}
	s += "]"
	return s
}
