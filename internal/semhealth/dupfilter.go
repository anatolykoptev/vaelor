package semhealth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/langutil"
)

// lowSignalKinds is the set of symbol kinds whose semantic similarity is not
// actionable as a duplicate signal. Getters, field declarations, constants, and
// import aliases look nearly identical by definition; flagging them generates
// noise without surfacing refactor opportunities.
// "function" and "method" (including cross-kind function↔method pairs) are
// intentionally absent — a free function duplicating a method body is exactly
// the target class of find-duplicates.
var lowSignalKinds = map[string]bool{
	"field":  true, // forward-defensive: no current parser emits "field", but a struct-field-aware grammar would
	"var":    true,
	"const":  true,
	"import": true,
}

// errGraphUnavailable is a sentinel used by tests to simulate graph errors.
// Production code returns nil error on graph-missing (graceful degradation);
// this sentinel lets tests distinguish "error path" from "empty-result path".
var errGraphUnavailable = errors.New("graph unavailable")

// GraphPairFilter is the exported injection seam for AGE graph queries. It is
// satisfied by *embeddings.Expander in production and by test doubles in unit
// tests. cmd/go-code references this type to wire the concrete Expander.
type GraphPairFilter interface {
	PairsConnectedByCalls(ctx context.Context, graphName string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error)
	PairsSharingInterface(ctx context.Context, graphName string, pairs []embeddings.PairKey) (map[embeddings.PairKey]bool, error)
}

// graphPairFilter is a package-internal alias kept for the unexported uses in
// filter functions and tests — avoids scattering the exported name internally.
type graphPairFilter = GraphPairFilter

// Compile-time assertion: *embeddings.Expander must satisfy GraphPairFilter.
// If either method signature drifts in Expander, this line breaks the build here,
// alerting the author before the mismatch reaches production.
// Mirrors the _ symbolNameSearcher = (*embeddings.Store)(nil) pattern in
// cmd/go-code/semantic_fallback.go.
var _ GraphPairFilter = (*embeddings.Expander)(nil)

// pairKeyOf builds the canonical embeddings.PairKey for a SimilarPair.
func pairKeyOf(p embeddings.SimilarPair) embeddings.PairKey {
	return embeddings.NewPairKey(p.FileA, p.SymbolA, p.FileB, p.SymbolB)
}

// filterSameFile drops pairs where both symbols are in the same file.
// When includeSameFile is true the filter is a no-op (all pairs are kept).
// Returns the kept slice and the number of dropped pairs.
func filterSameFile(pairs []embeddings.SimilarPair, includeSameFile bool) (kept []embeddings.SimilarPair, dropped int) {
	if includeSameFile {
		return pairs, 0
	}
	kept = pairs[:0:0] // start empty with cap 0; append allocates fresh, never aliases caller's slice
	for _, p := range pairs {
		if p.FileA == p.FileB {
			dropped++
			continue
		}
		kept = append(kept, p)
	}
	return kept, dropped
}

// filterTests drops pairs where either endpoint is a test file
// (as determined by langutil.IsTestFile). Test mirrors are expected to look
// identical to their implementation counterparts and are not duplicate candidates.
func filterTests(pairs []embeddings.SimilarPair) (kept []embeddings.SimilarPair, dropped int) {
	for _, p := range pairs {
		if langutil.IsTestFile(p.FileA) || langutil.IsTestFile(p.FileB) {
			dropped++
			continue
		}
		kept = append(kept, p)
	}
	return kept, dropped
}

// filterKind drops pairs where either endpoint has a low-signal symbol kind
// (field, var, const, import). function↔method cross-kind pairs are kept
// because a free function body duplicating a method body is a genuine target.
func filterKind(pairs []embeddings.SimilarPair) (kept []embeddings.SimilarPair, dropped int) {
	for _, p := range pairs {
		if lowSignalKinds[p.KindA] || lowSignalKinds[p.KindB] {
			dropped++
			continue
		}
		kept = append(kept, p)
	}
	return kept, dropped
}

// filterCallsEdges drops pairs where the two endpoints have a CALLS edge between
// them in the AGE graph (either direction). Caller/callee pairs look semantically
// similar because they share vocabulary, but they are not duplicates.
//
// Graceful-degradation contract: if gf is nil or returns an error, no pairs are
// dropped (a transient graph hiccup must not silently hide real duplicates).
func filterCallsEdges(ctx context.Context, gf graphPairFilter, graphName string, pairs []embeddings.SimilarPair) (kept []embeddings.SimilarPair, dropped int) {
	if gf == nil || len(pairs) == 0 {
		return pairs, 0
	}

	keys := make([]embeddings.PairKey, len(pairs))
	for i, p := range pairs {
		keys[i] = pairKeyOf(p)
	}

	connected, err := gf.PairsConnectedByCalls(ctx, graphName, keys)
	if err != nil {
		slog.Debug("dupfilter: PairsConnectedByCalls failed, keeping all pairs",
			slog.String("graph", graphName), slog.Any("error", err))
		return pairs, 0
	}

	for _, p := range pairs {
		if connected[pairKeyOf(p)] {
			dropped++
			continue
		}
		kept = append(kept, p)
	}
	return kept, dropped
}

// filterInterfaceSiblings drops pairs where both endpoints implement the same
// interface node in the AGE graph. Multiple structs implementing the same
// interface (e.g. four Search methods) are the largest false-positive class:
// they look semantically identical but are correct distinct implementations.
//
// Same graceful-degradation contract as filterCallsEdges.
func filterInterfaceSiblings(ctx context.Context, gf graphPairFilter, graphName string, pairs []embeddings.SimilarPair) (kept []embeddings.SimilarPair, dropped int) {
	if gf == nil || len(pairs) == 0 {
		return pairs, 0
	}

	keys := make([]embeddings.PairKey, len(pairs))
	for i, p := range pairs {
		keys[i] = pairKeyOf(p)
	}

	siblings, err := gf.PairsSharingInterface(ctx, graphName, keys)
	if err != nil {
		slog.Debug("dupfilter: PairsSharingInterface failed, keeping all pairs",
			slog.String("graph", graphName), slog.Any("error", err))
		return pairs, 0
	}

	for _, p := range pairs {
		if siblings[pairKeyOf(p)] {
			dropped++
			continue
		}
		kept = append(kept, p)
	}
	return kept, dropped
}
