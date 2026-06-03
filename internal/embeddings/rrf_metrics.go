package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_rrf_weights publishes the per-retriever weights deployed for the
// hybrid MergeRRF fusion. Operators read /metrics to confirm what's actually
// running matches RRF_WEIGHT_SEMANTIC / RRF_WEIGHT_KEYWORD / RRF_WEIGHT_SPARSE /
// RRF_WEIGHT_GRAPH env config.
//
// One gauge sample per label value, set once at startup via PublishRRFWeights.
var rrfWeightsGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_rrf_weights",
		Help: "Per-retriever weights used by MergeRRF (1.0 = unweighted RRF baseline).",
	},
	[]string{"retriever"},
)

// gocode_graph_candidates_total counts graph arm candidates by generator stage:
//
//	pagerank  — high-PageRank symbols matching a query term (sub-arm a).
//	calls2hop — 2-hop CALLS neighbors of top dense seeds (sub-arm b).
//	community — same-community members of the top seed (sub-arm c).
//	total     — deduped candidates returned to MergeRRF.
//
// Pre-touched in init() so /metrics always exposes every label even before the
// first query (mirrors the keywordArmTotal pattern in tool_semantic_search_hybrid.go).
var graphCandidatesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_graph_candidates_total",
		Help: "Total graph-arm candidate symbols produced per generator stage.",
	},
	[]string{"stage"},
)

// gocode_graph_arm_duration_seconds measures the wall-clock latency of the
// graph-candidate generation call (covers all three sub-arms end-to-end).
var graphArmDuration = promauto.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "gocode_graph_arm_duration_seconds",
		Help:    "Latency of the graph-candidate arm (GraphCandidates call) per semantic_search query.",
		Buckets: prometheus.DefBuckets,
	},
)

func init() {
	// Pre-touch all label values so /metrics always exposes every counter,
	// even before the first query arrives (mirrors keywordArmTotal pattern).
	graphCandidatesTotal.WithLabelValues("pagerank").Add(0)
	graphCandidatesTotal.WithLabelValues("calls2hop").Add(0)
	graphCandidatesTotal.WithLabelValues("community").Add(0)
	graphCandidatesTotal.WithLabelValues("total").Add(0)
}

// PublishRRFWeights records the deployed RRF weights to Prometheus. Idempotent
// (set, not increment); safe to call multiple times if config reloads land.
// Sparse defaults to 0.0 (dark-launched); Graph defaults to 0.0 (dark-launched).
// Flipping RRF_WEIGHT_SPARSE or RRF_WEIGHT_GRAPH after A/B gate will be visible
// here at next startup.
func PublishRRFWeights(w RRFWeights) {
	rrfWeightsGauge.WithLabelValues("semantic").Set(w.Semantic)
	rrfWeightsGauge.WithLabelValues("keyword").Set(w.Keyword)
	rrfWeightsGauge.WithLabelValues("sparse").Set(w.Sparse)
	rrfWeightsGauge.WithLabelValues("graph").Set(w.Graph)
}

// RecordGraphCandidates increments the per-stage counters and the latency
// histogram for one GraphCandidates call. Called by graph_arm.go.
func RecordGraphCandidates(nPageRank, nCalls2Hop, nCommunity, nTotal int, durationSec float64) {
	graphCandidatesTotal.WithLabelValues("pagerank").Add(float64(nPageRank))
	graphCandidatesTotal.WithLabelValues("calls2hop").Add(float64(nCalls2Hop))
	graphCandidatesTotal.WithLabelValues("community").Add(float64(nCommunity))
	graphCandidatesTotal.WithLabelValues("total").Add(float64(nTotal))
	graphArmDuration.Observe(durationSec)
}
