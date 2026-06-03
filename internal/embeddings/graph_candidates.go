package embeddings

// graph_arm.go — Graph-candidate arm for semantic_search (Phase 1 of the
// graph-first retrieval plan, 2026-06-02).
//
// GraphCandidates proposes candidates from three sub-generators:
//
//	(a) High-PageRank symbols whose name contains a query keyword — zero new AGE
//	    round-trips; reuses the prSignals batch already fetched by annotateWithPageRank.
//	(b) 2-hop CALLS neighbors of the strongest dense seeds — bounded by maxHops and
//	    fanOutCap to prevent combinatorial blow-up on dense call-hubs.
//	(c) Same-community members of the top dense seed — community is a vertex property
//	    stored by injectCommunities (codegraph/community.go) at index time.
//
// When RRF_WEIGHT_GRAPH == 0 (dark-launch default) the caller skips this function
// entirely — no latency cost. GraphCandidates is only called when weight > 0.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/graphx"
)

// graphRowColsExt is the number of columns returned by the extended graph arm
// queries that return (name, file, kind, community).
const graphRowColsExt = 4

// graphRowColsBase is the number of columns returned by basic graph arm
// queries that return (name, file, kind).
const graphRowColsBase = 3

// defaultGraphMaxHops is the hop depth for CALLS traversal (sub-arm b).
// Kept at 2 per the risk mitigation in the plan — increase only after measuring
// p99 latency at weight > 0.
const defaultGraphMaxHops = 2

// defaultGraphFanOutCap is the per-seed CALLS neighbor cap to bound the result
// set size from a dense call-hub.
const defaultGraphFanOutCap = 10

// defaultGraphTopSeeds is the number of top dense seeds used for sub-arms (b)
// and (c). Using only the top-3 bounds the number of Cypher queries.
const defaultGraphTopSeeds = 3

// defaultGraphTopK is the maximum total candidates returned from GraphCandidates
// when GraphCandidatesOpts.TopK is unset.
const defaultGraphTopK = 20

// GraphCandidatesOpts controls the graph-candidate generation.
// Zero values use the package defaults above.
type GraphCandidatesOpts struct {
	// MaxHops is the hop depth for the CALLS traversal (sub-arm b). Default 2.
	MaxHops int
	// FanOutCap is the per-seed CALLS result cap (sub-arm b). Default 10.
	FanOutCap int
	// TopSeeds is the number of dense seeds to use for sub-arms (b) and (c). Default 3.
	TopSeeds int
	// TopK is the maximum total candidates returned. Default 20.
	TopK int
}

func (o *GraphCandidatesOpts) hops() int {
	if o == nil || o.MaxHops <= 0 {
		return defaultGraphMaxHops
	}
	return o.MaxHops
}

func (o *GraphCandidatesOpts) fanOut() int {
	if o == nil || o.FanOutCap <= 0 {
		return defaultGraphFanOutCap
	}
	return o.FanOutCap
}

func (o *GraphCandidatesOpts) topSeeds() int {
	if o == nil || o.TopSeeds <= 0 {
		return defaultGraphTopSeeds
	}
	return o.TopSeeds
}

func (o *GraphCandidatesOpts) topK() int {
	if o == nil || o.TopK <= 0 {
		return defaultGraphTopK
	}
	return o.TopK
}

// GraphCandidates generates graph-arm candidates from three sub-generators and
// returns them as []GraphHit ready for MergeRRF's graph arm.
//
// graphName is the AGE graph name (codegraph.GraphNameFor(repoKey)).
// queryTerms is the list of keywords extracted from the user query.
// seeds is the ordered dense search results (top-N used for sub-arms b+c).
// prSignals is the already-fetched TopPageRank batch (sub-arm a is free).
// opts controls hop depth, fan-out, seed count, and topK.
//
// Returns nil on any AGE error — the caller falls back to 3-arm behavior.
// Performance contract: only 2 Cypher round-trips maximum (sub-arms b and c);
// sub-arm (a) is pure in-memory filtering over prSignals.
func (e *Expander) GraphCandidates(
	ctx context.Context,
	graphName string,
	queryTerms []string,
	seeds []SearchResult,
	prSignals []graphx.Signal,
	opts *GraphCandidatesOpts,
) []GraphHit {
	t0 := time.Now()
	seen := buildSeedSet(seeds)

	var (
		hitsPR   []GraphHit
		hitsHop  []GraphHit
		hitsComm []GraphHit
	)

	hitsPR = e.graphSubArmPageRank(queryTerms, prSignals, seen, opts.topK())
	hitsHop = e.graphSubArmCalls(ctx, graphName, seeds, seen, opts)
	hitsComm = e.graphSubArmCommunity(ctx, graphName, seeds, seen, opts.topK())

	all := mergeGraphHits(hitsPR, hitsHop, hitsComm, opts.topK())

	RecordGraphCandidates(len(hitsPR), len(hitsHop), len(hitsComm), len(all), time.Since(t0).Seconds())
	return all
}

// buildSeedSet builds a dedup set keyed by "file:name" from the dense seeds.
func buildSeedSet(seeds []SearchResult) map[string]bool {
	seen := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		seen[s.FilePath+":"+s.SymbolName] = true
	}
	return seen
}

// graphSubArmPageRank filters prSignals to symbols whose name contains at least
// one query keyword. Zero AGE round-trips — pure in-memory filter.
func (e *Expander) graphSubArmPageRank(
	queryTerms []string,
	prSignals []graphx.Signal,
	seen map[string]bool,
	cap int,
) []GraphHit {
	if len(prSignals) == 0 || len(queryTerms) == 0 {
		return nil
	}

	var hits []GraphHit
	for _, sig := range prSignals {
		if len(hits) >= cap {
			break
		}
		name := strings.ToLower(sig.Symbol.Name)
		matched := false
		for _, term := range queryTerms {
			if strings.Contains(name, strings.ToLower(term)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		key := sig.Symbol.File + ":" + sig.Symbol.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, GraphHit{
			FilePath:   sig.Symbol.File,
			SymbolName: sig.Symbol.Name,
		})
	}
	return hits
}

// graphSubArmCalls runs a CALLS*1..N Cypher query for the top-N dense seeds,
// collecting up to fanOutCap neighbors per seed.
func (e *Expander) graphSubArmCalls(
	ctx context.Context,
	graphName string,
	seeds []SearchResult,
	seen map[string]bool,
	opts *GraphCandidatesOpts,
) []GraphHit {
	topN := opts.topSeeds()
	if topN > len(seeds) {
		topN = len(seeds)
	}
	if topN == 0 {
		return nil
	}

	hops := opts.hops()
	fanOut := opts.fanOut()
	cap := opts.topK()

	var hits []GraphHit
	for i := 0; i < topN && len(hits) < cap; i++ {
		seed := seeds[i]
		seedName := escapeCypherName(seed.SymbolName)
		cypher := fmt.Sprintf(
			`MATCH (a)-[:CALLS*1..%d]->(b) WHERE a.name = '%s' RETURN b.name, b.file, b.kind LIMIT %d`,
			hops, seedName, fanOut,
		)
		rows := e.execCypher(ctx, graphName, cypher)
		for _, row := range rows {
			if len(hits) >= cap {
				break
			}
			if len(row) < graphRowColsBase {
				continue
			}
			name := stripAgtypeQuotes(row[0])
			file := stripAgtypeQuotes(row[1])
			kind := stripAgtypeQuotes(row[2])
			if name == "" || file == "" {
				continue
			}
			key := file + ":" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			hits = append(hits, GraphHit{
				FilePath:   file,
				SymbolName: name,
				SymbolKind: kind,
			})
		}
	}
	return hits
}

// graphSubArmCommunity fetches same-community members for the top seed's community.
// The community is embedded as `s.community` vertex property by injectCommunities
// (codegraph/community.go) at index time.
func (e *Expander) graphSubArmCommunity(
	ctx context.Context,
	graphName string,
	seeds []SearchResult,
	seen map[string]bool,
	cap int,
) []GraphHit {
	if len(seeds) == 0 {
		return nil
	}

	// Fetch the community of the top seed from the graph.
	topSeedName := escapeCypherName(seeds[0].SymbolName)
	commQuery := fmt.Sprintf(
		`MATCH (s:Symbol) WHERE s.name = '%s' RETURN s.name, s.file, s.kind, s.community LIMIT 1`,
		topSeedName,
	)
	rows := e.execCypher(ctx, graphName, commQuery)
	if len(rows) == 0 || len(rows[0]) < graphRowColsExt {
		return nil
	}
	community := stripAgtypeQuotes(rows[0][3])
	if community == "" || community == "null" {
		return nil
	}

	// Fetch up to cap symbols in the same community.
	memberQuery := fmt.Sprintf(
		`MATCH (s:Symbol) WHERE s.community = '%s' RETURN s.name, s.file, s.kind LIMIT %d`,
		escapeCypherName(community), cap,
	)
	memberRows := e.execCypher(ctx, graphName, memberQuery)

	var hits []GraphHit
	for _, row := range memberRows {
		if len(hits) >= cap {
			break
		}
		if len(row) < graphRowColsBase {
			continue
		}
		name := stripAgtypeQuotes(row[0])
		file := stripAgtypeQuotes(row[1])
		kind := stripAgtypeQuotes(row[2])
		if name == "" || file == "" {
			continue
		}
		key := file + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, GraphHit{
			FilePath:   file,
			SymbolName: name,
			SymbolKind: kind,
		})
	}
	return hits
}

// mergeGraphHits concatenates the three sub-arm slices, deduplicating by
// "file:name" key. The order is: pagerank hits first (highest signal fidelity),
// then calls2hop, then community. Bounded by cap.
func mergeGraphHits(pr, calls, comm []GraphHit, cap int) []GraphHit {
	seen := make(map[string]bool, len(pr)+len(calls)+len(comm))
	out := make([]GraphHit, 0, cap)
	for _, group := range [][]GraphHit{pr, calls, comm} {
		for _, h := range group {
			if len(out) >= cap {
				return out
			}
			key := h.FilePath + ":" + h.SymbolName
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, h)
		}
	}
	return out
}

// escapeCypherName escapes a symbol name for safe inline use in Cypher.
// Mirrors the escaping logic in expand.go buildNameFilter.
func escapeCypherName(name string) string {
	return strings.ReplaceAll(name, "'", "\\'")
}
