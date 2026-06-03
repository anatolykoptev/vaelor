package semhealth

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// Similarity tier thresholds (similarity = 1 − cosine distance).
// Plan names: T1=0.12 distance → very-close threshold = 1-0.12 = 0.88.
//
//	T2=0.20 distance → related threshold    = 1-0.20 = 0.80.
const (
	tierVeryCloseSimilarity = float32(0.88)
	tierRelatedSimilarity   = float32(0.80)
)

// Tier label strings — match the pre-touched init() labels in dup_metrics.go.
const (
	dupTierExact     = "exact"
	dupTierVeryClose = "very-close"
	dupTierRelated   = "related"
)

// Filter name constants — match the pre-touched init() labels in dup_metrics.go.
const (
	dupFilterTests            = "tests"
	dupFilterSameFile         = "same_file"
	dupFilterKind             = "kind"
	dupFilterBuildTag         = "build_tag"
	dupFilterCallsEdge        = "calls_edge"
	dupFilterInterfaceSibling = "interface_sibling"
)

// Error stage constants — match the pre-touched init() labels in dup_metrics.go.
const (
	dupStageExactQuery   = "exact_query"
	dupStageSimilarQuery = "similar_query"
)

// TriageOpts controls the behaviour of AnalyzeTriage.
type TriageOpts struct {
	// IncludeSameFile, when true, skips the same-file filter so pairs where
	// both endpoints live in the same file are also reported.
	IncludeSameFile bool

	// Root is the on-disk repo root used by the build-tag filter to read each
	// Go file's leading build constraint. When empty, the build-tag filter is a
	// no-op (graceful degradation: a missing checkout must not hide duplicates).
	Root string
}

// TriageResult is returned by AnalyzeTriage.
type TriageResult struct {
	// Groups is the merged, ordered list of duplicate groups.
	// Exact groups come first, then very-close, then related.
	// Within a tier, groups are ordered by AvgSimilarity descending.
	Groups []DupGroup

	// Candidates is the raw similar-pair count returned by FindNearDuplicates
	// before any filtering. Useful for metrics and dashboards.
	Candidates int

	// Dropped maps filter name → number of pairs the filter dropped.
	Dropped map[string]int

	// ReportedByTier maps tier name → number of groups surfaced.
	ReportedByTier map[string]int

	// TimedOut is true when the candidate search was incomplete — one or more
	// per-symbol HNSW queries failed (including statement_timeout SQLSTATE 57014)
	// or the bulk symbol load returned a fatal error. When true, the reported
	// groups are based on partial data; some duplicates may have been missed.
	// Operators should treat a TimedOut result as "possibly incomplete" rather
	// than "no duplicates found".
	TimedOut bool
}

// dupStore is the storage surface that AnalyzeTriage needs.
// It is satisfied by *embeddings.Store in production and by test doubles in
// unit tests.
type dupStore interface {
	// FindNearDuplicates is the scalable per-symbol HNSW k-NN generator (Phase 5).
	// It replaces FindSimilarPairs in the AnalyzeTriage hot path.
	FindNearDuplicates(ctx context.Context, repoKey string, k int, maxDist float32) (embeddings.NearDupResult, error)
	FindExactDuplicates(ctx context.Context, repoKey string) ([]embeddings.ExactDupPair, error)
}

// Compile-time assertion: *embeddings.Store satisfies dupStore.
var _ dupStore = (*embeddings.Store)(nil)

// AnalyzeTriage runs the full tiered duplicate analysis for a repo:
//  1. Exact tier  — body-hash equality scan (index-cheap).
//  2. Similar tiers — per-symbol HNSW k-NN at the "related" threshold (0.80),
//     pruned through the Phase-2 filter chain, then bucketed by similarity.
//
// The candidate generator is FindNearDuplicates (Phase 5 scalable path):
// N × O(log N) per-symbol HNSW queries instead of the O(N²) all-pairs self-join
// used by the old FindSimilarPairs. The size guard (semhealthMaxFuncs) is
// retained for the exact tier but the similar-tier no longer needs it for
// scalability reasons; it is kept for result-size bounding.
//
// The cheap pure filters (tests, same_file, kind) always run BEFORE the AGE
// graph filters (calls_edge, interface_sibling) so the graph receives a pruned
// set and does less work.
//
// Returns nil for invalid inputs (nil store / empty repoKey / zero funcs).
// Returns &TriageResult{} when the repo exceeds semhealthMaxFuncs.
// The exact-tier query error is swallowed (logged + metricked) — the run
// continues with the similar tiers.
// TriageResult.TimedOut is set when FindNearDuplicates reports SearchErrors > 0
// or returns a fatal error — the caller should surface this to the operator.
func AnalyzeTriage(
	ctx context.Context,
	store dupStore,
	gf graphPairFilter,
	graphName string,
	repoKey string,
	totalFuncs int,
	opts TriageOpts,
) *TriageResult {
	if store == nil || repoKey == "" || totalFuncs == 0 {
		return nil
	}
	if totalFuncs > semhealthMaxFuncs {
		slog.Info("semhealth: skipping triage, repo too large",
			slog.Int("totalFuncs", totalFuncs),
			slog.Int("threshold", semhealthMaxFuncs))
		return &TriageResult{}
	}

	start := time.Now()
	defer func() {
		dupDurationSeconds.Observe(time.Since(start).Seconds())
	}()

	dropped := make(map[string]int)

	// ── Exact tier ──────────────────────────────────────────────────────────
	exactGroups := collectExactGroups(ctx, store, repoKey)

	// ── Similar tiers (scalable per-symbol HNSW k-NN, Phase 5) ──────────────
	// relatedDistThreshold is the cosine distance equivalent of tierRelatedSimilarity.
	// similarity 0.80 → distance 0.20.
	const relatedDistThreshold = float32(1) - tierRelatedSimilarity
	pairs, candidates, timedOut := fetchNearDuplicates(ctx, store, repoKey, relatedDistThreshold)
	dupCandidatesTotal.WithLabelValues(repoKey).Add(float64(candidates))

	// Filter chain: cheap pure filters first, then graph filters. The build-tag
	// filter is pure-ish (reads a few KiB of file header) so it runs with the
	// cheap filters, before the AGE graph round-trips.
	pairs, dropped[dupFilterTests] = filterTests(pairs)
	pairs, dropped[dupFilterSameFile] = filterSameFile(pairs, opts.IncludeSameFile)
	pairs, dropped[dupFilterKind] = filterKind(pairs)
	pairs, dropped[dupFilterBuildTag] = filterBuildTagVariants(opts.Root, pairs)
	pairs, dropped[dupFilterCallsEdge] = filterCallsEdges(ctx, gf, graphName, pairs)
	pairs, dropped[dupFilterInterfaceSibling] = filterInterfaceSiblings(ctx, gf, graphName, pairs)

	// Emit filter metrics.
	for name, n := range dropped {
		if n > 0 {
			dupFilteredTotal.WithLabelValues(name).Add(float64(n))
		}
	}

	// Build a kind lookup from the filtered pairs so CollectDupGroups symbols
	// can be annotated with Kind without modifying CollectDupGroups itself.
	kindOf := buildKindMap(pairs)

	similarGroups := buildTieredGroups(pairs, kindOf)

	// ── Merge and sort ───────────────────────────────────────────────────────
	groups := mergeGroups(exactGroups, similarGroups)

	// Populate ReportedByTier.
	reportedByTier := countByTier(groups)

	// Emit reported metrics.
	for tier, n := range reportedByTier {
		if n > 0 {
			dupReportedTotal.WithLabelValues(tier).Add(float64(n))
		}
	}

	return &TriageResult{
		Groups:         groups,
		Candidates:     candidates,
		Dropped:        dropped,
		ReportedByTier: reportedByTier,
		TimedOut:       timedOut,
	}
}

// collectExactGroups runs FindExactDuplicates and converts pairs to DupGroups.
// On error, logs Debug, bumps the error metric, and returns nil (run continues).
func collectExactGroups(ctx context.Context, store dupStore, repoKey string) []DupGroup {
	pairs, err := store.FindExactDuplicates(ctx, repoKey)
	if err != nil {
		slog.Debug("semhealth triage: FindExactDuplicates failed",
			slog.String("repo", repoKey), slog.Any("error", err))
		dupErrorsTotal.WithLabelValues(dupStageExactQuery).Inc()
		return nil
	}
	if len(pairs) == 0 {
		return nil
	}
	return exactPairsToGroups(pairs)
}

// fetchNearDuplicates calls FindNearDuplicates at the related threshold (widest band)
// with the default k. It returns the pairs, the pre-filter candidate count, and a
// timedOut flag — true when SearchErrors > 0 or the call returned a fatal error.
//
// On fatal error (bulk load failure), the error is logged and metricked; the run
// continues with empty pairs so the exact tier can still be reported.
func fetchNearDuplicates(ctx context.Context, store dupStore, repoKey string, maxDist float32) ([]embeddings.SimilarPair, int, bool) {
	res, err := store.FindNearDuplicates(ctx, repoKey, embeddings.DefaultNearDupK, maxDist)
	if err != nil {
		slog.Debug("semhealth triage: FindNearDuplicates fatal error",
			slog.String("repo", repoKey), slog.Any("error", err))
		dupErrorsTotal.WithLabelValues(dupStageSimilarQuery).Inc()
		return nil, 0, true
	}
	if res.SearchErrors > 0 {
		slog.Debug("semhealth triage: FindNearDuplicates partial (search errors)",
			slog.String("repo", repoKey),
			slog.Int("search_errors", res.SearchErrors))
		dupErrorsTotal.WithLabelValues(dupStageSimilarQuery).Add(float64(res.SearchErrors))
		dupTimeoutTotal.Add(float64(res.SearchErrors))
	}
	return res.Pairs, len(res.Pairs), res.SearchErrors > 0
}

// exactPairsToGroups converts ExactDupPairs to DupGroups. Each pair becomes a
// 2-member group with Tier="exact" and Kind populated from SymbolRef.SymbolKind.
// A simple union-find over (file:symbol) merges pairs that share a symbol so
// chains like A=B, B=C collapse to one group.
func exactPairsToGroups(pairs []embeddings.ExactDupPair) []DupGroup {
	type symKey = string // "file:symbol"
	parent := make(map[symKey]symKey)
	kindMap := make(map[symKey]string)
	lineMap := make(map[symKey]int)
	nameMap := make(map[symKey]string)
	fileMap := make(map[symKey]string)

	keyOf := func(r embeddings.SymbolRef) symKey {
		return r.FilePath + ":" + r.SymbolName
	}
	var find func(x symKey) symKey
	find = func(x symKey) symKey {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b symKey) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	for _, p := range pairs {
		ka, kb := keyOf(p.A), keyOf(p.B)
		for _, ref := range []embeddings.SymbolRef{p.A, p.B} {
			k := keyOf(ref)
			if _, ok := parent[k]; !ok {
				parent[k] = k
				kindMap[k] = ref.SymbolKind
				lineMap[k] = ref.StartLine
				nameMap[k] = ref.SymbolName
				fileMap[k] = ref.FilePath
			}
		}
		union(ka, kb)
	}

	// Collect groups by root.
	byRoot := make(map[symKey][]symKey)
	for k := range parent {
		r := find(k)
		byRoot[r] = append(byRoot[r], k)
	}

	groups := make([]DupGroup, 0, len(byRoot))
	for _, members := range byRoot {
		syms := make([]DupSymbol, len(members))
		for i, m := range members {
			syms[i] = DupSymbol{
				Name: nameMap[m],
				File: fileMap[m],
				Line: lineMap[m],
				Kind: kindMap[m],
			}
		}
		groups = append(groups, DupGroup{
			Symbols:       syms,
			AvgSimilarity: 1.0,
			Tier:          dupTierExact,
		})
	}
	return groups
}

// buildKindMap returns a "file:symbol" → kind lookup from a filtered pair slice.
// When a symbol appears in multiple pairs with different kinds, the last one wins
// (all parsers assign a stable kind per symbol so this is deterministic).
func buildKindMap(pairs []embeddings.SimilarPair) map[string]string {
	m := make(map[string]string, len(pairs)*2)
	for _, p := range pairs {
		m[p.FileA+":"+p.SymbolA] = p.KindA
		m[p.FileB+":"+p.SymbolB] = p.KindB
	}
	return m
}

// buildTieredGroups groups filtered similar pairs by CollectDupGroups, then
// assigns the strongest tier among each group's constituent pairs and
// populates DupSymbol.Kind from kindOf.
func buildTieredGroups(pairs []embeddings.SimilarPair, kindOf map[string]string) []DupGroup {
	if len(pairs) == 0 {
		return nil
	}

	// Map "file:symbol" → strongest tier seen in any pair containing that symbol.
	symbolTier := make(map[string]string, len(pairs)*2)
	for _, p := range pairs {
		ka := p.FileA + ":" + p.SymbolA
		kb := p.FileB + ":" + p.SymbolB
		tier := pairTier(p.Similarity)
		for _, k := range []string{ka, kb} {
			if existing, ok := symbolTier[k]; !ok || tierRank(tier) > tierRank(existing) {
				symbolTier[k] = tier
			}
		}
	}

	groups := CollectDupGroups(pairs)

	// Annotate each group: strongest tier among members, Kind from kindOf.
	for i, g := range groups {
		bestTier := dupTierRelated
		for j, s := range g.Symbols {
			k := s.File + ":" + s.Name
			groups[i].Symbols[j].Kind = kindOf[k]
			if t, ok := symbolTier[k]; ok && tierRank(t) > tierRank(bestTier) {
				bestTier = t
			}
		}
		groups[i].Tier = bestTier
	}
	return groups
}

// pairTier maps a similarity score to a tier label.
func pairTier(sim float32) string {
	switch {
	case sim >= tierVeryCloseSimilarity:
		return dupTierVeryClose
	case sim >= tierRelatedSimilarity:
		return dupTierRelated
	default:
		return dupTierRelated // guard: threshold == tierRelatedSimilarity, but be safe
	}
}

// Tier rank constants used by tierRank; higher = stronger.
const (
	rankExact     = 3
	rankVeryClose = 2
	rankRelated   = 1
	rankUnknown   = 0
)

// tierRank maps a tier name to a numeric rank so "stronger" can be compared.
func tierRank(tier string) int {
	switch tier {
	case dupTierExact:
		return rankExact
	case dupTierVeryClose:
		return rankVeryClose
	case dupTierRelated:
		return rankRelated
	default:
		return rankUnknown
	}
}

// mergeGroups concatenates exactGroups (first) and similarGroups (after),
// then sorts within each tier by AvgSimilarity descending.
// The tier ordering invariant is: exact → very-close → related.
func mergeGroups(exactGroups, similarGroups []DupGroup) []DupGroup {
	all := make([]DupGroup, 0, len(exactGroups)+len(similarGroups))
	all = append(all, exactGroups...)
	all = append(all, similarGroups...)

	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := tierRank(all[i].Tier), tierRank(all[j].Tier)
		if ri != rj {
			return ri > rj // higher rank first (exact > very-close > related)
		}
		return all[i].AvgSimilarity > all[j].AvgSimilarity
	})
	return all
}

// countByTier counts groups per tier.
func countByTier(groups []DupGroup) map[string]int {
	m := make(map[string]int)
	for _, g := range groups {
		m[g.Tier]++
	}
	return m
}
