package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// toolColdReturnTotal counts short-circuit status responses returned while a
// repo is being built/indexed in the background, broken down by tool and the
// status value in the response. This makes cold-start behavior comparable
// across understand, call_trace, code_graph, federated_cochange, and
// semantic_search.
var toolColdReturnTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_tool_cold_return_total",
		Help: "Short-circuit status responses returned while a repo is being built/indexed in the background, by tool and status.",
	},
	[]string{"tool", "status"},
)

func init() {
	// Pre-touch all known label values so /metrics always exposes them from a cold start.
	pairs := []struct{ tool, status string }{
		{"understand", "building"},
		{"call_trace", "building"},
		{"code_graph", "building"},
		{"federated_cochange", "partial"},
		{"federated_cochange", "building"},
		{"semantic_search", "indexing"},
	}
	for _, p := range pairs {
		toolColdReturnTotal.WithLabelValues(p.tool, p.status).Add(0)
	}
}

// recordToolColdReturn increments gocode_tool_cold_return_total{tool,status}.
func recordToolColdReturn(tool, status string) {
	toolColdReturnTotal.WithLabelValues(tool, status).Inc()
}
