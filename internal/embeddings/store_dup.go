package embeddings

import (
	"context"
	"fmt"
)

const (
	// maxExactDupPairs caps the result set from FindExactDuplicates. The
	// body_hash equality scan is O(index) not O(n²), so the cap is a result-size
	// bound only — not a performance guard. 200 matches maxSimilarLimit.
	maxExactDupPairs = 200
)

// SymbolRef identifies one symbol in the code_embeddings table.
type SymbolRef struct {
	FilePath   string
	SymbolName string
	SymbolKind string
	StartLine  int
}

// ExactDupPair is two distinct symbols sharing an identical body_hash.
// Exact (textual) clones are found via a fast indexed equality scan on the
// (repo_key, body_hash) partial index — no vector distance computation required.
type ExactDupPair struct {
	A, B SymbolRef
	// BodyHash is the scanned BIGINT column value. The ingest-side hash is uint64
	// (FNV-64a), cast to int64 on write; values with the high bit set appear as
	// negative here. The sign is irrelevant for equality joins — this is the only
	// use of the field.
	BodyHash int64
}

// FindExactDuplicates returns pairs of distinct symbols within repoKey that
// share an identical non-zero body_hash. A non-zero body_hash means the symbol
// body was hashed at index time; DEFAULT 0 means "no hash" and is excluded to
// avoid false collisions between unhashed symbols.
//
// The result is ordered deterministically by
// (a.body_hash, a.file_path, a.symbol_name, b.file_path, b.symbol_name)
// and capped at maxExactDupPairs. Because the scan uses an indexed equality
// join on the (repo_key, body_hash) partial index (WHERE body_hash <> 0) it
// does not need the O(n²) statement_timeout guard used by FindSimilarPairs.
func (s *Store) FindExactDuplicates(ctx context.Context, repoKey string) ([]ExactDupPair, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, fmt.Errorf("find exact duplicates: %w", err)
	}

	// Self-join on body_hash with the same unique-pair ordering trick used in
	// FindSimilarPairs: (a.file_path||':'||a.symbol_name) < (b…) ensures each
	// unordered pair appears exactly once. body_hash <> 0 excludes symbols that
	// were not hashed at ingest time (DEFAULT 0 means "no hash").
	const q = `
		SELECT a.file_path, a.symbol_name, a.symbol_kind, a.start_line,
		       b.file_path, b.symbol_name, b.symbol_kind, b.start_line,
		       a.body_hash
		FROM public.code_embeddings a
		JOIN public.code_embeddings b
		  ON a.repo_key = b.repo_key
		 AND a.body_hash = b.body_hash
		 AND a.body_hash <> 0
		 AND (a.file_path || ':' || a.symbol_name) < (b.file_path || ':' || b.symbol_name)
		WHERE a.repo_key = $1
		ORDER BY a.body_hash, a.file_path, a.symbol_name, b.file_path, b.symbol_name
		LIMIT $2`

	rows, err := s.pool.Query(ctx, q, repoKey, maxExactDupPairs)
	if err != nil {
		return nil, fmt.Errorf("find exact duplicates: %w", err)
	}
	defer rows.Close()

	var pairs []ExactDupPair
	for rows.Next() {
		var p ExactDupPair
		if err := rows.Scan(
			&p.A.FilePath, &p.A.SymbolName, &p.A.SymbolKind, &p.A.StartLine,
			&p.B.FilePath, &p.B.SymbolName, &p.B.SymbolKind, &p.B.StartLine,
			&p.BodyHash,
		); err != nil {
			return nil, fmt.Errorf("find exact duplicates: scan pair: %w", err)
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err()
}
