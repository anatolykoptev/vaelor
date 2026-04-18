// Package graphx defines the cooperation interfaces between the ephemeral
// callgraph (internal/callgraph) and the persistent AGE graph (internal/codegraph).
// Both packages may implement these interfaces without either importing the other.
//
// Consumers receive an Analytics and/or CrossRefs value through analyze.Deps.
// When no persistent graph is available, Noop{} is used as the fallback — all
// methods return zero/empty results and never error.
package graphx

import "context"

// Signals holds scalar analytics for a single symbol computed by the persistent
// graph. When Found is false the graph is cold or the symbol was not indexed;
// callers MUST skip enrichment in that case.
type Signals struct {
	// PageRank is the normalized PageRank score (0..1).
	PageRank float64
	// Community is the Louvain community identifier. Empty when unassigned.
	Community string
	// Surprise is the structural surprise score (higher = less expected).
	Surprise float64
	// Found is false when the graph has no snapshot for this repo/symbol.
	// Zero values for PageRank/Community/Surprise are meaningless when Found is false.
	Found bool
}

// Signal pairs a symbol reference with its analytics signals.
// Used as an element type by Analytics.TopPageRank.
type Signal struct {
	Symbol SymbolRef
	Signals
}

// SymbolRef is a lightweight cross-package identifier for a symbol.
type SymbolRef struct {
	// Name is the qualified symbol name (e.g. "pkg.FunctionName").
	Name string
	// File is the source file path relative to the repo root.
	File string
}

// Route identifies an HTTP route by method and path pattern.
// An empty Method means "any method".
type Route struct {
	// Method is the HTTP method (e.g. "GET", "POST"). Empty means any.
	Method string
	// Path is the route path pattern (e.g. "/api/users/:id").
	Path string
}

// Analytics returns persistent-graph-computed scalar signals for symbols.
// Implementations MUST tolerate repos with no snapshot and return
// Signals{Found: false} rather than an error.
type Analytics interface {
	// Symbol returns the pagerank, community, and surprise signals for a
	// single symbol in the given repo.
	Symbol(ctx context.Context, repoKey, symbolName, file string) (Signals, error)

	// TopPageRank returns the k symbols with the highest pagerank in the repo,
	// ordered descending. Returns an empty slice (not an error) when the graph
	// has no snapshot.
	TopPageRank(ctx context.Context, repoKey string, k int) ([]Signal, error)
}

// CrossRefs surfaces graph edges that the ephemeral callgraph does not carry:
// HTTP route bindings, cross-language FETCHES, and test coverage edges.
// Implementations MUST tolerate missing snapshots and return empty results.
type CrossRefs interface {
	// HandlesRoute returns the HTTP Route served by a handler symbol, and
	// whether such a binding was found. Returns Route{}, false, nil when absent.
	HandlesRoute(ctx context.Context, repoKey, symbolName, file string) (Route, bool, error)

	// FetchedBy returns the frontend (or upstream) symbols that issue HTTP
	// requests to the given route. Returns nil, nil when none are found.
	FetchedBy(ctx context.Context, repoKey string, route Route) ([]SymbolRef, error)

	// TestedBy returns test functions that directly cover the given production
	// symbol. Returns nil, nil when no test edges are recorded.
	TestedBy(ctx context.Context, repoKey, symbolName, file string) ([]SymbolRef, error)
}

// Noop is the zero-cost fallback implementation used when no persistent graph
// is available (e.g. DATABASE_URL is empty or AGE is not ready).
// Every method returns an empty/zero result and never errors.
type Noop struct{}

// Symbol implements Analytics. Always returns Signals{Found: false}.
func (Noop) Symbol(_ context.Context, _, _, _ string) (Signals, error) {
	return Signals{}, nil
}

// TopPageRank implements Analytics. Always returns nil, nil.
func (Noop) TopPageRank(_ context.Context, _ string, _ int) ([]Signal, error) {
	return nil, nil
}

// HandlesRoute implements CrossRefs. Always returns Route{}, false, nil.
func (Noop) HandlesRoute(_ context.Context, _, _, _ string) (Route, bool, error) {
	return Route{}, false, nil
}

// FetchedBy implements CrossRefs. Always returns nil, nil.
func (Noop) FetchedBy(_ context.Context, _ string, _ Route) ([]SymbolRef, error) {
	return nil, nil
}

// TestedBy implements CrossRefs. Always returns nil, nil.
func (Noop) TestedBy(_ context.Context, _, _, _ string) ([]SymbolRef, error) {
	return nil, nil
}

// Compile-time interface satisfaction checks.
var _ Analytics = Noop{}
var _ CrossRefs = Noop{}
