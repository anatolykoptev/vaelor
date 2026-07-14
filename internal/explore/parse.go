package explore

import (
	"context"
	"os"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// parseAllFiles parses all ingested files, collecting symbols, calls, imports, and line counts.
func parseAllFiles(ctx context.Context, files []*ingest.File) (*parseResults, error) {
	result := parseResults{imports: make(map[string][]string, len(files))}
	for _, f := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		source, readErr := os.ReadFile(f.Path)
		if readErr != nil {
			continue
		}

		result.totalLines += goutil.CountLines(source)

		opts := parser.ParseOpts{
			Language:       f.Language,
			IncludeBody:    false,
			IncludeImports: true,
		}

		// Single parse for symbols+calls instead of ParseFile + ExtractCalls (issue #400).
		pr, calls, parseErr := parser.ParseFileWithCalls(f.Path, source, opts)
		if parseErr != nil {
			continue
		}
		result.symbols = append(result.symbols, pr.Symbols...)
		if len(pr.Imports) > 0 {
			result.imports[f.Path] = pr.Imports
		}
		result.calls = append(result.calls, calls...)
	}
	return &result, nil
}

// countIncomingCalls returns a map of symbol to its incoming call count.
func countIncomingCalls(cg *callgraph.CallGraph) map[*parser.Symbol]int {
	callCounts := make(map[*parser.Symbol]int)
	for _, edge := range cg.Edges {
		if edge.Callee != nil {
			callCounts[edge.Callee]++
		}
	}
	return callCounts
}
