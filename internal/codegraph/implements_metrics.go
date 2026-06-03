package codegraph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// implementsLoadTotal counts go/types satisfaction-load outcomes at index time:
// "ok" (packages loaded, satisfaction computed) or "error" (no go.mod handled
// upstream; here it is a load/timeout failure that fell back to the signature
// heuristic). A rising "error" rate means IMPLEMENTS enrichment is silently
// degrading — the find_duplicates filter is on its heuristic path for those repos.
//
//	gocode_implements_load_total{result="ok"} 12
//	gocode_implements_load_total{result="error"} 1
var implementsLoadTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_implements_load_total",
		Help: "Count of go/types interface-satisfaction load attempts at index time, labelled by result (ok|error).",
	},
	[]string{"result"},
)

// implementsEdgesTotal counts how many IMPLEMENTS edges the go/types pass produced
// per repo. Zero on a Go repo with a successful load means the module genuinely has
// no satisfied interfaces; zero with an "error" load means the pass did not run.
//
//	gocode_implements_edges_total{repo="go-code"} 84
var implementsEdgesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_implements_edges_total",
		Help: "Number of IMPLEMENTS (type→interface) edges built via go/types at index time, labelled by repo key.",
	},
	[]string{"repo"},
)

func init() {
	// Pre-touch so /metrics exports the series at boot before any repo is indexed.
	implementsLoadTotal.WithLabelValues("ok").Add(0)
	implementsLoadTotal.WithLabelValues("error").Add(0)
	implementsEdgesTotal.WithLabelValues("__boot__").Add(0)
}
