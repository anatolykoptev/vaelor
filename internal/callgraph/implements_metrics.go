package callgraph

import (
	"github.com/anatolykoptev/go-code/internal/strutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// implementsLoadTotal counts go/types satisfaction-load outcomes:
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
		Help: "Count of go/types interface-satisfaction load attempts, labelled by result (ok|error).",
	},
	[]string{"result"},
)

// implementsEdgesTotal counts how many IMPLEMENTS edges the go/types pass produced
// per repo. Zero on a Go repo with a successful load means the module genuinely has
// no satisfied interfaces; zero with an "error" load means the pass did not run.
//
// The "repo" label is NOT a human slug — it is strutil.RepoKey(root), the
// FNV-32a repo key rendered as code_<8hex>, so a repo at root
// "/host/src/go-code" surfaces as e.g. code_a1b2c3d4. Map a hex key back to a
// path with codegraph.GraphNameFor(path), or grep the load log line which
// carries the raw root. Querying repo="go-code" matches nothing.
//
//	gocode_implements_edges_total{repo="code_a1b2c3d4"} 84
var implementsEdgesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_implements_edges_total",
		Help: "Number of IMPLEMENTS (type→interface) edges built via go/types, labelled by the FNV-32a repo key (code_<8hex>, == strutil.RepoKey/graphName), NOT a human repo slug.",
	},
	[]string{"repo"},
)

func init() {
	// Pre-touch so /metrics exports the series at boot before any repo is indexed.
	implementsLoadTotal.WithLabelValues("ok").Add(0)
	implementsLoadTotal.WithLabelValues("error").Add(0)
	implementsEdgesTotal.WithLabelValues("__boot__").Add(0)
}

// implementsRepoKey returns the prometheus label for per-repo IMPLEMENTS metrics.
// Wraps strutil.RepoKey so callers don't need to import strutil directly.
func implementsRepoKey(root string) string {
	return strutil.RepoKey(root)
}
