package embeddings

// graph_candidates.go — Graph-candidate arm for semantic_search (Phase 1 of the
// graph-first retrieval plan, 2026-06-02).
//
// GraphCandidates proposes candidates from three sub-generators:
//
//	(a) Domain-relevant symbols ranked by PageRank — one AGE round-trip; queries
//	    symbols whose name OR file contains a query keyword, ordered by pagerank DESC.
//	    This is the corrected design: start from query-relevant symbols and rank by
//	    PageRank, rather than starting from the global top-PageRank batch (which is
//	    generic infrastructure: Write, Error, Close, etc.) and filtering by name.
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

	"github.com/anatolykoptev/vaelor/internal/graphx"
)

// graphRowColsExt is the number of columns returned by the extended graph arm
// queries that return (name, file, kind, community).
const graphRowColsExt = 4

// graphRowColsBase is the number of columns returned by basic graph arm
// queries that return (name, file, kind).
const graphRowColsBase = 3

// graphRowColsPR is the number of columns returned by the pagerank sub-arm query
// (name, file) — kind is not stored alongside pagerank in the Symbol vertex.
const graphRowColsPR = 2

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
// prSignals is used only by the caller (annotateWithPageRank) and is no longer
// consumed by sub-arm (a) — pass nil or the fetched batch; both are safe.
// opts controls hop depth, fan-out, seed count, and topK.
//
// Returns nil on any AGE error — the caller falls back to 3-arm behavior.
// Performance contract: 3 Cypher round-trips maximum (sub-arms a, b, and c).
// Sub-arm (a) issues one AGE query: symbols whose name/file matches query keywords,
// ordered by pagerank DESC — a single round-trip that replaces the broken
// in-memory filter over the global top-200 batch (which yielded 0 hits because
// the global top-200 are generic infrastructure symbols: Write, Error, Close, etc.).
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

	hitsPR = e.graphSubArmPageRank(ctx, graphName, queryTerms, seen, opts.topK())
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

// graphSubArmPageRank queries AGE for symbols relevant to the query keywords,
// ranked by pagerank descending. One AGE round-trip.
//
// Design rationale: the previous implementation filtered the global top-200 PageRank
// batch by name-substring match. The top-200 are generic infrastructure symbols
// (Write, Error, Close, String, …) whose names never overlap with domain-specific
// query keywords (embed, gate, fusion, rrf, …) — yielding 0 hits on every query.
//
// Corrected design: start from query-relevant symbols (name OR file contains a
// keyword) and rank them by pagerank. This surfaces the most-important symbols
// among those actually related to the query domain.
//
// Keywords are injected as safe OR-joined toLower() CONTAINS predicates. The
// primary injection guard is lextoken.KeywordTokenize, which upstream reduces
// query terms to [a-z0-9]+ before they reach here. escapeCypherName provides
// defense-in-depth for the literal-quote/backslash class, matching the escaping
// used by graphSubArmCalls and graphSubArmCommunity.
func (e *Expander) graphSubArmPageRank(
	ctx context.Context,
	graphName string,
	queryTerms []string,
	seen map[string]bool,
	limit int,
) []GraphHit {
	if len(queryTerms) == 0 {
		return nil
	}

	// Build keyword OR-filter: toLower(s.name) CONTAINS kw OR toLower(s.file) CONTAINS kw.
	// Terms arrive pre-filtered by lextoken.KeywordTokenize ([a-z0-9]+ only);
	// escapeCypherName is defense-in-depth for the backslash/quote class.
	filterParts := make([]string, 0, len(queryTerms))
	for _, term := range queryTerms {
		kw := escapeCypherName(strings.ToLower(term))
		filterParts = append(filterParts,
			fmt.Sprintf("toLower(s.name) CONTAINS '%s' OR toLower(s.file) CONTAINS '%s'", kw, kw))
	}
	keywordFilter := strings.Join(filterParts, " OR ")

	cypher := fmt.Sprintf(
		`MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL AND (%s) `+
			`RETURN s.name, s.file ORDER BY s.pagerank DESC LIMIT %d`,
		keywordFilter, limit,
	)

	rows := e.execCypherN(ctx, graphName, cypher, "name agtype, file agtype")

	var hits []GraphHit
	for _, row := range rows {
		if len(hits) >= limit {
			break
		}
		if len(row) < graphRowColsPR {
			continue
		}
		name := stripAgtypeQuotes(row[0])
		file := stripAgtypeQuotes(row[1])
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
	maxN := opts.topK()

	var hits []GraphHit
	for i := 0; i < topN && len(hits) < maxN; i++ {
		seed := seeds[i]
		seedName := escapeCypherName(seed.SymbolName)
		cypher := fmt.Sprintf(
			`MATCH (a)-[:CALLS*1..%d]->(b) WHERE a.name = '%s' RETURN b.name, b.file, b.kind LIMIT %d`,
			hops, seedName, fanOut,
		)
		rows := e.execCypher(ctx, graphName, cypher)
		for _, row := range rows {
			if len(hits) >= maxN {
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
//
// Two AGE round-trips:
//  1. A 4-column lookup (name, file, kind, community) to resolve the top seed's
//     community ID. Uses execCypherN with a matching 4-column AS-clause — AGE
//     requires RETURN arity == AS-clause arity exactly.
//  2. A 3-column member fetch (name, file, kind) for all symbols in that community.
func (e *Expander) graphSubArmCommunity(
	ctx context.Context,
	graphName string,
	seeds []SearchResult,
	seen map[string]bool,
	limit int,
) []GraphHit {
	if len(seeds) == 0 {
		return nil
	}

	// Fetch the community of the top seed from the graph.
	// RETURN has 4 columns → AS-clause must also declare 4 columns.
	topSeedName := escapeCypherName(seeds[0].SymbolName)
	commQuery := fmt.Sprintf(
		`MATCH (s:Symbol) WHERE s.name = '%s' RETURN s.name, s.file, s.kind, s.community LIMIT 1`,
		topSeedName,
	)
	rows := e.execCypherN(ctx, graphName, commQuery,
		"name agtype, file agtype, kind agtype, community agtype")
	if len(rows) == 0 || len(rows[0]) < graphRowColsExt {
		return nil
	}
	community := stripAgtypeQuotes(rows[0][3])
	if community == "" || community == "null" {
		return nil
	}

	// Fetch up to limit symbols in the same community (3-column query).
	memberQuery := fmt.Sprintf(
		`MATCH (s:Symbol) WHERE s.community = '%s' RETURN s.name, s.file, s.kind LIMIT %d`,
		escapeCypherName(community), limit,
	)
	memberRows := e.execCypher(ctx, graphName, memberQuery)

	var hits []GraphHit
	for _, row := range memberRows {
		if len(hits) >= limit {
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
// then calls2hop, then community. Bounded by limit.
func mergeGraphHits(pr, calls, comm []GraphHit, limit int) []GraphHit {
	seen := make(map[string]bool, len(pr)+len(calls)+len(comm))
	out := make([]GraphHit, 0, limit)
	for _, group := range [][]GraphHit{pr, calls, comm} {
		for _, h := range group {
			if len(out) >= limit {
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

// escapeCypherName escapes a string for safe inline use as a Cypher string
// literal (single-quoted context). This is defense-in-depth: the primary guard
// is that callers feed keywords through lextoken.KeywordTokenize, which reduces
// input to [a-z0-9]+ before they reach here. escapeCypherName therefore only
// covers the literal-quote/backslash class for direct symbol-name insertions.
//
// Escape order matters: backslash must be escaped first so the quote's added
// backslash is not itself re-escaped. "foo\" → "foo\\" → 'foo\\' (safe);
// "O'Brien" → "O\'Brien" → 'O\'Brien' (safe).
func escapeCypherName(name string) string {
	name = strings.ReplaceAll(name, `\`, `\\`)
	return strings.ReplaceAll(name, "'", "\\'")
}
