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
	"github.com/anatolykoptev/go-code/internal/render"
)

// llmContextBudget is the maximum number of characters to include in the LLM context.
const llmContextBudget = 150_000

// symbolSummaryBudget is the max chars reserved for the symbol summary section.
const symbolSummaryBudget = 30_000

// fileBudget is the max chars reserved for file contents.
const fileBudget = llmContextBudget - symbolSummaryBudget

// maxFileContentChars is the per-file character limit when cleaning for LLM context.
const maxFileContentChars = 8_000

// buildLLMContext assembles a structured context string for the LLM.
//
// Structure:
//  1. File tree (from ingest.RenderTree)
//  2. Symbol summary: name, kind, signature per file
//  3. File contents (cleaned, budget-limited, prioritized files first)
func buildLLMContext(ir *ingest.IngestResult, results []fileParseResult, query string, renderMode render.Mode) string {
	var sb strings.Builder

	sb.WriteString("## Query\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	sb.WriteString("## Repository File Tree\n```\n")
	sb.WriteString(ingest.RenderTree(ir.Files))
	sb.WriteString("\n```\n\n")

	symbolSection := buildSymbolSummary(results)
	sb.WriteString("## Symbol Summary\n")
	sb.WriteString(symbolSection)
	sb.WriteString("\n")

	prioritized := prioritizeFiles(ir.Files, results, query)
	queryTerms := extractQueryTerms(query)

	// Build path → ParseResult lookup for render modes that need symbols.
	parseMap := make(map[string]*parser.ParseResult, len(results))
	for _, pr := range results {
		if pr.result != nil {
			parseMap[pr.file.Path] = pr.result
		}
	}

	appendFileContents(&sb, prioritized, fileBudget, renderMode, queryTerms, parseMap)

	return sb.String()
}

// buildSymbolSummary returns a compact summary of symbols per file.
func buildSymbolSummary(results []fileParseResult) string {
	var sb strings.Builder
	remaining := symbolSummaryBudget

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
) {
	sb.WriteString("## File Contents\n\n")
	remaining := budget
	cleanOpts := clean.CleanOpts{
		StripComments:     true,
		StripBlankLines:   true,
		TruncateLongLines: true,
		MaxLineChars:      500, //nolint:mnd // line length limit for LLM context
		TruncateBase64:    true,
		MaxFileChars:      maxFileContentChars,
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
func prioritizeFiles(files []*ingest.File, results []fileParseResult, query string) []*ingest.File {
	importCounts := computeImportCounts(results)
	symbolCounts := computeSymbolCounts(results)
	queryTerms := extractQueryTerms(query)

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
