package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// interfaceSiblingPathTotal counts which discriminator path PairsSharingInterface
// took per invocation: "exact" when real IMPLEMENTS edges drove the decision
// (Go repo reindexed with the go/types satisfaction pass), or "heuristic" when no
// IMPLEMENTS edges exist for the graph and the #218 signature-receiver fallback
// ran (non-Go repos, or a Go repo not yet reindexed).
//
// An operator watching find_duplicates precision can see, per run, whether
// suppression was edge-driven or heuristic — and whether a repo expected to be on
// the exact path silently fell back (e.g. reindex never happened).
//
//	gocode_interface_sibling_path_total{path="exact"} 7
//	gocode_interface_sibling_path_total{path="heuristic"} 2
var interfaceSiblingPathTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_interface_sibling_path_total",
		Help: "Count of PairsSharingInterface invocations by discriminator path (exact|heuristic).",
	},
	[]string{"path"},
)

const (
	ifacePathExact     = "exact"
	ifacePathHeuristic = "heuristic"
)

func init() {
	// Pre-touch both paths so /metrics exports the series before any triage runs.
	interfaceSiblingPathTotal.WithLabelValues(ifacePathExact).Add(0)
	interfaceSiblingPathTotal.WithLabelValues(ifacePathHeuristic).Add(0)
}
