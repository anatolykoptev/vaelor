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
	TTLLocal  int // seconds, default 3600
	TTLRemote int // seconds, default 86400
	BatchSize int // vertices per Cypher batch, default 500

	// EnableSurpriseIndex triggers the two-phase surprise persistence pass
	// (IndexSurpriseEdges + IndexSurpriseNodes) after the pagerank/community
	// pass.  Gated behind CODEGRAPH_SURPRISE_INDEX=1 (default off).
	// Failures in the surprise pass are non-fatal — they log a warning and
	// IndexRepo continues normally.
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
//
// If the graph exists and is not stale it returns the cached GraphMeta immediately.
// Otherwise it drops any stale graph, rebuilds it from scratch, and persists GraphMeta.
func IndexRepo(ctx context.Context, store *Store, root string, isRemote bool, cfg IndexConfig) (*GraphMeta, error) {
	cfg = applyConfigDefaults(cfg)

	repoKey := graphName(root)
	gname := repoKey

	if cached, err := checkCache(ctx, store, repoKey, gname); err != nil {
		return nil, err
	} else if cached != nil {
		// Ensure indexes exist even on cache hit — safe to run (IF NOT EXISTS).
		// Handles existing graphs that were built before indexes were introduced.
		if ierr := store.EnsureIndexes(ctx, gname); ierr != nil {
			slog.Warn("codegraph: ensure indexes on cache hit", slog.Any("error", ierr))
		}
		return cached, nil
	}

	if err := store.EnsureGraph(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure graph: %w", err)
	}

	allFiles, allSymbols, allCalls, fileImports, allRels, allTplRefs, err := ingestAndParse(ctx, root)
	if err != nil {
		return nil, err
	}

	cg := callgraph.BuildCallGraph(allSymbols, allCalls)

	// Inject WordPress hook edges before building the graph so they appear
	// as regular CALLS edges in code_graph.
	hookRoutes := extractHookRoutes(root, allFiles)
	if len(hookRoutes) > 0 {
		callgraph.InjectHookEdges(cg, hookRoutes)
	}

	vertices, edges := buildGraph(buildGraphInput{
		Root:        root,
		Files:       allFiles,
		Symbols:     allSymbols,
		CallGraph:   cg,
		FileImports: fileImports,
		Rels:        allRels,
		TplRefs:     allTplRefs,
	})

	// Compute communities and inject into Symbol vertices before persisting.
	injectCommunities(vertices, edges)

	if err := insertBatches(ctx, store, gname, cfg.BatchSize, vertices, buildVertexBatch); err != nil {
		return nil, fmt.Errorf("insert vertices: %w", err)
	}

	if err := insertEdgeBatches(ctx, store, gname, cfg.BatchSize, edges); err != nil {
		return nil, fmt.Errorf("insert edges: %w", err)
	}

	// --- Cross-language analysis ---
	crossVertices, crossEdges := buildCrossLanguageData(root, allFiles)
	if len(crossVertices) > 0 {
		if err := insertBatches(ctx, store, gname, cfg.BatchSize, crossVertices, buildVertexBatch); err != nil {
			slog.Warn("codegraph: cross-language vertices", slog.Any("error", err))
		}
	}
	if len(crossEdges) > 0 {
		if err := insertEdgeBatches(ctx, store, gname, cfg.BatchSize, crossEdges); err != nil {
			slog.Warn("codegraph: cross-language edges", slog.Any("error", err))
		}
	}

	// Create Postgres indexes on AGE vertex tables for fast WHERE filtering.
	// Non-fatal: index failures log but don't block graph availability.
	if err := store.EnsureIndexes(ctx, gname); err != nil {
		slog.Warn("codegraph: ensure indexes", slog.Any("error", err))
	}

	ttl := cfg.TTLLocal
	if isRemote {
		ttl = cfg.TTLRemote
	}

	meta := &GraphMeta{
		RepoKey:     repoKey,
		RepoPath:    root,
		GraphName:   gname,
		FileCount:   len(allFiles),
		SymbolCount: len(allSymbols),
		EdgeCount:   len(edges),
		BuiltAt:     time.Now().UTC(),
		TTLSeconds:  ttl,
	}

	if err := upsertMeta(ctx, store, meta); err != nil {
		return nil, fmt.Errorf("upsert meta: %w", err)
	}

	// Store file mtimes for future incremental updates.
	storeFileMtimes(ctx, store, repoKey, allFiles)

	// Optional surprise persistence pass — gated behind EnableSurpriseIndex.
	// Errors are non-fatal: log a warning and continue so IndexRepo never fails
	// because of the surprise pass.
	if cfg.EnableSurpriseIndex {
		if err := IndexSurpriseEdges(ctx, store, gname); err != nil {
			slog.Warn("codegraph: surprise edge index failed", slog.Any("error", err))
		} else if err := IndexSurpriseNodes(ctx, store, gname); err != nil {
			slog.Warn("codegraph: surprise node index failed", slog.Any("error", err))
		}
	}

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
