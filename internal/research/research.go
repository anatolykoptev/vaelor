package research

import (
	"context"
	"fmt"
	"sort"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// Deps holds optional dependencies for the research pipeline.
// All fields are optional — the pipeline degrades gracefully.
type Deps struct {
	// AnalyzeDeps provides ingest, parse, and BM25 capabilities (required).
	AnalyzeDeps analyze.Deps

	// EmbedClient enables semantic search signal. Optional.
	EmbedClient EmbedClient

	// EmbedStore provides vector similarity search. Optional.
	EmbedStore EmbedStore

	// RepoKey is the embedding store key for this repository. Required when
	// EmbedClient/EmbedStore are set.
	RepoKey string
}

// Run executes the full code-research pipeline:
//  1. BM25F keyword seeds (always)
//  2. Semantic embed seeds (if EmbedClient+EmbedStore available) + RRF fusion
//  3. Import-DAG BFS expansion (±ExpandHops hops)
//  4. Token-budget pruning with distance decay
//  5. Aider-style compact map rendering
func Run(ctx context.Context, input Input, deps Deps) (*Result, error) {
	if input.Root == "" {
		return nil, fmt.Errorf("root is required")
	}
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = DefaultMaxTokens
	}
	if input.ExpandHops <= 0 {
		input.ExpandHops = DefaultExpandHops
	}

	// --- Step 1: parse repo + build BM25F scores ---
	data, err := analyze.AnalyzeForResearch(ctx, input.Root, input.Query, input.Language, input.FileGlob, input.IncludeTests, deps.AnalyzeDeps)
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	// --- Step 2: collect seed scores from fused ranking ---
	seedScores := make(map[string]float64, len(data.FusedScores))
	for relPath, score := range data.FusedScores {
		if score > 0 {
			seedScores[relPath] = score
		}
	}
	mode := "keyword-only"

	// --- Step 3: semantic search (optional) + RRF fusion ---
	var semanticHits []embeddings.SearchResult
	if deps.EmbedClient != nil && deps.EmbedStore != nil && deps.RepoKey != "" {
		hits, semMode := runSemanticSeeds(ctx, input, deps)
		if len(hits) > 0 {
			mode = semMode
			semanticHits = hits

			// Build per-file rank for fusion (highest 1-distance score wins per file).
			semFileScores := make(map[string]float64, len(hits))
			for _, r := range hits {
				s := 1.0 - float64(r.Distance)
				if s > semFileScores[r.FilePath] {
					semFileScores[r.FilePath] = s
				}
			}
			seedScores = fuseScores(seedScores, semFileScores)
		}
	}

	// --- Step 4: build seed set ---
	seedFiles := make(map[string]bool)
	for relPath, score := range seedScores {
		if score > 0 {
			seedFiles[relPath] = true
		}
	}

	// --- Step 5: DAG expansion ---
	expanded := expandFromSeeds(seedFiles, data.FileImports, input.ExpandHops)

	// --- Step 6: filter symbols per file by query terms ---
	filteredSymbols := make(map[string][]*parser.Symbol, len(expanded))
	for _, ex := range expanded {
		syms := data.FileSymbols[ex.relPath]
		filteredSymbols[ex.relPath] = filterSymbolsByQuery(syms, data.QueryTerms)
	}

	// --- Step 7: token-budget pruning ---
	kept, pruned := pruneToTokenBudget(expanded, seedScores, filteredSymbols, input.MaxTokens, input.IncludeBody)

	// --- Step 7b: attach sibling test files (only when IncludeTests=true) ---
	if input.IncludeTests {
		allFilesMap := make(map[string]bool, len(data.Files))
		for _, f := range data.Files {
			allFilesMap[f.RelPath] = true
		}
		kept = linkTestFiles(kept, allFilesMap)

		// Populate filteredSymbols for any newly-added test files so the
		// render/graph stages don't see nil entries.
		for _, sf := range kept {
			if _, ok := filteredSymbols[sf.expand.relPath]; !ok {
				filteredSymbols[sf.expand.relPath] = filterSymbolsByQuery(
					data.FileSymbols[sf.expand.relPath], data.QueryTerms,
				)
			}
		}
	}

	// --- Step 8: render map ---
	codeMap := RenderMap(kept, input.IncludeBody)
	estimatedTokens := estimateMapTokens(codeMap)

	// --- Build result ---
	seeds := buildSeedList(seedFiles, seedScores, data.FileSymbols, data.QueryTerms, semanticHits)
	graph := buildLinkedFiles(kept, data.FileSymbols, data.QueryTerms)

	return &Result{
		Seeds:           seeds,
		Graph:           graph,
		Map:             codeMap,
		EstimatedTokens: estimatedTokens,
		PrunedFiles:     pruned,
		Mode:            mode,
	}, nil
}

// runSemanticSeeds queries the embed store and returns the raw hits.
func runSemanticSeeds(ctx context.Context, input Input, deps Deps) ([]embeddings.SearchResult, string) {
	vec, err := deps.EmbedClient.EmbedQuery(ctx, input.Query)
	if err != nil {
		return nil, "keyword-only"
	}

	results, err := deps.EmbedStore.Search(ctx, vec, embeddings.SearchOpts{
		RepoKey:  deps.RepoKey,
		Language: input.Language,
		TopK:     20,
	})
	if err != nil || len(results) == 0 {
		return nil, "keyword-only"
	}

	return results, "full"
}

// fuseScores combines two score maps using Reciprocal Rank Fusion
// (Cormack et al. 2009, https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf).
// Each input list is sorted by score descending and converted to 1-based ranks;
// the final score is Σ 1/(k + rank_i) with k=60. Accepts nil for either input.
func fuseScores(a, b map[string]float64) map[string]float64 {
	const k = 60.0

	type pair struct {
		path  string
		score float64
	}
	rankList := func(m map[string]float64) []pair {
		out := make([]pair, 0, len(m))
		for p, s := range m {
			out = append(out, pair{p, s})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
		return out
	}

	merged := make(map[string]float64, len(a)+len(b))
	for _, lst := range [][]pair{rankList(a), rankList(b)} {
		for i, p := range lst {
			merged[p.path] += 1.0 / (k + float64(i+1))
		}
	}
	return merged
}

// estimateMapTokens estimates the token count of the rendered map string.
func estimateMapTokens(m string) int {
	return len(m) / charsPerToken
}

// buildSeedList converts seed file map into ordered SeedSymbol slice.
// semanticHits are emitted first (one per hit, preserving SymbolName/Kind/StartLine);
// keyword matches that overlap with a semantic hit on (file, name) are upgraded to "hybrid".
func buildSeedList(
	seedFiles map[string]bool,
	seedScores map[string]float64,
	fileSymbols map[string][]*parser.Symbol,
	queryTerms []string,
	semanticHits []embeddings.SearchResult,
) []SeedSymbol {
	var seeds []SeedSymbol

	// 1) Semantic-derived seeds — one entry per hit, preserves StartLine.
	type semKey struct {
		file string
		name string
	}
	semSeen := make(map[semKey]int) // maps key → index in seeds slice
	for _, h := range semanticHits {
		seeds = append(seeds, SeedSymbol{
			File:   h.FilePath,
			Name:   h.SymbolName,
			Kind:   h.SymbolKind,
			Line:   int(h.StartLine),
			Score:  1.0 - float64(h.Distance),
			Source: "semantic",
		})
		semSeen[semKey{h.FilePath, h.SymbolName}] = len(seeds) - 1
	}

	// 2) Keyword/fused-derived seeds. If the same (file, name) already exists
	//    from a semantic hit, upgrade it to "hybrid" instead of duplicating.
	for relPath := range seedFiles {
		score := seedScores[relPath]
		syms := filterSymbolsByQuery(fileSymbols[relPath], queryTerms)
		if len(syms) == 0 {
			// file-level seed with no resolved symbol
			key := semKey{relPath, ""}
			if _, already := semSeen[key]; !already {
				seeds = append(seeds, SeedSymbol{File: relPath, Score: score, Source: "keyword"})
			}
			continue
		}
		for _, sym := range syms {
			key := semKey{relPath, sym.Name}
			if idx, already := semSeen[key]; already {
				seeds[idx].Source = "hybrid"
				continue
			}
			seeds = append(seeds, SeedSymbol{
				File:   relPath,
				Name:   sym.Name,
				Kind:   string(sym.Kind),
				Line:   int(sym.StartLine),
				Score:  score,
				Source: "keyword",
			})
		}
	}
	return seeds
}

// buildLinkedFiles converts pruned scoredFiles into LinkedFile output.
func buildLinkedFiles(
	kept []scoredFile,
	fileSymbols map[string][]*parser.Symbol,
	queryTerms []string,
) []LinkedFile {
	out := make([]LinkedFile, 0, len(kept))
	for _, sf := range kept {
		syms := filterSymbolsByQuery(fileSymbols[sf.expand.relPath], queryTerms)
		out = append(out, LinkedFile{
			RelPath:   sf.expand.relPath,
			Distance:  sf.expand.distance,
			WhyLinked: sf.expand.whyLinked,
			Symbols:   syms,
			Score:     sf.seedScore,
		})
	}
	return out
}
