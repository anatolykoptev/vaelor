package analyze

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/goutil"
	"github.com/anatolykoptev/go-code/internal/importresolve"
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// Depth level constants.
const (
	DepthOverview = "overview"
	DepthModule   = "module"
	DepthDeep     = "deep"
)

// NormalizeDepth maps common LLM aliases to canonical depth values.
// Returns the canonical value and true, or ("", false) if unrecognized.
func NormalizeDepth(d string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", DepthOverview, DepthModule, DepthDeep:
		return d, true
	case "shallow", "quick", "brief", "compact", "light", "summary":
		return DepthOverview, true
	case "medium", "balanced", "standard", "normal", "default":
		return DepthModule, true
	case "full", "detailed", "complete", "all", "thorough", "maximum":
		return DepthDeep, true
	default:
		return "", false
	}
}

// ValidDepth reports whether d is a recognized analysis depth.
func ValidDepth(d string) bool {
	_, ok := NormalizeDepth(d)
	return ok
}

// ContextData holds mechanically-extracted analysis data: ranking, import graph,
// scores, and file metadata. Consumed directly by the XML output layer.
type ContextData struct {
	RankedFiles  []*ingest.File     // files ordered by relevance
	FileScores   map[string]float64 // relPath → BM25F+PageRank combined score
	ImportGraph  importGraph        // package-level import adjacency
	ImportedBy   map[string]int     // relPath → imported-by count
	SymbolCounts map[string]int     // relPath → symbol count
	FileTree     string             // rendered directory tree
	QueryTerms   []string           // extracted search terms
	Languages    map[string]int     // language → file count
}

// buildContextData extracts ranking, import graph, scores, and other
// mechanical analysis data from ingest + parse results.
func buildContextData(ir *ingest.IngestResult, results []fileParseResult, query string) *ContextData {
	queryTerms := extractQueryTerms(query)
	rankedFiles, fileScores := prioritizeFilesWithScores(ir.Root, ir.Files, results, queryTerms)
	ig := buildImportGraph(ir.Root, results, false)
	importedBy := computeImportedByCounts(ir.Root, results)
	symbolCounts := computeSymbolCounts(results)
	fileTree := ingest.RenderTree(ir.Files)

	languages := make(map[string]int)
	for _, f := range ir.Files {
		if f.Language != "" {
			languages[f.Language]++
		}
	}

	return &ContextData{
		RankedFiles:  rankedFiles,
		FileScores:   fileScores,
		ImportGraph:  ig,
		ImportedBy:   importedBy,
		SymbolCounts: symbolCounts,
		FileTree:     fileTree,
		QueryTerms:   queryTerms,
		Languages:    languages,
	}
}

// computeSymbolCounts returns the number of symbols in each file (by RelPath).
func computeSymbolCounts(results []fileParseResult) map[string]int {
	counts := make(map[string]int)
	for _, pr := range results {
		if pr.result != nil {
			counts[pr.file.RelPath] = len(pr.result.Symbols)
		}
	}
	return counts
}

// computeImportedByCounts returns how many packages import the package of each file.
// Uses the package-level import graph with the shared importresolve.Resolver for
// import resolution. This handles both Go-style suffix matches and TS/JS-style
// relative imports ("./x", "../x") that the old suffix-only implementation missed.
func computeImportedByCounts(root string, results []fileParseResult) map[string]int {
	pkgGraph := buildImportGraph(root, results, false)

	// Collect local package dirs (fileSet is empty — analyze works at pkg granularity).
	pkgDirs := make(map[string]struct{})
	for _, pr := range results {
		pkgDirs[goutil.PackageDir(root, pr.file.Path)] = struct{}{}
	}
	r := importresolve.New(pkgDirs, nil, importresolve.Config{})

	// Build reverse index: for each local package, how many packages import it.
	// Use importingPkg (the key) so that relative imports resolve against the
	// correct importing directory — the old loop discarded the key, making relative
	// resolution impossible.
	pkgImportedBy := make(map[string]int)
	for importingPkg, deps := range pkgGraph {
		for dep := range deps {
			if resolved, ok := r.Resolve(dep, importingPkg); ok {
				pkgImportedBy[resolved]++
			}
		}
	}

	// Map to files: each file gets its package's imported-by count.
	counts := make(map[string]int)
	for _, pr := range results {
		pkg := goutil.PackageDir(root, pr.file.Path)
		if n := pkgImportedBy[pkg]; n > 0 {
			counts[pr.file.RelPath] = n
		}
	}
	return counts
}

// nonAlphanumRe matches characters that are not letters, digits, or underscores.
var nonAlphanumRe = regexp.MustCompile(`[^\w]`)

// splitCamelCase splits a camelCase or PascalCase identifier into lowercase subwords.
func splitCamelCase(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	runes := []rune(s)
	start := 0

	for i := 1; i < len(runes); i++ {
		if isCamelBoundary(runes, i) {
			part := strings.ToLower(string(runes[start:i]))
			if len(part) >= 2 { //nolint:mnd // minimum subword length
				parts = append(parts, part)
			}
			start = i
		}
	}

	if start < len(runes) {
		part := strings.ToLower(string(runes[start:]))
		if len(part) >= 2 { //nolint:mnd // minimum subword length
			parts = append(parts, part)
		}
	}

	return parts
}

// isCamelBoundary returns true if position i in runes is a camelCase split point.
func isCamelBoundary(runes []rune, i int) bool {
	prev, cur := runes[i-1], runes[i]

	if unicode.IsLower(prev) && unicode.IsUpper(cur) {
		return true
	}
	if unicode.IsLetter(prev) && unicode.IsDigit(cur) {
		return true
	}
	if unicode.IsDigit(prev) && unicode.IsLetter(cur) {
		return true
	}
	if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
		return true
	}
	return false
}

// splitIdentifier splits an identifier on underscores, then splits each part by camelCase.
func splitIdentifier(s string) []string {
	snakeParts := strings.Split(s, "_")
	var result []string

	for _, part := range snakeParts {
		if part == "" {
			continue
		}
		camelParts := splitCamelCase(part)
		result = append(result, camelParts...)
	}

	return result
}

// extractQueryTerms splits the query into lowercase alphanumeric terms for matching.
func extractQueryTerms(query string) []string {
	seen := make(map[string]struct{})
	var terms []string

	addTerm := func(t string) {
		if _, ok := seen[t]; !ok && len(t) >= 3 { //nolint:mnd // minimum term length to avoid noise
			seen[t] = struct{}{}
			terms = append(terms, t)
		}
	}

	rawWords := strings.Fields(query)

	for _, raw := range rawWords {
		lower := strings.ToLower(raw)
		cleaned := nonAlphanumRe.ReplaceAllString(lower, "")
		if len(cleaned) >= 3 { //nolint:mnd // minimum term length to avoid noise
			addTerm(cleaned)
		}
	}

	for _, raw := range rawWords {
		cleaned := nonAlphanumRe.ReplaceAllString(raw, "")
		if len(cleaned) < 3 { //nolint:mnd // minimum term length to avoid noise
			continue
		}
		subwords := splitIdentifier(cleaned)
		for _, sw := range subwords {
			addTerm(sw)
		}
	}

	return terms
}
