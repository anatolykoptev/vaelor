package embeddings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/anatolykoptev/go-kit/sparse"
)

// gocode_sparse_query_failures_total counts sparse-retrieval failures by stage:
//   - "embed" — EmbedSparseQuery failed (query vector not produced)
//   - "query" — DB SELECT failed (sparse_embedding <#> literal query error)
//
// Pre-touched at 0 so /metrics always exposes both label values regardless of
// whether any failure has occurred (project metrics-first rule: no silent absences).
var sparseQueryFailTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_sparse_query_failures_total",
		Help: "Total SPLADE sparse-query failures by stage (embed, query).",
	},
	[]string{"stage"},
)

func init() {
	sparseQueryFailTotal.WithLabelValues("embed").Add(0)
	sparseQueryFailTotal.WithLabelValues("query").Add(0)
}

// SearchSparse retrieves the top-K symbols most relevant to the query via the
// SPLADE sparse_embedding column using negative inner product (<#> operator).
//
// Protocol:
//  1. Embed the query text to a SparseVector via sparseClient.EmbedSparseQuery.
//  2. Sanitize and prune the query vector (SanitizeAndFormatSparseVector, same
//     helper used at index time) to guard against fat query vectors that would
//     violate pgvector's HNSW 1000-nonzero cap.
//  3. Issue: SELECT … sparse_embedding <#> $1::sparsevec AS neg_ip … ORDER BY neg_ip LIMIT $3
//     — <#> returns negative inner product; ORDER BY ASC puts the most-similar rows first.
//  4. Filter WHERE sparse_embedding IS NOT NULL to skip rows not yet backfilled (P5).
//
// Gating: returns (nil, nil) when sparseClient is nil (SPARSE_EMBED_URL not set)
// or when the query text produces an empty sparse vector (all-stopword query).
// In both cases zero DB I/O is issued.
//
// Failures: embed-fail bumps gocode_sparse_query_failures_total{stage="embed"} and
// returns empty; DB-fail bumps {stage="query"} and returns empty. P4 treats an
// empty sparse arm as dense+keyword only — byte-identical to today.
func (s *Store) SearchSparse(
	ctx context.Context,
	queryText string,
	sparseClient sparse.SparseEmbedder,
	opts SearchOpts,
) ([]SparseHit, error) {
	if sparseClient == nil {
		return nil, nil
	}

	// Step 1: embed query.
	qvec, err := sparseClient.EmbedSparseQuery(ctx, queryText)
	if err != nil {
		sparseQueryFailTotal.WithLabelValues("embed").Inc()
		slog.Warn("sparse search: embed query failed", slog.Any("error", err))
		return nil, nil //nolint:nilerr // non-fatal: P4 falls back to dense+keyword
	}

	// Step 2: sanitize + prune — same invariants as index time.
	// Empty vector (pure-stopword query) → skip DB hit.
	lit := SanitizeAndFormatSparseVector(qvec, sparseDim)
	if lit == "" {
		return nil, nil
	}

	// Step 3: build and execute the sparse retrieval query.
	if err := s.EnsureSchema(ctx); err != nil {
		sparseQueryFailTotal.WithLabelValues("query").Inc()
		return nil, nil //nolint:nilerr // schema fail: non-fatal for sparse arm
	}

	topK := max(min(opts.TopK, maxTopK), 1)
	if opts.TopK <= 0 {
		topK = defaultTopK
	}

	// WHERE clause mirrors Search: repo_key and language filters.
	// sparse_embedding IS NOT NULL skips rows not yet backfilled (Phase 5).
	where := "sparse_embedding IS NOT NULL"
	args := []any{lit, topK}
	if opts.RepoKey != "" {
		where += fmt.Sprintf(" AND repo_key=$%d", len(args)+1)
		args = append(args, opts.RepoKey)
	}
	if opts.Language != "" {
		where += fmt.Sprintf(" AND language=$%d", len(args)+1)
		args = append(args, opts.Language)
	}

	q := `SELECT repo_key,file_path,symbol_name,symbol_kind,language,start_line,
		sparse_embedding <#> $1::sparsevec AS neg_ip
		FROM public.code_embeddings
		WHERE ` + where + `
		ORDER BY neg_ip LIMIT $2`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		sparseQueryFailTotal.WithLabelValues("query").Inc()
		slog.Warn("sparse search: query failed", slog.Any("error", err))
		return nil, nil //nolint:nilerr // non-fatal: P4 falls back to dense+keyword
	}
	defer rows.Close()

	var results []SparseHit
	for rows.Next() {
		var (
			repoKey, filePath, symbolName, symbolKind, language string
			startLine                                           int
			negIP                                               float64
		)
		if err := rows.Scan(&repoKey, &filePath, &symbolName, &symbolKind, &language, &startLine, &negIP); err != nil {
			sparseQueryFailTotal.WithLabelValues("query").Inc()
			slog.Warn("sparse search: scan failed", slog.Any("error", err))
			return nil, nil //nolint:nilerr // non-fatal: partial result discarded
		}
		results = append(results, SparseHit{
			FilePath:   filePath,
			SymbolName: symbolName,
			Line:       startLine,
		})
	}
	if err := rows.Err(); err != nil {
		sparseQueryFailTotal.WithLabelValues("query").Inc()
		slog.Warn("sparse search: rows error", slog.Any("error", err))
		return nil, nil //nolint:nilerr // non-fatal
	}
	return results, nil
}
