package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/clean"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/ranking"
	"github.com/anatolykoptev/go-code/internal/render"
)

// Depth level constants.
const (
	DepthOverview = "overview"
	DepthModule   = "module"
	DepthDeep     = "deep"
)

// contextBudget controls how much content fits in the LLM context for each depth level.
type contextBudget struct {
	total         int // total character budget for the entire context
	symbolSummary int // reserved for symbol summary section
	depGraph      int // reserved for dep graph section (0 = skip)
	maxFileChars  int // per-file character limit
}

// fileBudget returns the remaining budget for file contents after other sections.
func (b contextBudget) fileBudget() int {
	return b.total - b.symbolSummary - b.depGraph
}

// budgetForDepth returns the context budget for the given analysis depth.
func budgetForDepth(depth string) contextBudget {
	switch depth {
	case DepthOverview:
		return contextBudget{total: 50_000, symbolSummary: 15_000, depGraph: 0, maxFileChars: 3_000}
	case DepthDeep:
		return contextBudget{total: 200_000, symbolSummary: 40_000, depGraph: 20_000, maxFileChars: 12_000}
	default: // "" or "module"
		return contextBudget{total: 150_000, symbolSummary: 30_000, depGraph: 15_000, maxFileChars: 8_000}
	}
}

// ValidDepth reports whether d is a recognized analysis depth.
func ValidDepth(d string) bool {
	switch d {
	case "", DepthOverview, DepthModule, DepthDeep:
		return true
	default:
		return false
	}
}

// DefaultModeForDepth returns the default render mode for a given depth level.
func DefaultModeForDepth(depth string) string {
	switch depth {
	case DepthOverview:
		return "signatures"
	case DepthDeep:
		return "focused"
	default: // "" or "module"
		return "skeleton"
	}
}

// buildLLMContext assembles a structured context string for the LLM.
//
// Structure:
//  1. File tree (from ingest.RenderTree)
//  2. Symbol summary: name, kind, signature per file
//  3. File contents (cleaned, budget-limited, prioritized files first)
func buildLLMContext(ir *ingest.IngestResult, results []fileParseResult, query string, renderMode render.Mode, depth string) string {
	var sb strings.Builder

	budget := budgetForDepth(depth)

	sb.WriteString("<query>\n")
	sb.WriteString(query)
	sb.WriteString("\n</query>\n\n")

	sb.WriteString("<file-tree>\n")
	sb.WriteString(ingest.RenderTree(ir.Files))
	sb.WriteString("\n</file-tree>\n\n")

	// Append cross-language architecture section for polyglot repositories.
	if section := buildPolyglotSection(ir.Files); section != "" {
		sb.WriteString(section)
	}

	symbolSection := buildSymbolSummary(results, budget.symbolSummary)
	sb.WriteString("<symbols>\n")
	sb.WriteString(symbolSection)
	sb.WriteString("</symbols>\n\n")

	// Insert dependency graph for module and deep depths.
	if budget.depGraph > 0 {
		appendDepGraph(&sb, ir.Root, results, budget.depGraph)
	}

	queryTerms := extractQueryTerms(query)
	prioritized := prioritizeFiles(ir.Files, results, queryTerms)

	// Build path → ParseResult lookup for render modes that need symbols.
	parseMap := make(map[string]*parser.ParseResult, len(results))
	for _, pr := range results {
		if pr.result != nil {
			parseMap[pr.file.Path] = pr.result
		}
	}

	importedBy := computeImportedByCounts(results)
	symbolCounts := computeSymbolCounts(results)

	appendFileContents(&sb, prioritized, budget.fileBudget(), renderMode, queryTerms, parseMap, budget.maxFileChars, importedBy, symbolCounts)

	return sb.String()
}

// appendDepGraph builds and writes the dependency graph section into sb,
// budget-limited to maxChars.
func appendDepGraph(sb *strings.Builder, root string, results []fileParseResult, maxChars int) {
	graph := buildImportGraph(root, results, false)
	if len(graph) == 0 {
		return
	}

	mermaid := renderMermaid(graph)
	if len(mermaid) > maxChars {
		mermaid = mermaid[:maxChars] + "\n... (truncated)\n"
	}

	sb.WriteString("## Dependency Graph\n```mermaid\n")
	sb.WriteString(mermaid)
	sb.WriteString("```\n\n")
}

// buildSymbolSummary returns a compact summary of symbols per file.
func buildSymbolSummary(results []fileParseResult, budget int) string {
	var sb strings.Builder
	remaining := budget

	for _, pr := range results {
		if pr.result == nil || len(pr.result.Symbols) == 0 {
			continue
		}
		header := fmt.Sprintf("### %s\n", pr.file.RelPath)
		if remaining < len(header) {
			break
		}
		sb.WriteString(header)
		remaining -= len(header)

		for _, sym := range pr.result.Symbols {
			line := formatSymbolLine(sym)
			if remaining < len(line) {
				sb.WriteString("... (truncated)\n")
				return sb.String()
			}
			sb.WriteString(line)
			remaining -= len(line)
		}
	}
	return sb.String()
}

// formatSymbolLine renders a single symbol as a compact line.
func formatSymbolLine(sym *parser.Symbol) string {
	if sym.Signature != "" {
		return fmt.Sprintf("  %s %s: %s\n", sym.Kind, sym.Name, sym.Signature)
	}
	return fmt.Sprintf("  %s %s\n", sym.Kind, sym.Name)
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

// computeImportedByCounts returns how many files import each file (by RelPath).
// This is the reverse of buildPageRankGraph — counts inbound references.
func computeImportedByCounts(results []fileParseResult) map[string]int {
	baseToRel := make(map[string]string)
	for _, pr := range results {
		base := filepath.Base(pr.file.RelPath)
		baseToRel[base] = pr.file.RelPath
	}

	counts := make(map[string]int)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		for _, imp := range pr.result.Imports {
			base := filepath.Base(imp)
			if rel, ok := baseToRel[base]; ok {
				counts[rel]++
			}
		}
	}
	return counts
}

// fileAnnotation builds a short HTML comment annotation for a file.
// Returns "" if there's nothing useful to annotate.
func fileAnnotation(relPath string, importedBy, symbolCounts map[string]int, language string) string {
	var parts []string
	if n := importedBy[relPath]; n > 0 {
		parts = append(parts, fmt.Sprintf("imported by %d files", n))
	}
	if n := symbolCounts[relPath]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d symbols", n))
	}
	if language != "" {
		parts = append(parts, language)
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("<!-- %s -->\n", strings.Join(parts, ", "))
}

// appendFileContents writes file content blocks into sb, stopping at the budget.
// When renderMode is non-default and symbol data is available, files are
// rendered using the render package before cleaning.
func appendFileContents(
	sb *strings.Builder,
	files []*ingest.File,
	budget int,
	renderMode render.Mode,
	queryTerms []string,
	parseMap map[string]*parser.ParseResult,
	maxFileChars int,
	importedBy, symbolCounts map[string]int,
) {
	remaining := budget
	cleanOpts := clean.CleanOpts{
		StripComments:     true,
		StripBlankLines:   true,
		TruncateLongLines: true,
		MaxLineChars:      500, //nolint:mnd // line length limit for LLM context
		TruncateBase64:    true,
		MaxFileChars:      maxFileChars,
	}

	renderOpts := render.Opts{
		Mode:       renderMode,
		QueryTerms: queryTerms,
	}

	for _, f := range files {
		source, err := readFileContent(f.Path)
		if err != nil {
			continue
		}

		// Apply render mode if we have symbols for this file.
		rendered := source
		if renderMode != render.ModeDefault {
			if pr, ok := parseMap[f.Path]; ok && len(pr.Symbols) > 0 {
				rendered = render.RenderFile(source, pr.Symbols, renderOpts)
			}
		}

		cleaned := clean.CleanSource(rendered, f.Language, cleanOpts)
		annotation := fileAnnotation(f.RelPath, importedBy, symbolCounts, f.Language)
		block := annotation + formatFileBlock(f.RelPath, cleaned)
		if remaining < len(block) {
			break
		}
		sb.WriteString(block)
		remaining -= len(block)
	}
}

// formatFileBlock wraps file content in an XML-tagged section block.
func formatFileBlock(relPath, content string) string {
	return fmt.Sprintf("<file path=%q>\n%s\n</file>\n\n", relPath, content)
}

// readFileContent reads a file and returns its content as a string.
func readFileContent(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// prioritizeFiles orders files by relevance to the query using BM25F scoring
// combined with PageRank importance from the file import graph.
//
// BM25F weighs symbol name matches (x5), file path matches (x3), and content (x1).
// PageRank propagates importance through import edges, surfacing core files.
// Combined score: 70% BM25F relevance + 30% PageRank importance.
func prioritizeFiles(files []*ingest.File, results []fileParseResult, queryTerms []string) []*ingest.File {
	// Build BM25F documents.
	fileSymbols := buildFileSymbolMap(results)
	docs := make([]ranking.Document, len(files))
	for i, f := range files {
		docs[i] = ranking.Document{
			Path:    f.RelPath,
			Symbols: fileSymbols[f.RelPath],
		}
	}
	scorer := ranking.NewBM25F(docs)

	// Build import graph for PageRank.
	prGraph := buildPageRankGraph(results)
	pageRanks := ranking.PageRank(prGraph, 20, 0.85) //nolint:mnd // standard PageRank params

	type scoredFile struct {
		file  *ingest.File
		score float64
	}

	scored := make([]scoredFile, 0, len(files))
	for i, f := range files {
		bm25Score := scorer.ScoreTerms(queryTerms, docs[i])
		prScore := pageRanks[f.RelPath] * 100 //nolint:mnd // normalize PageRank to BM25F magnitude
		combined := bm25Score*0.7 + prScore*0.3 //nolint:mnd // 70% relevance + 30% importance
		scored = append(scored, scoredFile{file: f, score: combined})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	out := make([]*ingest.File, len(scored))
	for i, sf := range scored {
		out[i] = sf.file
	}
	return out
}

// buildFileSymbolMap extracts symbol names per file from parse results.
func buildFileSymbolMap(results []fileParseResult) map[string][]string {
	m := make(map[string][]string)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		names := make([]string, 0, len(pr.result.Symbols))
		for _, sym := range pr.result.Symbols {
			names = append(names, sym.Name)
		}
		m[pr.file.RelPath] = names
	}
	return m
}

// buildPageRankGraph builds a file-to-file import graph for PageRank.
// Each entry maps a source file (RelPath) to the files it imports.
func buildPageRankGraph(results []fileParseResult) map[string][]string {
	// Build a base-name → relPath map for resolving import targets.
	baseToRel := make(map[string]string)
	for _, pr := range results {
		base := filepath.Base(pr.file.RelPath)
		baseToRel[base] = pr.file.RelPath
	}

	graph := make(map[string][]string)
	for _, pr := range results {
		if pr.result == nil {
			continue
		}
		var targets []string
		for _, imp := range pr.result.Imports {
			base := filepath.Base(imp)
			if rel, ok := baseToRel[base]; ok {
				targets = append(targets, rel)
			}
		}
		graph[pr.file.RelPath] = targets
	}
	return graph
}


// nonAlphanumRe matches characters that are not letters, digits, or underscores.
var nonAlphanumRe = regexp.MustCompile(`[^\w]`)

// splitCamelCase splits a camelCase or PascalCase identifier into lowercase subwords.
// Consecutive uppercase letters are treated as an acronym (e.g. "LLMClient" → ["llm", "client"]).
// Letter-digit and digit-letter transitions are also split (e.g. "Auth2Factor" → ["auth", "factor"]).
// Subwords shorter than 2 characters are discarded.
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

	// Append the last segment.
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

	// lowerUpper: "handleUser" → split before 'U'
	if unicode.IsLower(prev) && unicode.IsUpper(cur) {
		return true
	}
	// letter-digit: "Auth2Factor" splits before '2'
	if unicode.IsLetter(prev) && unicode.IsDigit(cur) {
		return true
	}
	// digit-letter: "2Factor" → split before 'F'
	if unicode.IsDigit(prev) && unicode.IsLetter(cur) {
		return true
	}
	// acronym end: "LLMClient" → split before 'C' (upper+upper+lower)
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
// Compound identifiers (camelCase, snake_case) are split into subwords alongside
// the original compound term for better file and symbol matching.
func extractQueryTerms(query string) []string {
	seen := make(map[string]struct{})
	var terms []string

	addTerm := func(t string) {
		if _, ok := seen[t]; !ok && len(t) >= 3 { //nolint:mnd // minimum term length to avoid noise
			seen[t] = struct{}{}
			terms = append(terms, t)
		}
	}

	// Original (pre-lowercase) words for identifier splitting.
	rawWords := strings.Fields(query)

	// First pass: add cleaned lowercase words (existing behavior, but min length 3).
	for _, raw := range rawWords {
		lower := strings.ToLower(raw)
		cleaned := nonAlphanumRe.ReplaceAllString(lower, "")
		if len(cleaned) >= 3 { //nolint:mnd // minimum term length to avoid noise
			addTerm(cleaned)
		}
	}

	// Second pass: split identifiers into subwords from the original casing.
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

// buildPolyglotSection returns a "Cross-Language Architecture" section for
// polyglot repositories. Returns "" for single-language repos (no noise).
func buildPolyglotSection(files []*ingest.File) string {
	structure := polyglot.DetectStructure(files)
	if !structure.IsPolyglot() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Cross-Language Architecture\n")
	fmt.Fprintf(&sb, "This is a polyglot repository with %d languages.\n\n", len(structure.Languages))

	if len(structure.Layers) > 0 {
		sb.WriteString("Layers:\n")
		for _, layer := range structure.Layers {
			if layer.Role != "" {
				fmt.Fprintf(&sb, "- %s (%s, %s, %d files)\n", layer.Name, layer.Language, layer.Role, layer.Files)
			} else {
				fmt.Fprintf(&sb, "- %s (%s, %d files)\n", layer.Name, layer.Language, layer.Files)
			}
		}
		sb.WriteString("\n")
	}

	// Language summary sorted by file count (descending).
	type langCount struct {
		lang  string
		count int
	}
	langs := make([]langCount, 0, len(structure.Languages))
	for lang, count := range structure.Languages {
		langs = append(langs, langCount{lang, count})
	}
	sort.Slice(langs, func(i, j int) bool {
		return langs[i].count > langs[j].count
	})
	sb.WriteString("Languages: ")
	for i, lc := range langs {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s (%d files)", lc.lang, lc.count)
	}
	sb.WriteString("\n\n")

	return sb.String()
}
