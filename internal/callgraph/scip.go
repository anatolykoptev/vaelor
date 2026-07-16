package callgraph

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

const maxSCIPSourceFiles = 2000

// scipCache is a content-addressed cache for SCIP index files.
// Unlike cgCache (which caches the full call graph with a 5min TTL),
// the SCIP cache is keyed by a hash of source file mtimes and persists
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

// trySCIPResolution runs a SCIP indexer for the dominant language and converts
// the resulting index into a typed call graph. Returns nil on any failure.
// Uses a content-addressed cache (scip.Cache) to skip re-indexing when source
// files haven't changed since the last call.
func trySCIPResolution(ctx context.Context, root string, files []*ingest.File, tsSymbols []*parser.Symbol) *CallGraph {
	lang := polyglot.DominantLanguage(files)
	if lang == "" {
		return nil
	}

	cfg, ok := gocodescip.DetectIndexer(lang)
	if !ok {
		slog.Debug("scip: no indexer for language", "lang", lang)
		return nil
	}

	if !gocodescip.IndexerAvailable(cfg.Name) {
		slog.Debug("scip: indexer not in PATH", "indexer", cfg.Name)
		return nil
	}

	srcFiles := ingest.CountSourceFiles(files)
	if srcFiles > maxSCIPSourceFiles {
		slog.Debug("scip: repo too large, skipping", "files", srcFiles, "max", maxSCIPSourceFiles)
		return nil
	}

	// Content-addressed cache: if source files haven't changed, reuse the
	// cached SCIP index instead of re-running the indexer (saves 9-30s on
	// Rust repos, 5-15s on TypeScript repos).
	cacheKey := gocodescip.CacheKey(root)
	if cachedPath, ok := scipCache.Get(cacheKey); ok {
		slog.Info("scip: cache hit", "lang", lang, "root", root, "key", cacheKey)
		idx, err := gocodescip.ReadIndex(cachedPath)
		if err != nil {
			slog.Warn("scip: cached index read failed, re-indexing", "err", err)
		} else {
			typedEdges := gocodescip.ConvertToEdges(idx)
			if len(typedEdges) == 0 {
				slog.Debug("scip: cached index has no typed edges", "documents", idx.DocumentCount())
				return nil
			}
			slog.Info("scip: enhanced (cached)", "lang", lang, "edges", len(typedEdges), "documents", idx.DocumentCount())
			return ConvertToCallGraph(typedEdges, tsSymbols)
		}
	}

	slog.Info("scip: indexing", "lang", lang, "indexer", cfg.Name, "root", root, "files", srcFiles)

	result, err := gocodescip.RunIndexerSafe(ctx, cfg, root)
	if err != nil {
		// Classify: SIGKILL (cgroup OOM or ctx deadline) vs other indexer errors.
		// Both degrade to tree-sitter; the label lets operators correlate "killed"
		// spikes with memory pressure without grepping logs.
		reason := "indexer_error"
		if isKilledErr(err) {
			reason = "killed"
		}
		recordSCIPFallback(cfg.Name, reason)
		slog.Warn("scip: indexer failed", "indexer", cfg.Name, "err", err)
		return nil
	}
	if result.Cleanup != nil {
		defer result.Cleanup()
	}

	idx, err := gocodescip.ReadIndex(result.IndexPath)
	if err != nil {
		recordSCIPFallback(cfg.Name, "read_error")
		slog.Warn("scip: read index failed", "err", err)
		return nil
	}

	// Cache the index for future calls — the index file at result.IndexPath
	// is in a temp dir (or the repo dir); copy it to the persistent cache.
	if err := scipCache.Put(cacheKey, result.IndexPath); err != nil {
		slog.Debug("scip: cache put failed", "err", err)
	} else {
		slog.Info("scip: cached", "key", cacheKey, "path", filepath.Join(scipCacheDir(), cacheKey+".scip"))
	}

	typedEdges := gocodescip.ConvertToEdges(idx)
	if len(typedEdges) == 0 {
		recordSCIPFallback(cfg.Name, "no_edges")
		slog.Debug("scip: no typed edges extracted", "documents", idx.DocumentCount())
		return nil
	}

	slog.Info("scip: enhanced", "lang", lang, "edges", len(typedEdges), "documents", idx.DocumentCount())
	return ConvertToCallGraph(typedEdges, tsSymbols)
}
