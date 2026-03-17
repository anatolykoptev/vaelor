package callgraph

import (
	"context"
	"log/slog"
	"os"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

const maxSCIPSourceFiles = 2000

// trySCIPResolution runs a SCIP indexer for the dominant language and converts
// the resulting index into a typed call graph. Returns nil on any failure.
func trySCIPResolution(ctx context.Context, root string, files []*ingest.File, tsSymbols []*parser.Symbol) *CallGraph {
	lang := dominantLang(files)
	if lang == "" || lang == "go" {
		return nil // Go already handled by go/types
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

	srcFiles := countSourceFiles(files)
	if srcFiles > maxSCIPSourceFiles {
		slog.Debug("scip: repo too large, skipping", "files", srcFiles, "max", maxSCIPSourceFiles)
		return nil
	}

	slog.Info("scip: indexing", "lang", lang, "indexer", cfg.Name, "root", root, "files", srcFiles)

	indexPath, err := gocodescip.RunIndexerSafe(ctx, cfg, root)
	if err != nil {
		slog.Warn("scip: indexer failed", "indexer", cfg.Name, "err", err)
		return nil
	}
	defer os.Remove(indexPath)

	idx, err := gocodescip.ReadIndex(indexPath)
	if err != nil {
		slog.Warn("scip: read index failed", "err", err)
		return nil
	}

	typedEdges := gocodescip.ConvertToEdges(idx)
	if len(typedEdges) == 0 {
		slog.Debug("scip: no typed edges extracted", "documents", idx.DocumentCount())
		return nil
	}

	slog.Info("scip: enhanced", "lang", lang, "edges", len(typedEdges), "documents", idx.DocumentCount())
	return ConvertToCallGraph(typedEdges, tsSymbols)
}

// dominantLang returns the most common language among the given files.
func dominantLang(files []*ingest.File) string {
	counts := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			counts[f.Language]++
		}
	}
	best := ""
	bestN := 0
	for lang, n := range counts {
		if n > bestN {
			best = lang
			bestN = n
		}
	}
	return best
}

// countSourceFiles returns the number of files with a detected language.
func countSourceFiles(files []*ingest.File) int {
	n := 0
	for _, f := range files {
		if f.Language != "" {
			n++
		}
	}
	return n
}
