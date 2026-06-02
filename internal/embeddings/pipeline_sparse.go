package embeddings

import (
	"context"
	"fmt"
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

// FormatSparseVector formats a sparse.SparseVector as a pgvector sparsevec
// text literal: `{i1:w1,i2:w2,…}/dim`.
//
// pgvector requires indices to be sorted ascending. SPLADE output is
// weight-descending, so we sort a copy — we never mutate the caller's slice
// (per the sparse.SparseVector ownership comment in the go-kit/sparse package).
// An empty vector formats as `{}/dim`; callers should check v.IsEmpty() and
// bind nil (SQL NULL) rather than calling FormatSparseVector on empty inputs —
// this keeps un-backfilled rows as NULL for the Phase-5 IS NULL cursor.
func FormatSparseVector(v sparse.SparseVector, dim int) string {
	n := v.Len()
	if n == 0 {
		return fmt.Sprintf("{}/%d", dim)
	}
	type pair struct {
		idx uint32
		val float32
	}
	pairs := make([]pair, n)
	for i := range n {
		pairs[i] = pair{v.Indices[i], v.Values[i]}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })

	const sparseVecHeaderBytes = 16 // "{" + "/30522\0" with some slack for large dim literals
	b := make([]byte, 0, n*12+sparseVecHeaderBytes)
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
