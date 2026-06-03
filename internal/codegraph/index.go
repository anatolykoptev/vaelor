package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
)

const (
	defaultTTLLocal   = 3600
	defaultTTLRemote  = 86400
	defaultBatchSize  = 500
	maxIndexFileBytes = 512 * 1024
)

// IndexConfig controls caching behaviour for IndexRepo.
type IndexConfig struct {
	TTLLocal  int
	TTLRemote int
	BatchSize int

	// EnableSurpriseIndex triggers the two-phase surprise persistence pass
	// (IndexSurpriseEdges + IndexSurpriseNodes) after the pagerank/community
	// pass.  Gated behind CODEGRAPH_SURPRISE_INDEX=1 (default off).
	EnableSurpriseIndex bool
}

// GraphMeta describes a built code graph stored in code_graph_meta.
type GraphMeta struct {
	RepoKey     string    `json:"repo_key"`
	RepoPath    string    `json:"repo_path"`
	GraphName   string    `json:"graph_name"`
	FileCount   int       `json:"file_count"`
	SymbolCount int       `json:"symbol_count"`
	EdgeCount   int       `json:"edge_count"`
	BuiltAt     time.Time `json:"built_at"`
	TTLSeconds  int       `json:"ttl_seconds"`
}

// vertexData holds label and properties for one graph vertex.
type vertexData struct {
	Label string
	Props map[string]string
}

// edgeData holds all fields needed to express one directed graph edge.
type edgeData struct {
	FromLabel string
	FromKey   string
	ToLabel   string
	ToKey     string
	EdgeLabel string
	Props     map[string]string
}

// IndexRepo builds (or returns cached) a code graph for the repo at root.
func IndexRepo(ctx context.Context, store *Store, root string, isRemote bool, cfg IndexConfig) (*GraphMeta, error) {
	cfg = applyConfigDefaults(cfg)
	repoKey := graphName(root)
	gname := repoKey
	t0 := time.Now()

	// Deferred outcome recorder: bumps graphBuildTotal{repo, status} exactly once
	// per return path.  The variable is set to "error" by default and overridden to
	// "skip" (cache-fresh) or "ok" (successful build+persist) at the appropriate
	// terminal.  This avoids a double-bump when multiple error returns exist.
	buildStatus := "error"
	defer func() { recordGraphBuild(repoKey, buildStatus) }()

	if cached, err := checkCache(ctx, store, repoKey, gname); err != nil {
		return nil, err
	} else if cached != nil {
		buildStatus = "skip"
		if ierr := store.EnsureIndexes(ctx, gname); ierr != nil {
			slog.Warn("codegraph: ensure indexes on cache hit", slog.Any("error", ierr))
		}
		return cached, nil
	}

	if err := store.EnsureGraph(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure graph: %w", err)
	}
	if err := store.EnsureLabels(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure labels: %w", err)
	}

	// Create vertex indexes before inserts so UNWIND+MERGE uses index-based lookup
	// rather than full table scans. Creating indexes on empty tables is instant;
	// PostgreSQL maintains them incrementally during inserts.
	t_idx := time.Now()
	if err := store.EnsureIndexes(ctx, gname); err != nil {
		slog.Warn("codegraph: pre-insert EnsureIndexes", slog.Any("error", err))
	}
	slog.Info("codegraph: pre-insert EnsureIndexes done",
		slog.String("repo", root), slog.Duration("elapsed", time.Since(t_idx)))

	t1 := time.Now()
	allFiles, allSymbols, allCalls, fileImports, allRels, allTplRefs, skippedReasons, err := ingestAndParse(ctx, root)
	if err != nil {
		return nil, err
	}
	slog.Info("codegraph: ingestAndParse done",
		slog.Any("skipped_reasons", skippedReasons),
		slog.String("repo", root), slog.Int("files", len(allFiles)),
		slog.Duration("elapsed", time.Since(t1)))

	t2 := time.Now()
	cg := callgraph.BuildCallGraph(allSymbols, allCalls)
	hookRoutes := extractHookRoutes(root, allFiles)
	if len(hookRoutes) > 0 {
		callgraph.InjectHookEdges(cg, hookRoutes)
	}
	vertices, edges := buildGraph(buildGraphInput{
		Root: root, Files: allFiles, Symbols: allSymbols,
		CallGraph: cg, FileImports: fileImports, Rels: allRels, TplRefs: allTplRefs,
	})
	injectCommunities(vertices, edges)
	slog.Info("codegraph: buildGraph done",
		slog.String("repo", root), slog.Int("vertices", len(vertices)), slog.Int("edges", len(edges)),
		slog.Duration("elapsed", time.Since(t2)))

	// Phase 2: extract named execution flows (index-time precompute).
	// Runs after injectCommunities and computeSymbolPageRank (called inside buildGraph).
	// No AGE I/O — pure in-memory DFS over cg. Non-fatal: logs error and bumps counter.
	prScores := computeSymbolPageRank(root, allSymbols, cg)
	communityMap := buildCommunityMap(vertices)
	crossVerticesEarly, crossEdgesEarly := buildCrossLanguageData(root, allFiles, allSymbols)
	handlesTargets := extractHandlesTargets(root, crossEdgesEarly)
	{
		t_flows := time.Now()
		flows := ExtractFlows(root, cg, communityMap, prScores, handlesTargets)
		flowsExtractedTotal.WithLabelValues(repoKey).Add(float64(len(flows)))
		slog.Info("codegraph: flow extraction done",
			slog.String("repo", root), slog.Int("flows", len(flows)),
			slog.Duration("elapsed", time.Since(t_flows)))
		if len(flows) > 0 {
			if uErr := store.UpsertFlows(ctx, repoKey, flows); uErr != nil {
				slog.Warn("codegraph: flow upsert failed (non-fatal)",
					slog.String("repo", root), slog.Any("error", uErr))
				flowsDBErrorTotal.WithLabelValues(repoKey).Inc()
			}
		}
		flowsExtractDuration.WithLabelValues(repoKey).Observe(time.Since(t_flows).Seconds())
	}
	// Use the early cross-language data for the main insert pass.
	crossVertices, crossEdges := crossVerticesEarly, crossEdgesEarly

	// Open a bulk write session (synchronous_commit=off, single connection).
	// 5x faster than per-call pool acquire. Falls back to Store on error.
	var writer CypherWriter = store
	bw, bwErr := store.NewBulkWriter(ctx)
	if bwErr != nil {
		slog.Warn("codegraph: BulkWriter unavailable, using standard writes", slog.Any("error", bwErr))
	} else {
		defer bw.Close(ctx)
		writer = bw
	}

	t3 := time.Now()
	// Attempt direct COPY INSERT (bypasses Cypher parser, 10-20x faster).
	// Falls back to UNWIND inserts on any error.
	if err := store.BulkCopyInsert(ctx, gname, vertices, edges); err != nil {
		slog.Warn("codegraph: BulkCopyInsert failed, falling back to UNWIND inserts",
			slog.String("repo", root), slog.Any("error", err))
		if fbErr := insertBatches(ctx, writer, gname, cfg.BatchSize, vertices, buildVertexBatch); fbErr != nil {
			return nil, fmt.Errorf("insert vertices (fallback): %w", fbErr)
		}
		if fbErr := insertEdgeBatches(ctx, writer, gname, cfg.BatchSize, edges); fbErr != nil {
			return nil, fmt.Errorf("insert edges (fallback): %w", fbErr)
		}
	}
	slog.Info("codegraph: insert done",
		slog.String("repo", root),
		slog.Int("vertices", len(vertices)), slog.Int("edges", len(edges)),
		slog.Duration("elapsed", time.Since(t3)))

	// Cross-language analysis (non-fatal). Data already computed above for flow
	// extraction; reuse the hoisted crossVertices/crossEdges variables.
	if len(crossVertices) > 0 {
		if err := insertBatches(ctx, writer, gname, cfg.BatchSize, crossVertices, buildVertexBatch); err != nil {
			slog.Warn("codegraph: cross-language vertices", slog.Any("error", err))
		}
	}
	if len(crossEdges) > 0 {
		if err := insertEdgeBatches(ctx, writer, gname, cfg.BatchSize, crossEdges); err != nil {
			slog.Warn("codegraph: cross-language edges", slog.Any("error", err))
		}
	}

	// Indexes already created before edge inserts; run again as IF NOT EXISTS
	// for cross-language vertices added after the main pass.
	if len(crossVertices) > 0 {
		if err := store.EnsureIndexes(ctx, gname); err != nil {
			slog.Warn("codegraph: post-cross EnsureIndexes", slog.Any("error", err))
		}
	}

	ttl := cfg.TTLLocal
	if isRemote {
		ttl = cfg.TTLRemote
	}
	meta := &GraphMeta{
		RepoKey: repoKey, RepoPath: root, GraphName: gname,
		FileCount: len(allFiles), SymbolCount: len(allSymbols), EdgeCount: len(edges),
		BuiltAt: time.Now().UTC(), TTLSeconds: ttl,
	}
	if err := upsertMeta(ctx, store, meta); err != nil {
		return nil, fmt.Errorf("upsert meta: %w", err)
	}

	t6 := time.Now()
	storeFileMtimes(ctx, store, repoKey, allFiles)
	slog.Info("codegraph: storeFileMtimes done",
		slog.String("repo", root), slog.Int("files", len(allFiles)),
		slog.Duration("elapsed", time.Since(t6)))

	// Pre-score dead_code candidates so query-time reranking is instant.
	// Non-fatal: errors are logged but do not fail IndexRepo.
	t7 := time.Now()
	if scoreErr := store.ScoreDeadCodeCandidates(ctx, gname, repoKey, len(allSymbols)); scoreErr != nil {
		slog.Warn("codegraph: dead_code pre-scoring failed (non-fatal)",
			slog.String("repo", root), slog.Any("error", scoreErr))
	} else {
		slog.Info("codegraph: dead_code pre-scoring done",
			slog.String("repo", root), slog.Duration("elapsed", time.Since(t7)))
	}

	slog.Info("codegraph: IndexRepo complete",
		slog.String("repo", root), slog.Duration("total", time.Since(t0)))

	if cfg.EnableSurpriseIndex {
		if err := IndexSurpriseEdges(ctx, store, gname); err != nil {
			slog.Warn("codegraph: surprise edge index failed", slog.Any("error", err))
		} else if err := IndexSurpriseNodes(ctx, store, gname); err != nil {
			slog.Warn("codegraph: surprise node index failed", slog.Any("error", err))
		}
	}

	buildStatus = "ok"
	return meta, nil
}

// applyConfigDefaults fills in zero-value fields with sensible defaults.
func applyConfigDefaults(cfg IndexConfig) IndexConfig {
	if cfg.TTLLocal <= 0 {
		cfg.TTLLocal = defaultTTLLocal
	}
	if cfg.TTLRemote <= 0 {
		cfg.TTLRemote = defaultTTLRemote
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	return cfg
}
