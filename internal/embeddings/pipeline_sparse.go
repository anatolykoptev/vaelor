package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/anatolykoptev/go-kit/sparse"
)

// sparseServerMaxDocs is the per-request input cap enforced by embed-server
// (EMBED_MAX_INPUT_ARRAY=32). The dense path already chunks by indexChunkSize=100;
// the sparse call inside one 100-chunk must sub-batch by this value so no single
// /embed_sparse request exceeds the server limit.
const sparseServerMaxDocs = 32

// sparseMaxTerms is the maximum number of non-zero terms kept per sparse vector
// after sanitization. pgvector HNSW on sparsevec_ip_ops has a hard 1000-nonzero
// cap per indexed vector — exceeding it silently degrades the row to brute-force
// scan once the P3 HNSW index is added. SPLADE doc-side expansion on long code
// files routinely exceeds 1000 terms. We cap at 256 (well under the 1000 ceiling,
// ~2 KB/row) which matches production SPLADE top-k pruning practice and keeps
// memory and index size bounded. Only the highest-weight 256 terms are retained;
// the rest contribute negligible ranking signal anyway.
const sparseMaxTerms = 256

// gocode_sparse_embed_failures_total counts all sparse-embedding failures by
// stage ("index" when batching document texts, "write" when persisting to DB).
// Pre-touched at 0 so the counter is always present on /metrics regardless of
// whether any failure has occurred. Matches the project metrics-first rule:
// every new failure class ships with its own counter.
var sparseEmbedFailTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_sparse_embed_failures_total",
		Help: "Total SPLADE sparse-embed failures by stage (index, write).",
	},
	[]string{"stage"},
)

func init() {
	// Pre-touch both label values so /metrics always shows the counter even
	// before the first failure (metrics-first: no silent absences).
	sparseEmbedFailTotal.WithLabelValues("index").Add(0)
	sparseEmbedFailTotal.WithLabelValues("write").Add(0)
}

// embedSparseBatched calls the sparse embedder in sub-batches of maxBatch texts
// to respect the server's EMBED_MAX_INPUT_ARRAY cap (sparseServerMaxDocs=32 by
// default; overridden via SPARSE_EMBED_MAX_ARRAY / Pipeline.sparseMaxBatch).
//
// On error for any sub-batch the failure counter is bumped and the error is
// returned — the caller (embedAndUpsert) treats sparse failure as non-fatal:
// it logs, leaves SparseEmbedding zero-valued (→ NULL on upsert), and
// continues with the dense path. This preserves the cold-path guarantee that a
// degraded sparse service degrades ranking only, never breaks search.
func embedSparseBatched(ctx context.Context, e sparse.SparseEmbedder, texts []string, maxBatch int) ([]sparse.SparseVector, error) {
	if maxBatch <= 0 {
		maxBatch = sparseServerMaxDocs
	}
	out := make([]sparse.SparseVector, 0, len(texts))
	for i := 0; i < len(texts); i += maxBatch {
		j := i + maxBatch
		if j > len(texts) {
			j = len(texts)
		}
		vecs, err := e.EmbedSparse(ctx, texts[i:j])
		if err != nil {
			sparseEmbedFailTotal.WithLabelValues("index").Inc()
			return nil, fmt.Errorf("embed sparse batch [%d:%d]: %w", i, j, err)
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// SanitizeAndFormatSparseVector sanitizes v and formats it as a pgvector sparsevec
// text literal: `{i1:w1,i2:w2,…}/dim`.
//
// Sanitization (applied before formatting, never mutates the caller's slice):
//   - drops entries with zero weight (pgvector rejects them)
//   - deduplicates indices (keeps the last occurrence, matching SPLADE convention)
//   - drops entries with index ≥ dim (out-of-range for the column's sparsevec(dim))
//   - prunes to the top sparseMaxTerms (256) highest-weight entries (pgvector HNSW
//     on sparsevec_ip_ops hard-caps at 1000 nonzero terms; SPLADE doc-side can exceed)
//
// After sanitization an empty result is formatted as "" (not `{}/dim`) so callers
// can detect a fully-degenerate vector and bind NULL instead. Non-empty results
// are always index-ascending (required by pgvector).
func SanitizeAndFormatSparseVector(v sparse.SparseVector, dim int) string {
	type pair struct {
		idx uint32
		val float32
	}
	// Deduplicate: keep last value for each index (SPLADE convention).
	seen := make(map[uint32]float32, v.Len())
	for i := range v.Len() {
		idx := v.Indices[i]
		val := v.Values[i]
		if val == 0 || idx >= uint32(dim) { //nolint:gosec // G115: dim is always 30522, no overflow risk
			continue // drop zero-weight and out-of-range entries
		}
		seen[idx] = val // last-wins dedup
	}
	if len(seen) == 0 {
		return "" // fully degenerate — caller binds NULL
	}
	pairs := make([]pair, 0, len(seen))
	for idx, val := range seen {
		pairs = append(pairs, pair{idx, val})
	}
	// Top-K prune: pgvector HNSW on sparsevec_ip_ops hard-caps at 1000 nonzero
	// terms per row; exceeding it degrades the row to brute-force scan once the
	// P3 index lands. Cap at sparseMaxTerms (256) by discarding the lowest-weight
	// tail — those terms contribute negligible IP ranking signal anyway.
	if len(pairs) > sparseMaxTerms {
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].val > pairs[j].val }) // weight desc
		pairs = pairs[:sparseMaxTerms]
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })

	const sparseVecHeaderBytes = 16
	b := make([]byte, 0, len(pairs)*12+sparseVecHeaderBytes)
	b = append(b, '{')
	for i, p := range pairs {
		if i > 0 {
			b = append(b, ',')
		}
		b = fmt.Appendf(b, "%d:%g", p.idx, p.val)
	}
	b = append(b, '}')
	b = fmt.Appendf(b, "/%d", dim)
	return string(b)
}

// newSparseEmbedder validates that the embedder's vocab size matches the
// sparsevec column dimension (30522). Returns nil with a WARN log on mismatch,
// keeping sparse disabled rather than corrupting writes with out-of-range indices.
func newSparseEmbedder(e sparse.SparseEmbedder) sparse.SparseEmbedder {
	if vs := e.VocabSize(); vs != sparseDim {
		slog.Warn("sparse embedder VocabSize mismatch: sparse indexing disabled",
			slog.Int("embedder_vocab_size", vs),
			slog.Int("column_dim", sparseDim))
		return nil
	}
	return e
}
