package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_rrf_weights publishes the per-retriever weights deployed for the
// hybrid MergeRRF fusion. Operators read /metrics to confirm what's actually
// running matches RRF_WEIGHT_SEMANTIC / RRF_WEIGHT_KEYWORD / RRF_WEIGHT_SPARSE
// env config.
//
// One gauge sample per label value, set once at startup via PublishRRFWeights.
var rrfWeightsGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_rrf_weights",
		Help: "Per-retriever weights used by MergeRRF (1.0 = unweighted RRF baseline).",
	},
	[]string{"retriever"},
)

// PublishRRFWeights records the deployed RRF weights to Prometheus. Idempotent
// (set, not increment); safe to call multiple times if config reloads land.
// Sparse defaults to 0.0 (dark-launched); flipping RRF_WEIGHT_SPARSE>0 after
// Phase 6 A/B will be visible here at next startup.
func PublishRRFWeights(w RRFWeights) {
	rrfWeightsGauge.WithLabelValues("semantic").Set(w.Semantic)
	rrfWeightsGauge.WithLabelValues("keyword").Set(w.Keyword)
	rrfWeightsGauge.WithLabelValues("sparse").Set(w.Sparse)
}
