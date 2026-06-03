package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
	pgvector "github.com/pgvector/pgvector-go"
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

	// defaultNearDupK is the number of nearest neighbours requested per symbol
	// in FindNearDuplicates. k+1 is passed to Store.Search because the symbol
	// itself (distance 0) is always in the result and is dropped.
	defaultNearDupK = 5

	// DefaultNearDupK is the exported form of defaultNearDupK for callers in
	// other packages (e.g. semhealth) that need to pass k to FindNearDuplicates.
	DefaultNearDupK = defaultNearDupK
)

// nearDupSymbol is an internal record for a symbol loaded by FindNearDuplicates.
// It carries the embedding so we can call Store.Search with a constant query vector.
type nearDupSymbol struct {
	filePath   string
	symbolName string
	symbolKind string
	startLine  int
	embedding  []float32
}

// NearDupResult is returned by FindNearDuplicates and carries both the
// deduplicated pair slice and a count of per-symbol Search errors so the caller
// can distinguish a complete run from a partial one.
type NearDupResult struct {
	// Pairs is the deduplicated set of near-duplicate candidates.
	Pairs []SimilarPair
	// SearchErrors is the number of per-symbol Store.Search calls that failed.
	// Non-zero means the result is INCOMPLETE — some symbols were skipped.
	// A SQLSTATE 57014 (statement_timeout) on an individual Search increments
	// this counter and is reflected here.
	SearchErrors int
}

// SimilarPair represents two semantically similar symbols within the same repo.
type SimilarPair struct {
	SymbolA    string
	FileA      string
	LineA      int
	KindA      string // symbol_kind of A (e.g. "function", "method", "class")
	SymbolB    string
	FileB      string
	LineB      int
	KindB      string // symbol_kind of B
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

	q := `SELECT a.symbol_name, a.file_path, a.start_line, a.symbol_kind,
	             b.symbol_name, b.file_path, b.start_line, b.symbol_kind,
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
		if err := rows.Scan(&p.SymbolA, &p.FileA, &p.LineA, &p.KindA,
			&p.SymbolB, &p.FileB, &p.LineB, &p.KindB, &p.Similarity); err != nil {
			return nil, fmt.Errorf("scan pair: %w", err)
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err()
}

// nearDupPairKey returns a canonical dedup key for a (fileA:symA, fileB:symB) pair.
// The lesser endpoint (lexicographic on "file:symbol") is always first, so both
// (A,B) and (B,A) produce the same key. This prevents double-counting: each
// symbol's k-NN query sees the pair from its own side; the map deduplicates.
func nearDupPairKey(fileA, symA, fileB, symB string) string {
	ea := fileA + ":" + symA
	eb := fileB + ":" + symB
	if ea <= eb {
		return ea + "|" + eb
	}
	return eb + "|" + ea
}

// nearDupCanonicalPair builds a SimilarPair with the canonical A-is-lesser ordering
// and Similarity = 1 - distance. Kind and Line fields are passed in directly.
func nearDupCanonicalPair(
	fileA, symA string, lineA int, kindA string,
	fileB, symB string, lineB int, kindB string,
	distance float32,
) SimilarPair {
	ea := fileA + ":" + symA
	eb := fileB + ":" + symB
	sim := float32(1) - distance
	if ea <= eb {
		return SimilarPair{
			FileA: fileA, SymbolA: symA, LineA: lineA, KindA: kindA,
			FileB: fileB, SymbolB: symB, LineB: lineB, KindB: kindB,
			Similarity: sim,
		}
	}
	return SimilarPair{
		FileA: fileB, SymbolA: symB, LineA: lineB, KindA: kindB,
		FileB: fileA, SymbolB: symA, LineB: lineA, KindB: kindA,
		Similarity: sim,
	}
}

// loadSymbolsWithEmbeddings loads the file_path, symbol_name, symbol_kind,
// start_line, and embedding for every symbol in repoKey from code_embeddings.
// The embedding is scanned as pgvector.Vector and converted to []float32 via .Slice().
func (s *Store) loadSymbolsWithEmbeddings(ctx context.Context, repoKey string) ([]nearDupSymbol, error) {
	const q = `SELECT file_path, symbol_name, symbol_kind, start_line, embedding
	            FROM public.code_embeddings
	            WHERE repo_key = $1`

	rows, err := s.pool.Query(ctx, q, repoKey)
	if err != nil {
		return nil, fmt.Errorf("load symbols for near-dup: %w", err)
	}
	defer rows.Close()

	var syms []nearDupSymbol
	for rows.Next() {
		var sym nearDupSymbol
		var vec pgvector.Vector
		if err := rows.Scan(&sym.filePath, &sym.symbolName, &sym.symbolKind, &sym.startLine, &vec); err != nil {
			return nil, fmt.Errorf("load symbols for near-dup: scan: %w", err)
		}
		sym.embedding = vec.Slice()
		syms = append(syms, sym)
	}
	return syms, rows.Err()
}

// FindNearDuplicates finds near-duplicate symbol pairs in repoKey using
// per-symbol HNSW k-NN queries — N × O(log N) instead of the O(N²) all-pairs
// self-join used by FindSimilarPairs.
//
// For each symbol it calls Store.Search with the symbol's own embedding as the
// constant query vector, requesting k+1 neighbours (k+1 because the symbol
// itself is always among the results at distance 0 and is dropped). Pairs whose
// cosine distance exceeds maxDist are discarded. The result is deduplicated
// using nearDupPairKey so each unordered pair appears exactly once regardless
// of which endpoint discovered it.
//
// Per-symbol resilience: if an individual Search fails (including SQLSTATE 57014
// statement_timeout), the error is logged, SearchErrors is incremented, and
// processing continues with the remaining symbols. The returned NearDupResult
// carries SearchErrors > 0 when any query failed, signalling an incomplete run
// to the caller. A non-nil error is reserved for a fatal failure of the bulk
// symbol load (the entire result set would be wrong).
//
// Semantic note: this returns each symbol's top-k nearest neighbours, not all
// pairs under maxDist. A symbol with more than k near-duplicates will surface
// only its k closest. k=defaultNearDupK (5) covers the common case; the
// operator can pass a larger k for exhaustive analysis.
func (s *Store) FindNearDuplicates(ctx context.Context, repoKey string, k int, maxDist float32) (NearDupResult, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return NearDupResult{}, err
	}

	syms, err := s.loadSymbolsWithEmbeddings(ctx, repoKey)
	if err != nil {
		return NearDupResult{}, fmt.Errorf("find near duplicates: %w", err)
	}

	seen := make(map[string]struct{}, len(syms))
	var pairs []SimilarPair
	searchErrors := 0

	for _, sym := range syms {
		results, searchErr := s.Search(ctx, sym.embedding, SearchOpts{
			RepoKey:     repoKey,
			TopK:        k + 1, // +1 because self (distance 0) is always included
			MaxDistance: maxDist,
		})
		if searchErr != nil {
			var pgErr *pgconn.PgError
			if errors.As(searchErr, &pgErr) && pgErr.Code == pgErrQueryCanceled {
				slog.Warn("semhealth near-dup: per-symbol search hit statement_timeout",
					slog.String("repo", repoKey),
					slog.String("symbol", sym.symbolName),
					slog.String("file", sym.filePath))
			} else {
				slog.Warn("semhealth near-dup: per-symbol search error",
					slog.String("repo", repoKey),
					slog.String("symbol", sym.symbolName),
					slog.Any("error", searchErr))
			}
			searchErrors++
			continue
		}

		for _, r := range results {
			// Drop self-match.
			if r.FilePath == sym.filePath && r.SymbolName == sym.symbolName {
				continue
			}

			key := nearDupPairKey(sym.filePath, sym.symbolName, r.FilePath, r.SymbolName)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}

			pairs = append(pairs, nearDupCanonicalPair(
				sym.filePath, sym.symbolName, sym.startLine, sym.symbolKind,
				r.FilePath, r.SymbolName, r.StartLine, r.SymbolKind,
				r.Distance,
			))
		}
	}

	return NearDupResult{Pairs: pairs, SearchErrors: searchErrors}, nil
}
