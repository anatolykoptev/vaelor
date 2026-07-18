package main

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// buildHybridCandidates unions the candidate symbols produced by semantic,
// keyword, sparse, and graph retrievers into a single GraphHit slice. The first
// occurrence of a (file, symbol) pair wins; the signal arms only need the key
// to compute file-level hotspot/recency scores.
func buildHybridCandidates(
	semantic []embeddings.SearchResult,
	keyword []embeddings.KeywordHit,
	sparse []embeddings.SparseHit,
	graph []embeddings.GraphHit,
) []embeddings.GraphHit {
	seen := make(map[string]struct{}, len(semantic)+len(keyword)+len(sparse)+len(graph))
	out := make([]embeddings.GraphHit, 0, len(semantic)+len(keyword)+len(sparse)+len(graph))

	add := func(h embeddings.GraphHit) {
		key := h.FilePath + ":" + h.SymbolName
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}

	for _, r := range semantic {
		add(embeddings.GraphHit{
			FilePath:   r.FilePath,
			SymbolName: r.SymbolName,
			SymbolKind: r.SymbolKind,
			Line:       r.StartLine,
		})
	}
	for _, h := range keyword {
		add(embeddings.GraphHit{FilePath: h.FilePath, SymbolName: h.SymbolName, Line: h.Line})
	}
	for _, h := range sparse {
		add(embeddings.GraphHit{FilePath: h.FilePath, SymbolName: h.SymbolName, Line: h.Line})
	}
	for _, h := range graph {
		add(h)
	}
	return out
}

// buildSignalHits builds the hotspot and recency RRF arms for the candidate set.
// It returns nil for either arm when the feature is disabled, the graph store is
// unavailable, or the underlying data is missing. Errors are non-fatal and only
// logged at debug level to keep the query hot-path resilient.
func buildSignalHits(
	ctx context.Context,
	deps SemanticDeps,
	repoKey, root string,
	candidates []embeddings.GraphHit,
	rerankCap int,
) (hotspot, recency []embeddings.GraphHit) {
	if len(candidates) == 0 || deps.GraphStore == nil {
		return nil, nil
	}
	if deps.RRFWeights.Hotspot <= 0 && deps.RRFWeights.Recency <= 0 {
		return nil, nil
	}

	mtimes, err := deps.GraphStore.GetFileMtimes(ctx, repoKey)
	if err != nil {
		slog.Debug("semantic_search: GetFileMtimes failed", slog.String("repo", repoKey), slog.Any("error", err))
		return nil, nil
	}
	if len(mtimes) == 0 {
		return nil, nil
	}

	if deps.RRFWeights.Recency > 0 {
		recency = buildRecencyHits(candidates, mtimes, rerankCap)
	}

	if deps.RRFWeights.Hotspot > 0 {
		hotspot = buildHotspotHits(ctx, deps, repoKey, root, candidates, mtimes, rerankCap)
	}

	return hotspot, recency
}

// buildRecencyHits ranks candidates by the most recent file modification time
// stored at graph build time. Files with no stored mtime are treated as oldest.
func buildRecencyHits(
	candidates []embeddings.GraphHit,
	mtimes map[string]time.Time,
	rerankCap int,
) []embeddings.GraphHit {
	type scored struct {
		hit   embeddings.GraphHit
		score int64
	}
	items := make([]scored, 0, len(candidates))
	for _, h := range candidates {
		t := mtimes[h.FilePath]
		items = append(items, scored{hit: h, score: t.Unix()})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	if len(items) > rerankCap {
		items = items[:rerankCap]
	}
	out := make([]embeddings.GraphHit, len(items))
	for i, s := range items {
		out[i] = s.hit
	}
	return out
}

// buildHotspotHits ranks candidates by the Omen-style churn x complexity score
// for the file containing the symbol. It requires both git churn (compare) and
// per-symbol complexity stored on the AGE graph. The score is computed on the
// file level because the original Omen research correlates file churn with
// average cyclomatic complexity per file.
func buildHotspotHits(
	ctx context.Context,
	deps SemanticDeps,
	repoKey, root string,
	candidates []embeddings.GraphHit,
	mtimes map[string]time.Time,
	rerankCap int,
) []embeddings.GraphHit {
	churnMap, err := compare.CollectChurn(ctx, root, 0)
	if err != nil {
		slog.Debug("semantic_search: CollectChurn failed", slog.String("repo", repoKey), slog.Any("error", err))
		return nil
	}
	if churnMap == nil {
		return nil
	}

	complexityByFile, err := queryComplexityByFile(ctx, deps, repoKey)
	if err != nil {
		slog.Debug("semantic_search: symbol complexity query failed", slog.String("repo", repoKey), slog.Any("error", err))
		return nil
	}
	if len(complexityByFile) == 0 {
		return nil
	}

	entries := buildHotspotEntries(mtimes, complexityByFile, churnMap)
	if len(entries) == 0 {
		return nil
	}

	scoreByFile := scoreHotspotEntries(entries)
	return rankHotspotCandidates(candidates, scoreByFile, rerankCap)
}

// queryComplexityByFile returns the average cyclomatic complexity per indexed
// file. Symbols without a complexity property are ignored.
func queryComplexityByFile(
	ctx context.Context,
	deps SemanticDeps,
	repoKey string,
) (map[string]float64, error) {
	rows, err := deps.GraphStore.ExecCypher(
		ctx, repoKey,
		"MATCH (s:Symbol) WHERE s.complexity IS NOT NULL RETURN s.file, s.complexity",
		2,
	)
	if err != nil {
		return nil, err
	}

	type acc struct {
		total int
		count int
	}
	byFile := make(map[string]*acc)
	for _, r := range rows {
		if len(r) != 2 {
			continue
		}
		// AGE string agtypes are JSON-quoted ("cmd/eval/main.go"), but integer
		// properties may return bare ("15" or 15). Unquote if possible, fall back
		// to the raw value, then parse the complexity number.
		file, err := strconv.Unquote(r[0])
		if err != nil {
			file = r[0]
		}
		comp, err := strconv.Unquote(r[1])
		if err != nil {
			comp = r[1]
		}
		c, err := strconv.Atoi(comp)
		if err != nil {
			continue
		}
		a := byFile[file]
		if a == nil {
			a = &acc{}
			byFile[file] = a
		}
		a.total += c
		a.count++
	}

	out := make(map[string]float64, len(byFile))
	for file, a := range byFile {
		if a.count == 0 {
			continue
		}
		out[file] = float64(a.total) / float64(a.count)
	}
	return out, nil
}

// hotspotEntry is the file-level churn x complexity pair used by the hotspot arm.
type hotspotEntry struct {
	file       string
	churnScore float64
	complexity float64
}

// buildHotspotEntries filters the file universe to files that have both positive
// churn and positive complexity.
func buildHotspotEntries(
	mtimes map[string]time.Time,
	complexityByFile map[string]float64,
	churnMap map[string]compare.ChurnStats,
) []hotspotEntry {
	var entries []hotspotEntry
	for file := range mtimes {
		complexity, ok := complexityByFile[file]
		if !ok || complexity <= 0 {
			continue
		}
		cs := 0.0
		if c, ok := churnMap[file]; ok {
			cs = c.ChurnScore()
		}
		if cs <= 0 {
			continue
		}
		entries = append(entries, hotspotEntry{file: file, churnScore: cs, complexity: complexity})
	}
	return entries
}

// scoreHotspotEntries computes the Omen-style percentile(churn) x
// percentile(complexity) score per file and returns a map from file to score.
func scoreHotspotEntries(entries []hotspotEntry) map[string]float64 {
	churnValues := make([]float64, len(entries))
	complexityValues := make([]float64, len(entries))
	for i, e := range entries {
		churnValues[i] = e.churnScore
		complexityValues[i] = e.complexity
	}
	sort.Float64s(churnValues)
	sort.Float64s(complexityValues)

	scoreByFile := make(map[string]float64, len(entries))
	for _, e := range entries {
		cp := percentileRank(e.churnScore, churnValues)
		pp := percentileRank(e.complexity, complexityValues)
		scoreByFile[e.file] = cp * pp
	}
	return scoreByFile
}

// rankHotspotCandidates sorts the candidate symbols by their file's hotspot score
// and returns the top rerankCap.
func rankHotspotCandidates(
	candidates []embeddings.GraphHit,
	scoreByFile map[string]float64,
	rerankCap int,
) []embeddings.GraphHit {
	type scoredHit struct {
		hit   embeddings.GraphHit
		score float64
	}
	scored := make([]scoredHit, 0, len(candidates))
	for _, h := range candidates {
		if s, ok := scoreByFile[h.FilePath]; ok {
			scored = append(scored, scoredHit{hit: h, score: s})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if len(scored) > rerankCap {
		scored = scored[:rerankCap]
	}
	out := make([]embeddings.GraphHit, len(scored))
	for i, s := range scored {
		out[i] = s.hit
	}
	return out
}

// percentileRank returns the fraction of values in the sorted slice that are <= val.
// It is the same CDF-style percentile used by compare.ComputeHotspots.
func percentileRank(val float64, sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := sort.Search(n, func(i int) bool { return sorted[i] > val })
	return float64(rank) / float64(n)
}
