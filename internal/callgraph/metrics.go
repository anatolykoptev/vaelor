package callgraph

import (
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_callees_emitted_total counts call-graph edges by language and kind.
//
//   - kind="call" — primary call (parent is call_expression / equivalent)
//   - kind="argref_kept" — heuristic arg/struct-literal reference that resolved
//     to a known function symbol; kept as an edge.
//   - kind="argref_dropped_unresolved" — heuristic argref dropped because no
//     function symbol matches its name (e.g. `ctx`, `opts.Slug`, `dirPerm`).
//   - kind="argref_kept_legacy" — heuristic argref kept despite no resolution
//     because the caller passed IncludeFieldAccess=true (MCP field_access=true).
//
// Useful for empirically observing the noise reduction of the callees filter
// post-deploy via :8897/metrics. A high argref_dropped_unresolved:call ratio
// at deploy time, dropping after rollout, confirms the fix landed.
var calleesEmittedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_callees_emitted_total",
		Help: "Call graph edges emitted, labelled by language and kind " +
			"(call, argref_kept, argref_kept_legacy, argref_dropped_unresolved).",
	},
	[]string{"language", "kind"},
)

// gocode_callgraph_eager_warm_total counts startup GOCACHE pre-warm outcomes.
// Eager warming is performed once per Go repo discovered under AUTO_INDEX_DIRS
// at process start. The first user request lands on a warm cache and returns
// tier=enhanced from request #1 (instead of tier=basic on cold-cache miss).
//
// Outcomes (cardinality 3, no repo label to keep cardinality bounded):
//   - started   — goroutine kicked off for a repo
//   - completed — go build returned without error
//   - failed    — go build or packages.Load returned an error (non-fatal)
var eagerWarmTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_callgraph_eager_warm_total",
		Help: "Eager startup GOCACHE pre-warm outcomes per repo, labelled by outcome (started, completed, failed).",
	},
	[]string{"outcome"},
)

// recordEagerWarm bumps the eager-warm counter for one outcome.
func recordEagerWarm(outcome string) {
	eagerWarmTotal.WithLabelValues(outcome).Inc()
}

// recordCallee bumps the counter for one parser.CallSite outcome.
func recordCallee(file, kind string) {
	calleesEmittedTotal.WithLabelValues(languageFromExt(file), kind).Inc()
}

// languageFromExt maps a file path to a coarse language label for metrics.
// Unknown extensions report "other" — callers should not introduce a new
// label without bounding cardinality.
func languageFromExt(file string) string {
	ext := strings.ToLower(filepath.Ext(file))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".cjs", ".mjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hh":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".svelte":
		return "svelte"
	case ".astro":
		return "astro"
	default:
		return "other"
	}
}
