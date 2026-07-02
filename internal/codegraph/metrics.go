package codegraph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// schemaDriftTotal counts detections of a data table (code_repo_state,
// code_embeddings, code_health_cache) found in ag_catalog instead of public.
// Pre-touched at 0 for all three tables at boot so Prometheus always exports
// the series even when no drift has occurred.
//
// Use via /metrics to detect search_path leak regressions instantly:
//
//	gocode_schema_drift_total{table="code_repo_state"} 0
var schemaDriftTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_schema_drift_total",
		Help: "Count of detections of a public-schema data table found in ag_catalog (search_path leak).",
	},
	[]string{"table"},
)

// graphMissingTotal counts read-path queries that hit a non-existent AGE graph.
// The "tool" label names the MCP tool that triggered the query
// (e.g. "understand", "semantic_search", "code_graph").
// Bump this counter in every IsGraphMissingError guard on the read path.
//
// Use via /metrics to measure how often users query repos that are not indexed:
//
//	code_graph_missing_total{tool="understand"} 12
var graphMissingTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "code_graph_missing_total",
		Help: "Count of read-path queries hitting a non-existent AGE graph, labelled by tool.",
	},
	[]string{"tool"},
)

// recordGraphMissing bumps the graph-missing counter for the named tool.
func recordGraphMissing(tool string) {
	graphMissingTotal.WithLabelValues(tool).Inc()
}

// deadCodeRerankBatchTotal counts build-time dead_code rerank batches by
// outcome. Build-time scoring splits candidates into server-sized batches
// (rerankServerMaxDocs); a "skipped" batch (reranker unavailable, 4xx/5xx, or
// the shared deadline elapsing mid-loop) leaves that batch's candidates with
// their PRIOR scores while the rest refresh — so skipped>0 signals partial
// scoring coverage for that index. Pre-touched at 0 so both series always
// export.
//
//	code_dead_code_rerank_batch_total{outcome="ok"} 5
//	code_dead_code_rerank_batch_total{outcome="skipped"} 0
var deadCodeRerankBatchTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "code_dead_code_rerank_batch_total",
		Help: "Count of build-time dead_code rerank batches by outcome (ok|skipped); skipped>0 means partial scoring coverage.",
	},
	[]string{"outcome"},
)

func init() {
	deadCodeRerankBatchTotal.WithLabelValues("ok").Add(0)
	deadCodeRerankBatchTotal.WithLabelValues("skipped").Add(0)
}

// recordRerankBatch bumps the dead_code rerank batch counter for the outcome
// ("ok" or "skipped").
func recordRerankBatch(outcome string) {
	deadCodeRerankBatchTotal.WithLabelValues(outcome).Inc()
}

// deadCodeScoresPrunedTotal counts code_dead_code_scores rows pruned because
// their function is no longer a current orphan in the live AGE graph (gained
// an incoming CALLS edge, or was removed outright). Without this prune,
// upsertDeadCodeScores is insert/update-only, so the table only ever grows
// and code_health over-counts dead functions monotonically. Unlabeled to
// avoid per-repo cardinality.
var deadCodeScoresPrunedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_dead_code_scores_pruned_total",
		Help: "Total code_dead_code_scores rows pruned because their function is no longer a current orphan (or was removed).",
	},
)

// semanticDupCollapsedTotal counts duplicate file:symbol pairs that were
// collapsed before reaching the caller. Two labels:
//
//   - "semantic_only" — collapsed in semanticOnlyResult (dense + trigram-name
//     arms both returned the same symbol; no MergeRRF dedup ran).
//   - "ce_rerank"     — collapsed in RerankSemanticResults before CE reranker
//     (defensive layer: hardens against phantom rows from Bug B or any future
//     upstream path that skips MergeRRF).
//
// Pre-touched at 0 so both series always export, making regressions visible
// immediately without needing a live duplicate to trigger the first scrape:
//
//	gocode_semantic_dup_collapsed_total{path="semantic_only"} 0
//	gocode_semantic_dup_collapsed_total{path="ce_rerank"} 0
var semanticDupCollapsedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_semantic_dup_collapsed_total",
		Help: "Duplicate file:symbol pairs collapsed before semantic search output, labelled by path (semantic_only|ce_rerank).",
	},
	[]string{"path"},
)

func init() {
	semanticDupCollapsedTotal.WithLabelValues("semantic_only").Add(0)
	semanticDupCollapsedTotal.WithLabelValues("ce_rerank").Add(0)
}

// RecordSemanticDupCollapsed bumps the dup-collapsed counter for the given path
// ("semantic_only" or "ce_rerank"). Exported so cmd/go-code can call it from
// semanticOnlyResult (which already imports codegraph for RerankSemanticResults).
func RecordSemanticDupCollapsed(path string) {
	semanticDupCollapsedTotal.WithLabelValues(path).Inc()
}
