package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	defaultSimilarityThreshold float32 = 0.92
	defaultSimilarLimit                = 50
	maxSimilarLimit                    = 200

	// similarPairsStatementTimeout is applied as SET LOCAL statement_timeout
	// inside the FindSimilarPairs transaction. The O(n²) pgvector self-join can
	// run for 18+ minutes on repos with 24k embeddings (287M candidate pairs),
	// pinning a CPU core on the 4-core ARM box. 15s allows reasonable repos
	// (≤5k functions from the semhealth guard) to complete while bounding the
	// worst case. SQLSTATE 57014 (query_canceled) is treated as "no pairs".
	similarPairsStatementTimeout = "15s"

	// pgErrQueryCanceled is the PostgreSQL SQLSTATE for statement_timeout / query cancel.
	pgErrQueryCanceled = "57014"
)

// SimilarPair represents two semantically similar symbols within the same repo.
type SimilarPair struct {
	SymbolA    string
	FileA      string
	LineA      int
	SymbolB    string
	FileB      string
	LineB      int
	Similarity float32
}

// SimilarPairOpts controls the similarity search parameters.
type SimilarPairOpts struct {
	RepoKey   string
	Threshold float32 // minimum cosine similarity (default 0.92)
	Limit     int     // max pairs returned (default 50)
}

func (o SimilarPairOpts) effectiveThreshold() float32 {
	if o.Threshold > 0 {
		return o.Threshold
	}
	return defaultSimilarityThreshold
}

func (o SimilarPairOpts) effectiveLimit() int {
	if o.Limit > 0 && o.Limit <= maxSimilarLimit {
		return o.Limit
	}
	if o.Limit > maxSimilarLimit {
		return maxSimilarLimit
	}
	return defaultSimilarLimit
}

// FindSimilarPairs finds semantically similar function pairs within a repo
// using pgvector cosine distance self-join. Results are ordered by similarity descending.
//
// The self-join is wrapped in a short transaction with SET LOCAL statement_timeout
// to prevent it from pinning a CPU core for minutes on large repos. SQLSTATE 57014
// (query_canceled) is treated as "no pairs found" and logged at Debug level.
func (s *Store) FindSimilarPairs(ctx context.Context, opts SimilarPairOpts) ([]SimilarPair, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	threshold := opts.effectiveThreshold()
	limit := opts.effectiveLimit()

	// Cosine distance in pgvector: 0 = identical, 2 = opposite.
	// Similarity = 1 - distance. Threshold 0.92 → max distance 0.08.
	maxDist := 1.0 - float64(threshold)

	q := `SELECT a.symbol_name, a.file_path, a.start_line,
	             b.symbol_name, b.file_path, b.start_line,
	             1 - (a.embedding <=> b.embedding) AS similarity
	      FROM public.code_embeddings a, public.code_embeddings b
	      WHERE a.repo_key = $1 AND b.repo_key = $1
	        AND (a.file_path || ':' || a.symbol_name) < (b.file_path || ':' || b.symbol_name)
	        AND (a.embedding <=> b.embedding) < $2
	      ORDER BY similarity DESC
	      LIMIT $3`

	// Bound the O(n²) self-join with a per-statement timeout.
	// SET LOCAL applies only for the duration of this transaction.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("find similar pairs: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback on all paths; commit not needed for read-only

	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '`+similarPairsStatementTimeout+`'`); err != nil {
		return nil, fmt.Errorf("find similar pairs: set statement_timeout: %w", err)
	}

	rows, err := tx.Query(ctx, q, opts.RepoKey, maxDist, limit)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrQueryCanceled {
			// statement_timeout fired: treat as "no pairs" rather than an error.
			slog.Debug("semhealth: similarity self-join exceeded statement_timeout",
				slog.String("repo", opts.RepoKey),
				slog.String("timeout", similarPairsStatementTimeout))
			return nil, nil
		}
		return nil, fmt.Errorf("find similar pairs: %w", err)
	}
	defer rows.Close()

	var pairs []SimilarPair
	for rows.Next() {
		var p SimilarPair
		if err := rows.Scan(&p.SymbolA, &p.FileA, &p.LineA,
			&p.SymbolB, &p.FileB, &p.LineB, &p.Similarity); err != nil {
			return nil, fmt.Errorf("scan pair: %w", err)
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err()
}
