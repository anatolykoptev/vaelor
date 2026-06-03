package codegraph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// flowsExtractedTotal counts how many named flows were extracted per repo at
// index time. Pre-touched at 0 so /metrics always exports the series.
//
//	gocode_flows_extracted_total{repo="go-code"} 42
var flowsExtractedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_flows_extracted_total",
		Help: "Number of named execution flows extracted at index time, labelled by repo key.",
	},
	[]string{"repo"},
)

// flowsExtractDuration observes how long ExtractFlows + DB upsert take per
// index run. Histogram mirrors the pattern used for gocode_sparse_backfill_*.
var flowsExtractDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gocode_flows_extract_duration_seconds",
		Help:    "Duration of flow extraction (DFS + DB upsert) per index run, labelled by repo key.",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"repo"},
)

// flowsDBErrorTotal counts write failures when persisting flows to code_flows.
// A non-zero value means flows were extracted but not stored — operators should
// investigate. Pre-touched at 0 so the series is always visible.
var flowsDBErrorTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_flows_db_error_total",
		Help: "Count of DB write failures when persisting flows to code_flows, labelled by repo key.",
	},
	[]string{"repo"},
)

func init() {
	// Pre-touch with a sentinel label so Prometheus exports the series at boot
	// before any repo is indexed. The sentinel label value is never used in
	// real counters — real labels are repo keys (e.g. "go-code", "memdb-go").
	flowsExtractedTotal.WithLabelValues("__boot__").Add(0)
	flowsDBErrorTotal.WithLabelValues("__boot__").Add(0)
}
