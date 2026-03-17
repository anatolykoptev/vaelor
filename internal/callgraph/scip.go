package callgraph

import (
	"context"
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
		return nil
	}

	if !gocodescip.IndexerAvailable(cfg.Name) {
		return nil
	}

	// Guard: skip very large repos (>2000 source files) to avoid OOM.
	if countSourceFiles(files) > maxSCIPSourceFiles {
		return nil
	}

	indexPath, err := gocodescip.RunIndexerSafe(ctx, cfg, root)
	if err != nil {
		return nil
	}
	defer os.Remove(indexPath)

	idx, err := gocodescip.ReadIndex(indexPath)
	if err != nil {
		return nil
	}

	typedEdges := gocodescip.ConvertToEdges(idx)
	if len(typedEdges) == 0 {
		return nil
	}

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
