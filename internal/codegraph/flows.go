package codegraph

import (
	"crypto/rand"
	"encoding/hex"
	"sort"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

const (
	// flowsMax is the default cap on the total number of flows per repo.
	// Operators override via FLOWS_MAX in IndexConfig (env: FLOWS_MAX).
	flowsMax = 50

	// flowsDFSDepth is the default DFS traversal depth bound per flow.
	// Operators override via FLOWS_DFS_DEPTH in IndexConfig (env: FLOWS_DFS_DEPTH).
	flowsDFSDepth = 8

	// flowArrow is the separator used in the human-readable flow name.
	flowArrow = " → "
)

// Flow is a named execution path extracted at index time.
// One Flow maps to one row in the code_flows relational table (ADR-001).
type Flow struct {
	FlowID     string   // random hex ID, unique within an index run
	Name       string   // "<entry> → <leaf>" e.g. "handleSearch → MergeRRF"
	EntrySym   string   // entry-point symbol name
	EntryFile  string   // relative file path of the entry symbol
	LeafSym    string   // dominant-leaf symbol name (highest PageRank along chain)
	MemberSyms []string // all symbols in the chain (ordered, entry first)
	Priority   float64  // max PageRank along the chain; used for ranking
	Community  string   // Louvain community id string ("0", "1", …)
}

// ExtractFlows derives named execution flows from the in-memory call graph.
//
// Must be called AFTER injectCommunities and computeSymbolPageRank so both
// maps are fully populated. No AGE I/O — pure in-memory DFS.
//
//   - root           — repo root; used to normalise symbol keys to relFile form
//     consistent with the pagerank map ("name\x00relFile")
//   - cg             — in-memory call graph (CALLS edges + symbol list)
//   - communities    — map[symKey]communityID  (symKey = "name\x00relFile")
//   - pagerank       — map[symKey]score
//   - handlesTargets — set of symKeys that are HANDLES targets;
//     always treated as entry-points regardless of in-degree
//   - maxFlows       — cap on the total number of flows (≤0 uses default flowsMax)
//   - dfsDepth       — DFS traversal depth bound (≤0 uses default flowsDFSDepth)
func ExtractFlows(
	root string,
	cg *callgraph.CallGraph,
	communities map[string]int,
	pagerank map[string]float64,
	handlesTargets map[string]bool,
	maxFlows int,
	dfsDepth int,
) []Flow {
	if maxFlows <= 0 {
		maxFlows = flowsMax
	}
	if dfsDepth <= 0 {
		dfsDepth = flowsDFSDepth
	}

	if cg == nil || len(cg.Symbols) == 0 {
		return []Flow{}
	}

	// Build a callee adjacency index: caller symbol → []callee symbols.
	calleeAdj := buildSymCalleeIndex(cg.Edges)

	// Count in-edges per symbol key to identify zero-caller symbols.
	inDegree := buildInDegree(root, cg.Edges)

	// Determine entry-points.
	entries := collectEntryPoints(root, cg.Symbols, inDegree, handlesTargets)
	if len(entries) == 0 {
		return []Flow{}
	}

	// DFS from each entry-point to collect community-bounded reachable sets.
	flows := make([]Flow, 0, len(entries))
	for _, e := range entries {
		visited := make(map[*parser.Symbol]bool)
		chain := make([]*parser.Symbol, 0, dfsDepth+1)
		entryCommunity := flowSymCommunity(root, e, communities)
		dfsCollectChain(e, calleeAdj, visited, &chain, entryCommunity, communities, root, 0, dfsDepth)
		if len(chain) == 0 {
			continue
		}
		f := buildFlow(root, chain, communities, pagerank)
		if f == nil {
			continue
		}
		flows = append(flows, *f)
	}

	flows = capFlows(flows, maxFlows)
	sortFlows(flows)
	return flows
}

// buildSymCalleeIndex builds a map from each caller *parser.Symbol to its callee symbols.
func buildSymCalleeIndex(edges []callgraph.CallEdge) map[*parser.Symbol][]*parser.Symbol {
	m := make(map[*parser.Symbol][]*parser.Symbol, len(edges))
	for i := range edges {
		e := &edges[i]
		if e.Caller == nil || e.Callee == nil {
			continue
		}
		m[e.Caller] = append(m[e.Caller], e.Callee)
	}
	return m
}

// buildInDegree counts how many callers each symbol key has.
func buildInDegree(root string, edges []callgraph.CallEdge) map[string]int {
	m := make(map[string]int, len(edges))
	for i := range edges {
		e := &edges[i]
		if e.Callee == nil {
			continue
		}
		k := flowSymKey(root, e.Callee)
		m[k]++
	}
	return m
}

// collectEntryPoints returns symbols that are entry-points:
//  1. Targeted by a HANDLES edge (handlesTargets set), OR
//  2. Exported (IsPublic) with zero in-edges.
//
// Both sets can overlap — deduplication is handled by a seen map.
func collectEntryPoints(
	root string,
	symbols []*parser.Symbol,
	inDegree map[string]int,
	handlesTargets map[string]bool,
) []*parser.Symbol {
	seen := make(map[string]bool, len(symbols))
	out := make([]*parser.Symbol, 0, len(symbols)/4+1)

	for _, s := range symbols {
		if s.Kind != parser.KindFunction && s.Kind != parser.KindMethod {
			continue
		}
		k := flowSymKey(root, s)
		if seen[k] {
			continue
		}
		if handlesTargets[k] || isZeroCallerExported(s, inDegree[k]) { //nolint:gocritic // k is flowSymKey result
			seen[k] = true
			out = append(out, s)
		}
	}

	// Sort for determinism before DFS so output order is stable.
	sort.Slice(out, func(i, j int) bool {
		ki := flowSymKey(root, out[i])
		kj := flowSymKey(root, out[j])
		return ki < kj
	})
	return out
}

// isZeroCallerExported returns true when the symbol has no callers AND is
// exported (starts with an uppercase letter — language-agnostic heuristic).
func isZeroCallerExported(s *parser.Symbol, inEdges int) bool {
	return inEdges == 0 && isExported(s)
}

// isExported reports whether the symbol is considered exported for
// flow entry-point purposes. Delegates to the language-aware langutil rule:
//   - IsPublic=true always wins
//   - Go/Java/C#: uppercase-first convention
//   - JS/TS/Rust/Python and others: any non-underscore first rune
func isExported(s *parser.Symbol) bool {
	return langutil.IsExportedForDoc(s.Name, s.Language, s.IsPublic)
}

// dfsCollectChain performs bounded depth-first traversal from src, appending
// visited symbols to chain. Traversal stays within the same Louvain community
// as entryCommunity to keep flows cohesive.
//
// The resulting chain is a community-bounded reachable set (subtree flatten),
// NOT a single linear call path. MemberSyms reflects all symbols reachable from
// the entry within the community up to maxDepth hops.
func dfsCollectChain(
	src *parser.Symbol,
	adj map[*parser.Symbol][]*parser.Symbol,
	visited map[*parser.Symbol]bool,
	chain *[]*parser.Symbol,
	entryCommunity int,
	communities map[string]int,
	root string,
	depth int,
	maxDepth int,
) {
	if depth > maxDepth || visited[src] {
		return
	}
	visited[src] = true
	*chain = append(*chain, src)

	for _, callee := range adj[src] {
		if visited[callee] {
			continue
		}
		// Stay within the entry community.
		if c, ok := communities[flowSymKey(root, callee)]; ok && c != entryCommunity {
			continue
		}
		dfsCollectChain(callee, adj, visited, chain, entryCommunity, communities, root, depth+1, maxDepth)
	}
}

// buildFlow converts a DFS chain into a Flow, picking the leaf by PageRank.
// Returns nil if the chain is empty.
func buildFlow(
	root string,
	chain []*parser.Symbol,
	communities map[string]int,
	pagerank map[string]float64,
) *Flow {
	if len(chain) == 0 {
		return nil
	}
	entry := chain[0]

	// Find the dominant leaf: the highest-PageRank symbol in the chain excluding
	// the entry-point, so the name reads "<entry> → <most important callee>".
	// Falls back to the entry itself when the chain has only one symbol.
	leaf := entry
	if len(chain) > 1 {
		leaf = chain[1]
		maxLeafPR := pagerank[flowSymKey(root, chain[1])]
		for _, s := range chain[2:] {
			if pr := pagerank[flowSymKey(root, s)]; pr > maxLeafPR {
				maxLeafPR = pr
				leaf = s
			}
		}
	}

	// Priority is the max PageRank value along the entire chain.
	priority := 0.0
	for _, s := range chain {
		if pr := pagerank[flowSymKey(root, s)]; pr > priority {
			priority = pr
		}
	}

	members := make([]string, len(chain))
	for i, s := range chain {
		members[i] = s.Name
	}

	community := "0"
	if c, ok := communities[flowSymKey(root, entry)]; ok {
		community = intToString(c)
	}

	name := entry.Name + flowArrow + leaf.Name
	// If entry == leaf (single-symbol chain), still produce a valid name.
	if entry == leaf && len(chain) == 1 {
		name = entry.Name + flowArrow + entry.Name
	}

	return &Flow{
		FlowID:     newFlowID(),
		Name:       name,
		EntrySym:   entry.Name,
		EntryFile:  relPath(entry.File, root),
		LeafSym:    leaf.Name,
		MemberSyms: members,
		Priority:   priority,
		Community:  community,
	}
}

// capFlows keeps only the top-priority flows up to max (per ADR-001 cap rule).
// Mutates the slice in-place by sorting then truncating.
func capFlows(flows []Flow, max int) []Flow {
	if len(flows) <= max {
		return flows
	}
	// Sort descending by priority so the cap retains the highest-priority flows.
	sort.Slice(flows, func(i, j int) bool {
		return flows[i].Priority > flows[j].Priority
	})
	return flows[:max]
}

// sortFlows sorts flows by priority descending, then by name for determinism.
func sortFlows(flows []Flow) {
	sort.Slice(flows, func(i, j int) bool {
		if flows[i].Priority != flows[j].Priority {
			return flows[i].Priority > flows[j].Priority
		}
		return flows[i].Name < flows[j].Name
	})
}

// flowSymKey returns the canonical pagerank/community map key for a symbol.
// Consistent with computeSymbolPageRank: "name\x00relFile".
func flowSymKey(root string, s *parser.Symbol) string {
	return s.Name + compositeKeyDelim + relPath(s.File, root)
}

// flowSymCommunity returns the community id for a symbol, defaulting to 0.
func flowSymCommunity(root string, s *parser.Symbol, communities map[string]int) int {
	if c, ok := communities[flowSymKey(root, s)]; ok {
		return c
	}
	return 0
}

// flowIDBytes is the number of random bytes in a generated flow ID (16 hex chars).
const flowIDBytes = 8

// newFlowID returns a short random hex string for the FlowID field.
func newFlowID() string {
	b := make([]byte, flowIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// intToString converts an int to its decimal string representation.
// Avoids pulling in strconv or fmt in this path.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// buildCommunityMap converts the vertexData slice produced by injectCommunities
// into a map[symKey]communityID suitable for ExtractFlows.
// symKey form: "name\x00relFile" — consistent with computeSymbolPageRank.
func buildCommunityMap(vertices []vertexData) map[string]int {
	m := make(map[string]int, len(vertices))
	for _, v := range vertices {
		if v.Label != "Symbol" {
			continue
		}
		key := v.Props["name"] + compositeKeyDelim + v.Props["file"]
		cid := 0
		for _, r := range v.Props["community"] {
			digit := int(r - '0')
			cid = cid*10 + digit
		}
		m[key] = cid
	}
	return m
}

// extractHandlesTargets returns a set of symKeys (name\x00relFile) for every
// Symbol that is the FROM-side of a HANDLES edge in the given edge list.
// These symbols are entry-points (route handlers) regardless of in-degree.
func extractHandlesTargets(root string, edges []edgeData) map[string]bool {
	out := make(map[string]bool)
	for _, e := range edges {
		if e.EdgeLabel != edgeLabelHandles || e.FromLabel != "Symbol" {
			continue
		}
		// FromKey is "name\x00file" — the file part is already repo-relative
		// (set by handlesFromKey in index_layers.go).
		out[e.FromKey] = true
	}
	_ = root // retained for signature clarity; FromKey already uses relFile
	return out
}
