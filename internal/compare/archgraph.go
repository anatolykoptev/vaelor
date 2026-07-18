package compare

import (
	"context"
	"errors"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
)

// ArchMetrics holds architectural metrics derived from the code graph.
type ArchMetrics struct {
	PackageCount      int           `json:"packageCount"`
	CommunityCount    int           `json:"communityCount"`
	CrossPkgCallRatio float64       `json:"crossPkgCallRatio"`
	MaxCallDepth      int           `json:"maxCallDepth"`
	InterfaceRatio    float64       `json:"interfaceRatio"`
	GodPackages       []GodPackage  `json:"godPackages,omitempty"`
	CircularDeps      []CircularDep `json:"circularDeps,omitempty"`
	// NotIndexed is set when the code graph has no packages for this repo,
	// meaning code_graph tool was never called with this repo path. Call
	// code_graph with the same repo first to populate the graph.
	NotIndexed bool `json:"notIndexed,omitempty"`
	// Approximate is set when metrics were derived from an in-memory call graph
	// rather than the full Apache AGE graph. MaxCallDepth, InterfaceRatio, and
	// CommunityCount are not computed in the approximate path and must not be
	// presented as real measurements.
	Approximate bool   `json:"approximate,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// GodPackage represents a package with many importers (high coupling).
type GodPackage struct {
	Name      string `json:"name"`
	Importers int    `json:"importers"`
}

// CircularDep represents a circular dependency between two packages.
type CircularDep struct {
	PackageA string `json:"packageA"`
	PackageB string `json:"packageB"`
}

const godPackageThreshold = 5 // packages with 5+ importers are considered "god packages"

// HintApproxArchMetrics is the caveat attached to ArchMetrics results derived from
// an in-memory call graph rather than the full Apache AGE graph. MaxCallDepth,
// InterfaceRatio, and CommunityCount are not computed by the fallback.
const HintApproxArchMetrics = "Architecture metrics are approximate (derived from in-memory call graph). " +
	"Run the `code_graph` tool with the same repo path for full analysis including call depth."

// hintNotIndexed is the message emitted when the fallback itself also fails (no graph at all).
const hintNotIndexed = "Architecture metrics unavailable: code graph not indexed for this repo. " +
	"Call the `code_graph` tool with the same repo path first to populate the graph, " +
	"then re-run code_compare."

// CollectArchMetrics queries the Apache AGE code graph for architectural metrics.
// Each sub-query is non-fatal: failures are logged and skipped.
// When the graph is absent (NotIndexed), it falls back to FallbackArchMetrics
// to derive PackageCount and CrossPkgCallRatio from an in-memory call graph.
func CollectArchMetrics(ctx context.Context, store *codegraph.Store, root string) *ArchMetrics {
	if store == nil {
		return nil
	}

	graph := codegraph.GraphNameFor(root)
	m := &ArchMetrics{}

	// Preflight: check graph existence before issuing any Cypher queries.
	// Avoids postgres ERROR logs for repos that were never indexed.
	// The existing IsGraphMissingError guards in each sub-query remain as
	// race fallbacks (graph dropped between preflight and query).
	if err := store.EnsureGraphExistsForRead(ctx, graph); err != nil {
		if errors.Is(err, codegraph.ErrGraphNotIndexed) {
			slog.Debug("archgraph: graph absent (preflight) — using in-memory fallback", "graph", graph)
			if fb := FallbackArchMetrics(ctx, root); fb != nil {
				fb.Approximate = true
				fb.Hint = HintApproxArchMetrics
				return fb
			}
			m.NotIndexed = true
			m.Hint = hintNotIndexed
			return m
		}
		slog.Warn("archgraph: preflight check failed", "graph", graph, "err", err)
		// Fall through — let sub-queries attempt and log their own errors.
	}

	// Package count doubles as an indexed-graph probe (fastest query).
	m.PackageCount = queryPackageCount(ctx, store, graph)
	if m.PackageCount == 0 {
		// Graph is empty — fall back to in-memory call graph for basic metrics.
		slog.Debug("archgraph: graph empty — using in-memory fallback", "graph", graph)
		if fb := FallbackArchMetrics(ctx, root); fb != nil {
			fb.Approximate = true
			fb.Hint = HintApproxArchMetrics
			return fb
		}
		m.NotIndexed = true
		m.Hint = hintNotIndexed
		return m
	}

	// Serial execution — pgxpool has only ~4 connections by default.
	// Two repos run in parallel (called from enrich.go), so 2 concurrent
	// queries is safe. More concurrency exhausts the pool.
	m.CrossPkgCallRatio = queryCrossPkgRatio(ctx, store, graph)
	m.CommunityCount = queryCommunityCount(ctx, store, graph)
	m.InterfaceRatio = queryInterfaceRatio(ctx, store, graph)
	m.GodPackages = queryGodPackages(ctx, store, graph)
	m.CircularDeps = queryCircularDeps(ctx, store, graph)
	// MaxCallDepth is expensive (variable-length path probing) — run last.
	m.MaxCallDepth = queryMaxCallDepth(ctx, store, graph)

	return m
}
