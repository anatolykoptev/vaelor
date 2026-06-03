package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_repo_state_advanced_with_zero_embeddings_total is a defense-in-depth
// counter that bumps whenever SetRepoState would advance head_sha while the
// repo has 0 embedding rows. After Bug #1 is fixed this counter must stay 0.
// A non-zero value means the recovery path was bypassed — operator investigation
// required.
//
// Cardinality: 1 label (repo) — bounded by indexed repo count (~100).
var repoStateAdvancedWithZeroEmbeddingsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_repo_state_advanced_with_zero_embeddings_total",
		Help: "Times head_sha was about to advance while code_embeddings was empty; post-fix must be 0.",
	},
	[]string{"repo"},
)

// gocode_repo_embeddings_present is a gauge (0/1) per repo indicating whether
// the embedding store has ≥1 row. The existing gocode_index_commits_behind
// reads 0=healthy for EMPTY repos (commits_behind==0 AND empty is the frozen
// state). This gauge is the direct desync detector: 0 + commits_behind==0 is
// the alert condition.
//
// Set by SetEmbeddingsPresentGauge; called after each IncrementalSync and IndexRepo.
// Pre-touched in init() so fresh-deploy dashboards show 0 instead of "no data".
//
// Cardinality: 1 label (repo) — bounded by indexed repo count (~100).
var repoEmbeddingsPresent = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_repo_embeddings_present",
		Help: "1 if repo has ≥1 embedding row, 0 if empty. " +
			"commits_behind==0 AND embeddings_present==0 is the frozen-empty alert condition.",
	},
	[]string{"repo"},
)

// gocode_index_cancelled_total counts index operations that were aborted because
// the context was cancelled. Labelled by tool (the MCP tool that triggered the
// index) and phase (embed | db_write | chunk_loop). A non-zero rate indicates
// clients are disconnecting mid-index — expected behavior, but useful for
// confirming Bug #2 fix is holding.
var indexCancelledTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_index_cancelled_total",
		Help: "Index operations aborted due to context cancellation, by tool and phase.",
	},
	[]string{"tool", "phase"},
)

// SetEmbeddingsPresentGauge sets gocode_repo_embeddings_present{repo} to 1 when
// count > 0, and to 0 otherwise. Called after each successful IndexRepo or
// IncrementalSync pass to keep the gauge current.
func SetEmbeddingsPresentGauge(repo string, count int) {
	if count > 0 {
		repoEmbeddingsPresent.WithLabelValues(repo).Set(1)
	} else {
		repoEmbeddingsPresent.WithLabelValues(repo).Set(0)
	}
}

// RecordIndexCancelled increments the gocode_index_cancelled_total counter
// for the given tool and phase. Call at any point where ctx.Err() != nil
// aborts an index write path.
func RecordIndexCancelled(tool, phase string) {
	indexCancelledTotal.WithLabelValues(tool, phase).Inc()
}

func init() {
	// Pre-touch all counter/gauge label combinations so Prometheus exposes the
	// series on startup (no "no data" on fresh-deploy dashboards).
	// repoStateAdvancedWithZeroEmbeddingsTotal: no pre-touch — cardinality
	//   driven by repos encountered at runtime; pre-touching requires knowing
	//   the full repo list which is unavailable at init().
	//
	// repoEmbeddingsPresent: same rationale — repo labels are runtime-dynamic.
	//
	// indexCancelledTotal: pre-touch known tool/phase combinations.
	for _, tool := range []string{"semantic_search", "code_graph", "understand", "repo_analyze"} {
		for _, phase := range []string{"embed", "db_write", "chunk_loop"} {
			indexCancelledTotal.WithLabelValues(tool, phase)
		}
	}
}
