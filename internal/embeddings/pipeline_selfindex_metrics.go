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
//
// Pre-touched at boot for every known repo_key via
// WarmRepoStateAdvancedZeroEmbeddings (called from cmd/go-code register.go
// with embeddings.Store.ListRepoKeys) — see that function's doc comment for
// why increase() needs the series to already exist before the first event.
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

// gocode_index_partial_abort_total counts background index runs that committed ≥1
// chunk then aborted before writing all chunks AND without advancing repo_state SHA.
// This is the exact "0→100→0→100" churn signature: rows are in the DB but SHA is
// frozen, so every re-trigger re-deletes and re-writes chunk 1.
//
// A non-zero rate means embed-server latency is still exceeding EmbedHTTPTimeout
// on some batches. After Fix 1 (EMBED_HTTP_TIMEOUT=120s), this should drop to 0.
// Persistent non-zero values indicate the timeout needs to be raised further.
//
// Cardinality: 1 label (repo) — bounded by indexed repo count (~100).
var indexPartialAbortTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_index_partial_abort_total",
		Help: "Background index runs that committed ≥1 chunk then aborted before SHA advance. " +
			"Post-fix must approach 0; persistent non-zero means EmbedHTTPTimeout is still too short.",
	},
	[]string{"repo"},
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

// RecordIndexPartialAbort increments gocode_index_partial_abort_total{repo}
// when a background index run has committed ≥1 chunk (rowsWritten > 0) but is
// aborting before writing all chunks and before advancing the repo_state SHA.
// This is the "0→100→0→100" churn signature: partial rows survive but SHA stays frozen.
func RecordIndexPartialAbort(repo string) {
	indexPartialAbortTotal.WithLabelValues(repo).Inc()
}

func init() {
	// Pre-touch all counter/gauge label combinations so Prometheus exposes the
	// series on startup (no "no data" on fresh-deploy dashboards).
	//
	// repoStateAdvancedWithZeroEmbeddingsTotal: no pre-touch HERE — the full
	//   repo_key list lives in Postgres (code_repo_state) and is unavailable
	//   at package init() (no DB handle yet). Instead it is warmed once the
	//   store is available, at boot: cmd/go-code register.go calls
	//   embeddings.Store.ListRepoKeys then WarmRepoStateAdvancedZeroEmbeddings
	//   (2026-07-01 metrics audit; see that function's doc comment).
	//
	// repoEmbeddingsPresent, indexPartialAbortTotal: same DB-dependent
	//   rationale, deliberately left un-warmed for now — unlike the counter
	//   above, no Prometheus alert rule currently reads either metric
	//   (config/prometheus/alerts-go-code.yml), so a boot-time N-repo warm
	//   query buys no alert coverage today. Revisit if/when a rule is added
	//   (repo-review-council 2026-07-01 NOISE #37 proposes the "frozen-empty"
	//   compound alert this gauge would back).
	//
	// indexCancelledTotal: pre-touch known tool/phase combinations — fully
	//   bounded, no DB dependency, so it stays here in init().
	for _, tool := range []string{"semantic_search", "code_graph", "understand", "repo_analyze", "autoindex", "code_research"} {
		for _, phase := range []string{"embed", "db_write", "chunk_loop"} {
			indexCancelledTotal.WithLabelValues(tool, phase)
		}
	}
}

// WarmRepoStateAdvancedZeroEmbeddings pre-touches
// gocode_repo_state_advanced_with_zero_embeddings_total{repo} for every
// repoKey supplied, so the series exists before the first real event.
//
// Why this matters: Prometheus increase() treats a counter series's first
// sample as having nothing to subtract from — a label combination that comes
// into existence AT the moment of a bad event yields increase()==0 for that
// exact event. A repo desyncing for the first time after a process restart
// would therefore be invisible to GocodeRepoStateAdvancedZeroEmbeddings until
// its SECOND desync in the same process lifetime. Pre-touching at boot with
// the known repo_key list (code_repo_state — repos indexed at least once)
// closes that window for every repo already on record.
//
// Repos indexed for the first time after boot are not covered by this
// pre-touch, but are lower risk: SetRepoState establishes their
// code_repo_state row (and, transitively, their eligibility for this call on
// the NEXT boot) as part of the same successful-index path that would need to
// desync to trigger the counter at all.
func WarmRepoStateAdvancedZeroEmbeddings(repoKeys []string) {
	for _, repo := range repoKeys {
		repoStateAdvancedWithZeroEmbeddingsTotal.WithLabelValues(repo).Add(0)
	}
}
