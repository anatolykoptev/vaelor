package embeddings

import (
	"context"
	"fmt"
)

const (
	defaultSimilarityThreshold float32 = 0.92
	defaultSimilarLimit                = 50
	maxSimilarLimit                    = 200
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
	      FROM code_embeddings a, code_embeddings b
	      WHERE a.repo_key = $1 AND b.repo_key = $1
	        AND (a.file_path || ':' || a.symbol_name) < (b.file_path || ':' || b.symbol_name)
	        AND (a.embedding <=> b.embedding) < $2
	      ORDER BY similarity DESC
	      LIMIT $3`

	rows, err := s.pool.Query(ctx, q, opts.RepoKey, maxDist, limit)
	if err != nil {
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
