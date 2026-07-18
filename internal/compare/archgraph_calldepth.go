package compare

import (
	"context"
	"fmt"
	"strconv"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
)

// Call depth probing limits.
// Variable-length path queries are O(edges^depth) — must cap aggressively.
const (
	maxProbedCallDepth    = 10
	maxProbedCallDepthXL  = 6 // cap for large graphs to avoid exponential blowup
	largeGraphEdgesCutoff = 8000
)

func queryMaxCallDepth(ctx context.Context, store *codegraph.Store, graph string) int {
	// AGE limitation: max(length(p)) doesn't work in aggregate.
	// Strategy: binary search via fixed-length paths *N..N.
	//
	// For large graphs (>8000 edges) we cap depth at 6: depth=7+ on large
	// graphs can take 30+ seconds due to exponential path explosion.
	hi := maxProbedCallDepth
	if isLargeGraph(ctx, store, graph) {
		hi = maxProbedCallDepthXL
	}

	lo, best := 1, 0
	for lo <= hi {
		mid := (lo + hi) / 2
		cypher := fmt.Sprintf(
			`MATCH (a:Symbol)-[:CALLS*%d..%d]->(b:Symbol) RETURN count(*)`, mid, mid)
		rows, err := store.ExecCypher(ctx, graph, cypher, 1)
		if err != nil || len(rows) == 0 {
			// Query failed (likely timeout) — try shallower.
			hi = mid - 1
			continue
		}
		n, _ := strconv.Atoi(rows[0][0])
		if n > 0 {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

// isLargeGraph returns true when the graph has many CALLS edges, signaling
// that variable-length path queries will be expensive.
func isLargeGraph(ctx context.Context, store *codegraph.Store, graph string) bool {
	rows, err := store.ExecCypher(ctx, graph,
		`MATCH ()-[:CALLS]->() RETURN count(*)`, 1)
	if err != nil || len(rows) == 0 {
		return false
	}
	n, _ := strconv.Atoi(rows[0][0])
	return n > largeGraphEdgesCutoff
}
