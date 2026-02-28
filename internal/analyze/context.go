package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/anatolykoptev/go-code/internal/clean"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/polyglot"
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

	sb.WriteString("## Query\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	sb.WriteString("## Repository File Tree\n```\n")
	sb.WriteString(ingest.RenderTree(ir.Files))
	sb.WriteString("\n```\n\n")

	// Append cross-language architecture section for polyglot repositories.
	if section := buildPolyglotSection(ir.Files); section != "" {
		sb.WriteString(section)
	}

	symbolSection := buildSymbolSummary(results, budget.symbolSummary)
	sb.WriteString("## Symbol Summary\n")
	sb.WriteString(symbolSection)
	sb.WriteString("\n")

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

	appendFileContents(&sb, prioritized, budget.fileBudget(), renderMode, queryTerms, parseMap, budget.maxFileChars)

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
) {
	sb.WriteString("## File Contents\n\n")
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
		block := formatFileBlock(f.RelPath, cleaned)
		if remaining < len(block) {
			break
		}
		sb.WriteString(block)
		remaining -= len(block)
	}
}

// formatFileBlock wraps file content in a labeled section block.
func formatFileBlock(relPath, content string) string {
	return fmt.Sprintf("=== File: %s ===\n%s\n\n", relPath, content)
}

// readFileContent reads a file and returns its content as a string.
func readFileContent(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// prioritizeFiles orders files by relevance to the query.
//
// Priority order:
//  1. Files whose path contains focus/query keywords (highest relevance)
//  2. Files imported by many other files (high connectivity)
//  3. Files with the most symbols
//  4. Remaining files (by path alphabetically)
func prioritizeFiles(files []*ingest.File, results []fileParseResult, queryTerms []string) []*ingest.File {
	importCounts := computeImportCounts(results)
	symbolCounts := computeSymbolCounts(results)

	type scoredFile struct {
		file  *ingest.File
		score int
	}

	scored := make([]scoredFile, 0, len(files))
	for _, f := range files {
		score := scoreFile(f.RelPath, importCounts, symbolCounts, queryTerms)
		scored = append(scored, scoredFile{file: f, score: score})
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

// scoreFile computes a relevance score for a file.
func scoreFile(relPath string, importCounts, symbolCounts map[string]int, queryTerms []string) int {
	const (
		queryMatchScore  = 100
		importScore      = 10
		symbolScoreScale = 1
	)
	score := 0
	lower := strings.ToLower(relPath)

	for _, term := range queryTerms {
		if strings.Contains(lower, term) {
			score += queryMatchScore
		}
	}
	score += importCounts[relPath] * importScore
	score += symbolCounts[relPath] * symbolScoreScale
	return score
}

// computeImportCounts returns how many files import each file (by RelPath).
func computeImportCounts(results []fileParseResult) map[string]int {
	// Build a base-name → relPath map first.
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

// nonAlphanumRe matches characters that are not letters, digits, or underscores.
var nonAlphanumRe = regexp.MustCompile(`[^\w]`)

// extractQueryTerms splits the query into lowercase alphanumeric terms for matching.
func extractQueryTerms(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var terms []string
	for _, w := range words {
		clean := nonAlphanumRe.ReplaceAllString(w, "")
		if len(clean) >= 3 { //nolint:mnd // minimum term length to avoid noise
			terms = append(terms, clean)
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
