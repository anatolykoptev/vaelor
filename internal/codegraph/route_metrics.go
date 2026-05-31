package codegraph

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// routesExtractedTotal counts HTTP routes pulled from source per repo.
// The "framework" label names the HTTP framework detected (e.g. "gin",
// "express", "axum"); "side" is "server" or "client".
// Bump this counter once per route successfully extracted from source.
//
//	codegraph_routes_extracted_total{repo="…",framework="gin",side="server"} 42
var routesExtractedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "codegraph_routes_extracted_total",
		Help: "HTTP routes successfully extracted from source per repo, framework, and side.",
	},
	[]string{"repo", "framework", "side"},
)

// routeEdgesBuiltTotal counts HANDLES/FETCHES edges actually created in the
// AGE graph per repo.  Before this counter was added, zero edges were built
// silently (HANDLES=0 fleet-wide) with no observability.
//
//	codegraph_route_edges_built_total{repo="…",label="HANDLES"} 17
var routeEdgesBuiltTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "codegraph_route_edges_built_total",
		Help: "HANDLES/FETCHES edges created in the AGE graph per repo (label = HANDLES|FETCHES).",
	},
	[]string{"repo", "label"},
)

// routeHandlerUnresolvedTotal counts routes dropped because neither a named
// handler symbol nor an enclosing function could be resolved.  This is the
// primary data-loss counter for the empty-Handler bug class (CG-T1 scaffold).
//
//	codegraph_route_handler_unresolved_total{repo="…"} 16
var routeHandlerUnresolvedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "codegraph_route_handler_unresolved_total",
		Help: "Routes dropped because no handler (named or enclosing-fn) could be resolved.",
	},
	[]string{"repo"},
)

// routeRejectedTotal counts routes dropped by the junk/test-file filter before
// edge-building.  The "reason" label distinguishes the two rejection classes.
//
//	codegraph_route_rejected_total{repo="…",reason="junk"} 3
var routeRejectedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "codegraph_route_rejected_total",
		Help: "Routes dropped by the junk/test-file filter (reason = junk|test_file).",
	},
	[]string{"repo", "reason"},
)

// graphBuildTotal counts graph build outcomes per repo.
//
//	codegraph_graph_build_total{repo="…",status="ok"} 1
var graphBuildTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "codegraph_graph_build_total",
		Help: "Graph build outcomes per repo (status = ok|error|skip).",
	},
	[]string{"repo", "status"},
)

// recordRoutesExtracted bumps the routes-extracted counter for the given repo,
// HTTP framework, and side ("server"|"client").
func recordRoutesExtracted(repo, framework, side string) {
	routesExtractedTotal.WithLabelValues(repo, framework, side).Inc()
}

// recordRouteEdgeBuilt bumps the route-edges-built counter for the given repo
// and edge label ("HANDLES"|"FETCHES").
func recordRouteEdgeBuilt(repo, label string) {
	routeEdgesBuiltTotal.WithLabelValues(repo, label).Inc()
}

// recordRouteHandlerUnresolved bumps the handler-unresolved counter for the
// given repo.  Call this whenever a route is dropped because its Handler field
// is empty and no enclosing function can be found.
func recordRouteHandlerUnresolved(repo string) {
	routeHandlerUnresolvedTotal.WithLabelValues(repo).Inc()
}

// recordRouteRejected bumps the route-rejected counter for the given repo and
// rejection reason ("junk"|"test_file").
func recordRouteRejected(repo, reason string) {
	routeRejectedTotal.WithLabelValues(repo, reason).Inc()
}

// recordGraphBuild bumps the graph-build counter for the given repo and build
// status ("ok"|"error"|"skip").
func recordGraphBuild(repo, status string) {
	graphBuildTotal.WithLabelValues(repo, status).Inc()
}
