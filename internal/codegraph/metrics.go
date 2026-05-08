package codegraph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
