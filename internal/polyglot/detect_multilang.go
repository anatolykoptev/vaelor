package polyglot

import (
	"sort"

	"github.com/anatolykoptev/vaelor/internal/ingest"
)

// minLangFiles is the minimum number of source files a language must have
// to be considered for SCIP indexing. Below this threshold the language is
// too small to justify running an indexer (e.g. a few .py scripts in a
// mostly-Rust repo).
const minLangFiles = 3

// DetectedLanguages returns all languages with at least minLangFiles source
// files, sorted by count descending. This is the multi-language counterpart
// to DominantLanguage — use it when you need to run per-language indexers
// (SCIP) across all significant languages in a polyglot repo.
func DetectedLanguages(files []*ingest.File) []string {
	counts := make(map[string]int)
	for _, f := range files {
		if f.Language != "" {
			counts[f.Language]++
		}
	}
	return DetectedLanguagesFromCounts(counts)
}

// DetectedLanguagesFromCounts returns languages with count >= minLangFiles,
// sorted by count descending. Ties are broken alphabetically for
// deterministic output.
func DetectedLanguagesFromCounts(counts map[string]int) []string {
	type langCount struct {
		lang  string
		count int
	}
	var lcs []langCount
	for lang, count := range counts {
		if count >= minLangFiles {
			lcs = append(lcs, langCount{lang: lang, count: count})
		}
	}
	sort.Slice(lcs, func(i, j int) bool {
		if lcs[i].count != lcs[j].count {
			return lcs[i].count > lcs[j].count
		}
		return lcs[i].lang < lcs[j].lang
	})
	result := make([]string, len(lcs))
	for i, lc := range lcs {
		result[i] = lc.lang
	}
	return result
}
