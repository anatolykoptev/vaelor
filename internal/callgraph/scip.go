package callgraph

import (
	"context"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

const maxSCIPSourceFiles = 2000

// trySCIPResolution runs a SCIP indexer for the dominant language and converts
// the resulting index into a typed call graph. Returns nil on any failure.
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

	typedEdges := gocodescip.ConvertToEdges(idx)
	if len(typedEdges) == 0 {
		recordSCIPFallback(cfg.Name, "no_edges")
		slog.Debug("scip: no typed edges extracted", "documents", idx.DocumentCount())
		return nil
	}

	slog.Info("scip: enhanced", "lang", lang, "edges", len(typedEdges), "documents", idx.DocumentCount())
	return ConvertToCallGraph(typedEdges, tsSymbols)
}
