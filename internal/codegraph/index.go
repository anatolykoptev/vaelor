package codegraph

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
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
		return cached, nil
	}

	if err := store.EnsureGraph(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure graph: %w", err)
	}

	allFiles, allSymbols, allCalls, err := ingestAndParse(ctx, root)
	if err != nil {
		return nil, err
	}

	cg := callgraph.BuildCallGraph(allSymbols, allCalls)
	vertices, edges := buildGraph(root, allFiles, allSymbols, cg)

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
			log.Printf("codegraph: cross-language vertices: %v", err)
		}
	}
	if len(crossEdges) > 0 {
		if err := insertEdgeBatches(ctx, store, gname, cfg.BatchSize, crossEdges); err != nil {
			log.Printf("codegraph: cross-language edges: %v", err)
		}
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

// checkCache returns existing fresh meta, nil+nil when rebuild is needed, or
// nil+err on a hard failure (e.g. drop failed).
func checkCache(ctx context.Context, store *Store, repoKey, gname string) (*GraphMeta, error) {
	existing, err := getMeta(ctx, store, repoKey)
	if err != nil || existing == nil {
		return nil, nil //nolint:nilerr // missing meta = no cache, not an error
	}
	if isFresh(existing.BuiltAt, existing.TTLSeconds) {
		return existing, nil
	}
	if dropErr := store.DropGraph(ctx, gname, repoKey); dropErr != nil {
		return nil, fmt.Errorf("drop stale graph: %w", dropErr)
	}
	return nil, nil
}

// relPath returns the path of abs relative to root.
// If abs does not have root as prefix, it returns abs unchanged.
func relPath(abs, root string) string {
	if root == "" {
		return abs
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

// maxRoleSampleBytes limits how much source code is read per file for role
// classification.
const maxRoleSampleBytes = 500

// buildCrossLanguageData performs polyglot detection, route extraction, role
// classification, and returns cross-language vertices and edges ready for
// insertion. It also produces BELONGS_TO edges (File -> Layer).
func buildCrossLanguageData(root string, allFiles []*ingest.File) ([]vertexData, []edgeData) {
	structure := polyglot.DetectStructure(allFiles)

	// Extract routes from all applicable files.
	var routeList []routes.Route
	for _, f := range allFiles {
		if f.Language == "" || f.Language == "c" || f.Language == "cpp" {
			continue
		}
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		fileRoutes := routes.ExtractAll(f.Language, src)
		rel := relPath(f.Path, root)
		for i := range fileRoutes {
			fileRoutes[i].File = rel
		}
		routeList = append(routeList, fileRoutes...)
	}

	// Classify layer roles by sampling source snippets.
	layerSources := make(map[string][]string)
	for _, f := range allFiles {
		if f.Language == "" {
			continue
		}
		for i := range structure.Layers {
			l := &structure.Layers[i]
			if matchesLayer(f.RelPath, l.RootDir) {
				src, err := os.ReadFile(f.Path)
				if err == nil {
					limit := maxRoleSampleBytes
					if len(src) < limit {
						limit = len(src)
					}
					layerSources[l.Name] = append(layerSources[l.Name], string(src[:limit]))
				}
				break
			}
		}
	}
	for i := range structure.Layers {
		l := &structure.Layers[i]
		if samples, ok := layerSources[l.Name]; ok {
			l.Role = polyglot.ClassifyLayerRole(samples)
		}
	}

	// Build file -> layer mapping.
	fileToLayer := make(map[string]string)
	for _, f := range allFiles {
		rel := relPath(f.Path, root)
		for _, l := range structure.Layers {
			if matchesLayer(f.RelPath, l.RootDir) {
				fileToLayer[rel] = l.Name
				break
			}
		}
	}

	// Build Layer/Route vertices and HANDLES/FETCHES edges.
	crossVertices, crossEdges := buildCrossLanguageGraph(structure.Layers, routeList, fileToLayer)

	// Append BELONGS_TO edges (File -> Layer).
	for file, layerName := range fileToLayer {
		crossEdges = append(crossEdges, edgeData{
			FromLabel: "File",
			FromKey:   file,
			ToLabel:   "Layer",
			ToKey:     layerName,
			EdgeLabel: "BELONGS_TO",
			Props:     map[string]string{},
		})
	}

	return crossVertices, crossEdges
}

// matchesLayer reports whether a relative file path belongs to the given layer
// root directory.
func matchesLayer(relPath, rootDir string) bool {
	if rootDir == "" || rootDir == "." {
		return true
	}
	return strings.HasPrefix(relPath, rootDir+"/") || relPath == rootDir
}
