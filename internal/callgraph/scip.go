package callgraph

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

const maxSCIPSourceFiles = 2000

// scipCache is a content-addressed cache for SCIP index files.
// Unlike cgCache (which caches the full call graph with a 5min TTL),
// the SCIP cache is keyed by a hash of source file content and persists
// across call_trace invocations with different depth/direction/focus
// parameters — the index only changes when source files change.
var scipCache = gocodescip.NewCache(scipCacheDir())

// scipCacheDir returns the directory for cached SCIP index files.
// Uses $SCIP_CACHE_DIR if set, otherwise /tmp/scip-cache.
func scipCacheDir() string {
	if d := os.Getenv("SCIP_CACHE_DIR"); d != "" {
		return d
	}
	return "/tmp/scip-cache"
}

// trySCIPResolution runs SCIP indexers for ALL detected languages (not just
// the dominant one) and converts the resulting indices into a typed call
// graph. Returns nil on any failure (graceful degradation to tree-sitter).
//
// Multi-language: iterates polyglot.DetectedLanguages, runs the appropriate
// indexer for each, and merges all typed edges into a single CallGraph.
// This ensures polyglot monorepos (Rust+TS, Go+Python) get typed edges for
// all significant languages, not just the majority one.
func trySCIPResolution(ctx context.Context, root string, files []*ingest.File, tsSymbols []*parser.Symbol) *CallGraph {
	langs := polyglot.DetectedLanguages(files)
	if len(langs) == 0 {
		return nil
	}

	srcFiles := ingest.CountSourceFiles(files)
	if srcFiles > maxSCIPSourceFiles {
		slog.Debug("scip: repo too large, skipping", "files", srcFiles, "max", maxSCIPSourceFiles)
		return nil
	}

	// Collect typed edges from all languages.
	var allTypedEdges []goanalysis.TypedEdge
	anyIndexed := false

	for _, lang := range langs {
		cfg, ok := gocodescip.DetectIndexer(lang)
		if !ok {
			slog.Debug("scip: no indexer for language", "lang", lang)
			continue
		}

		if !gocodescip.IndexerAvailable(cfg.Name) {
			slog.Debug("scip: indexer not in PATH", "indexer", cfg.Name, "lang", lang)
			continue
		}

		typedEdges, ok := indexOneLanguage(ctx, cfg, lang, root)
		if !ok {
			continue
		}
		anyIndexed = true
		allTypedEdges = append(allTypedEdges, typedEdges...)
	}

	if !anyIndexed || len(allTypedEdges) == 0 {
		return nil
	}

	slog.Info("scip: enhanced (multi-language)",
		"languages", langs,
		"edges", len(allTypedEdges))

	return ConvertToCallGraph(allTypedEdges, tsSymbols)
}

// indexOneLanguage runs the SCIP indexer for a single language, using the
// content-addressed cache to skip re-indexing when source files haven't
// changed. Returns the typed edges and true on success, nil and false on
// any failure.
func indexOneLanguage(ctx context.Context, cfg gocodescip.IndexerConfig, lang, root string) ([]goanalysis.TypedEdge, bool) {
	// Content-addressed cache: keyed by language + content hash so each
	// language gets its own cache entry (rust-analyzer and scip-typescript
	// produce different indices for the same repo).
	cacheKey := lang + ":" + gocodescip.CacheKey(root)
	if cachedPath, ok := scipCache.Get(cacheKey); ok {
		slog.Info("scip: cache hit", "lang", lang, "root", root, "key", cacheKey)
		idx, err := gocodescip.ReadIndex(cachedPath)
		if err != nil {
			slog.Warn("scip: cached index read failed, re-indexing", "err", err)
		} else {
			typedEdges := gocodescip.ConvertToEdges(idx)
			if len(typedEdges) == 0 {
				slog.Debug("scip: cached index has no typed edges", "documents", idx.DocumentCount())
				return nil, false
			}
			slog.Info("scip: enhanced (cached)", "lang", lang, "edges", len(typedEdges), "documents", idx.DocumentCount())
			return typedEdges, true
		}
	}

	slog.Info("scip: indexing", "lang", lang, "indexer", cfg.Name, "root", root)

	result, err := gocodescip.RunIndexerSafe(ctx, cfg, root)
	if err != nil {
		reason := "indexer_error"
		if isKilledErr(err) {
			reason = "killed"
		}
		recordSCIPFallback(cfg.Name, reason)
		slog.Warn("scip: indexer failed", "indexer", cfg.Name, "err", err)
		return nil, false
	}
	if result.Cleanup != nil {
		defer result.Cleanup()
	}

	idx, err := gocodescip.ReadIndex(result.IndexPath)
	if err != nil {
		recordSCIPFallback(cfg.Name, "read_error")
		slog.Warn("scip: read index failed", "err", err)
		return nil, false
	}

	// Cache the index for future calls.
	if err := scipCache.Put(cacheKey, result.IndexPath); err != nil {
		slog.Debug("scip: cache put failed", "err", err)
	} else {
		slog.Info("scip: cached", "key", cacheKey, "path", filepath.Join(scipCacheDir(), cacheKey+".scip"))
	}

	typedEdges := gocodescip.ConvertToEdges(idx)
	if len(typedEdges) == 0 {
		recordSCIPFallback(cfg.Name, "no_edges")
		slog.Debug("scip: no typed edges extracted", "documents", idx.DocumentCount())
		return nil, false
	}

	slog.Info("scip: enhanced", "lang", lang, "edges", len(typedEdges), "documents", idx.DocumentCount())
	return typedEdges, true
}
