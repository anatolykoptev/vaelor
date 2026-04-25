package explore

import (
	"path/filepath"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// buildDeadCodeSummary runs dead code analysis and returns a compact summary.
func buildDeadCodeSummary(cg *callgraph.CallGraph) *DeadCodeSummary {
	dcResult := deadcode.Analyze(cg, deadcode.Options{
		Relationships: cg.TypeRels,
	})
	if dcResult.DeadCount == 0 {
		return nil
	}
	samples := make([]string, 0, maxDeadCodeSamples)
	for _, ds := range dcResult.DeadSymbols {
		if len(samples) >= maxDeadCodeSamples {
			break
		}
		// Skip minified symbols (single/double char names from minified JS).
		if len(ds.Name) <= 2 {
			continue
		}
		// Skip symbols from compiled artifact paths.
		if compare.IsCompiledArtifact(ds.File) {
			continue
		}
		samples = append(samples, ds.Name)
	}
	return &DeadCodeSummary{
		Count:   dcResult.DeadCount,
		Samples: samples,
	}
}

// buildLanguageStats computes per-language file counts and ratios.
func buildLanguageStats(files []*ingest.File) []LanguageStat {
	langFiles := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			langFiles[f.Language]++
		}
	}

	fileCount := len(files)
	langStats := make([]LanguageStat, 0, len(langFiles))
	for name, count := range langFiles {
		var ratio float64
		if fileCount > 0 {
			ratio = float64(count) / float64(fileCount)
		}
		langStats = append(langStats, LanguageStat{
			Name:  name,
			Files: count,
			Ratio: ratio,
		})
	}
	sort.Slice(langStats, func(i, j int) bool {
		return langStats[i].Files > langStats[j].Files
	})
	return langStats
}

// buildTopSymbols returns the top symbols sorted by call count descending.
func buildTopSymbols(symbols []*parser.Symbol, callCounts map[*parser.Symbol]int, root string) []SymbolSummary {
	type entry struct {
		sym   *parser.Symbol
		count int
	}

	var entries []entry
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		count := callCounts[sym]
		if count == 0 {
			continue
		}
		entries = append(entries, entry{sym: sym, count: count})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	limit := maxTopSymbols
	if len(entries) < limit {
		limit = len(entries)
	}

	result := make([]SymbolSummary, limit)
	for i := range limit {
		e := entries[i]
		file := e.sym.File
		if rel, err := filepath.Rel(root, file); err == nil {
			file = rel
		}
		result[i] = SymbolSummary{
			Name:      e.sym.Name,
			Kind:      string(e.sym.Kind),
			File:      file,
			CallCount: e.count,
		}
	}
	return result
}

// buildPackageList collects unique directory paths relative to root.
func buildPackageList(files []*ingest.File, root string) []string {
	seen := make(map[string]struct{})
	for _, f := range files {
		dir := filepath.Dir(f.Path)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			rel = dir
		}
		seen[rel] = struct{}{}
	}

	pkgs := make([]string, 0, len(seen))
	for p := range seen {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return pkgs
}
