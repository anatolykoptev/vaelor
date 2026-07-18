// Package learnings persists review findings so future reviews on the same
// repo/symbol can reference prior outcomes.
package learnings

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/vaelor/internal/pgutil"
)

//go:embed schema.sql
var schemaSQL string

// Embedder abstracts the embedding client; pass nil to disable vector updates
// and fall back to exact (repo, symbol) lookups.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Record is a single learning.
//
// RiskLevel and ReviewOutcome are orthogonal:
//   - RiskLevel is written by the impact-analysis path (review_pr) and uses
//     the vocabulary low|medium|high.
//   - ReviewOutcome is written by the posted-review path (review_pr with dry_run=false) and
//     uses the vocabulary good|neutral|bad (mapped from the GitHub review
//     event).
//
// Either (or both) may be empty on a given row depending on which writer
// produced it.
type Record struct {
	Repo          string `json:"repo"`
	Symbol        string `json:"symbol"`
	RiskLevel     string `json:"risk_level,omitempty"`
	ReviewOutcome string `json:"review_outcome,omitempty"`
	Flag          string `json:"flag,omitempty"`
	Note          string `json:"note,omitempty"`
	PRURL         string `json:"pr_url,omitempty"`
}

// Store persists learnings in Postgres (pgvector).
type Store struct {
	pool *pgxpool.Pool
	emb  Embedder
}

// ownershipOnce gates the ownership-transfer ALTER to at most once per DSN
// per process. Unlike embeddings/designmd (long-lived Store singletons with
// a per-instance sync.Once), New() is also called per-request from
// reviewPRDryRun with a fresh pool each time — ALTER TABLE ... OWNER TO
// takes an ACCESS EXCLUSIVE lock even as a no-op, so without this gate every
// dry-run review would re-acquire that lock on the hot path.
var ownershipOnce sync.Map // dsn string -> *sync.Once

// New opens a pool. Caller must call Close.
//
// After migrating the schema it attempts (at most once per DSN per process)
// to transfer table ownership to CURRENT_USER (best-effort: warns instead of
// failing if the connected role is not the current table owner — e.g. after
// a superuser pg_restore left review_learnings owned by the restoring role).
func New(ctx context.Context, dsn string, emb Embedder) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate review_learnings schema: %w", err)
	}
	// Best-effort ownership transfer so the connected role can INSERT/TRUNCATE
	// review_learnings without needing explicit grants from an admin. Gated
	// to once per DSN per process — see ownershipOnce doc.
	onceForDSN, _ := ownershipOnce.LoadOrStore(dsn, &sync.Once{})
	onceForDSN.(*sync.Once).Do(func() {
		pgutil.TransferOwnership(ctx, pool, "learnings", "public.review_learnings")
	})
	return &Store{pool: pool, emb: emb}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// HasEmbedder reports whether this store has a configured Embedder for
// vector similarity search. When false, NearestVector calls will error.
func (s *Store) HasEmbedder() bool { return s.emb != nil }

// NearestByRepo returns up to k prior learnings for the given repo,
// ordered by creation time descending. It matches any symbol.
func (s *Store) NearestByRepo(ctx context.Context, repo string, k int) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT repo, symbol, risk_level, review_outcome, flag, note, pr_url
		FROM review_learnings
		WHERE repo = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, repo, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var risk, outcome *string
		if err := rows.Scan(&r.Repo, &r.Symbol, &risk, &outcome, &r.Flag, &r.Note, &r.PRURL); err != nil {
			return nil, err
		}
		if risk != nil {
			r.RiskLevel = *risk
		}
		if outcome != nil {
			r.ReviewOutcome = *outcome
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

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
		INSERT INTO review_learnings (repo, symbol, risk_level, review_outcome, flag, note, pr_url, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, r.Repo, r.Symbol, r.RiskLevel, r.ReviewOutcome, r.Flag, r.Note, r.PRURL, vectorArg(emb))
	return err
}

// Nearest returns up to k prior learnings for the same (repo, symbol).
// (Vector search is added later; exact match is enough for v1.)
func (s *Store) Nearest(ctx context.Context, repo, symbol string, k int) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT repo, symbol, risk_level, review_outcome, flag, note, pr_url
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
		var risk, outcome *string
		if err := rows.Scan(&r.Repo, &r.Symbol, &risk, &outcome, &r.Flag, &r.Note, &r.PRURL); err != nil {
			return nil, err
		}
		if risk != nil {
			r.RiskLevel = *risk
		}
		if outcome != nil {
			r.ReviewOutcome = *outcome
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// NearestVector returns up to k prior learnings closest to the given query
// string by cosine distance over the embedding column. It requires a configured
// Embedder; callers should fall back to the exact (repo, symbol) Nearest path
// when no embedder is available.
func (s *Store) NearestVector(ctx context.Context, query string, k int) ([]Record, error) {
	if s.emb == nil {
		return nil, errors.New("embedder not configured")
	}
	emb, err := s.emb.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	arg := vectorArg(emb)
	if arg == nil {
		return nil, errors.New("empty query embedding")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT repo, symbol, risk_level, review_outcome, flag, note, pr_url
		FROM review_learnings
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $2
	`, arg, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var risk, outcome *string
		if err := rows.Scan(&r.Repo, &r.Symbol, &risk, &outcome, &r.Flag, &r.Note, &r.PRURL); err != nil {
			return nil, err
		}
		if risk != nil {
			r.RiskLevel = *risk
		}
		if outcome != nil {
			r.ReviewOutcome = *outcome
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
