package codesearch

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

const (
	defaultMaxResults  = 100
	defaultMaxFileSize = 512 * 1024
)

// SearchInput controls how code search is performed.
type SearchInput struct {
	Root          string
	Pattern       string
	IsRegex       bool
	FileGlob      string
	ExcludeGlob   string
	Language      string
	ContextLines  int
	MaxResults    int
	CaseSensitive bool
}

// SearchMatch represents a single line match in a file.
type SearchMatch struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Text    string   `json:"text"`
	Context []string `json:"context,omitempty"`
}

// Search scans all files in a repository for lines matching a pattern.
// It uses ingest.IngestRepo to collect files, then searches each file line by line.
func Search(ctx context.Context, input SearchInput) ([]SearchMatch, error) {
	if input.MaxResults <= 0 {
		input.MaxResults = defaultMaxResults
	}

	re, err := buildPattern(input.Pattern, input.IsRegex, input.CaseSensitive)
	if err != nil {
		return nil, err
	}

	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		MaxFileBytes: defaultMaxFileSize,
		Languages:    langs,
	})
	if err != nil {
		return nil, err
	}

	hardcap := input.MaxResults * 5 //nolint:mnd // collect extra for re-ranking
	var allMatches []SearchMatch

	for _, f := range ir.Files {
		if ctx.Err() != nil {
			break
		}

		if input.FileGlob != "" && !matchesFileGlob(f.RelPath, input.FileGlob) {
			continue
		}

		if input.ExcludeGlob != "" && matchesExclude(f.RelPath, input.ExcludeGlob) {
			continue
		}

		fileMatches := searchFile(f.Path, f.RelPath, re, input.ContextLines)
		allMatches = append(allMatches, fileMatches...)
		if len(allMatches) >= hardcap {
			break
		}
	}

	// Re-rank: files with more matches first, then truncate.
	if len(allMatches) > input.MaxResults {
		rankByMatchDensity(allMatches)
	}
	if len(allMatches) > input.MaxResults {
		allMatches = allMatches[:input.MaxResults]
	}
	return allMatches, nil
}

// rankByMatchDensity re-orders matches so files with more hits appear first.
// Files with equal counts preserve their original encounter order.
// Within each file, matches keep their original line order.
func rankByMatchDensity(matches []SearchMatch) {
	counts := make(map[string]int, len(matches))
	for i := range matches {
		counts[matches[i].File]++
	}

	// Track first-seen position per file for stable grouping.
	firstSeen := make(map[string]int, len(counts))
	for i, m := range matches {
		if _, ok := firstSeen[m.File]; !ok {
			firstSeen[m.File] = i
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		ci, cj := counts[matches[i].File], counts[matches[j].File]
		if ci != cj {
			return ci > cj
		}
		// Equal density: group by file, earlier-seen file first.
		return firstSeen[matches[i].File] < firstSeen[matches[j].File]
	})
}

func buildPattern(pattern string, isRegex, caseSensitive bool) (*regexp.Regexp, error) {
	if !isRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}

	return regexp.Compile(pattern)
}

// matchesFileGlob checks whether a relative path matches the file glob filter.
// Supports: extension globs ("*.go" matches basename), directory prefixes
// ("pkg/engine/**" matches anything under pkg/engine/), and full-path globs.
func matchesFileGlob(relPath, glob string) bool {
	// Try matching against full relative path first.
	if matched, _ := filepath.Match(glob, relPath); matched {
		return true
	}
	// Try matching against basename (e.g. "*.go").
	if matched, _ := filepath.Match(glob, filepath.Base(relPath)); matched {
		return true
	}
	// Directory prefix: "pkg/engine/**" → check if relPath starts with "pkg/engine/".
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "/**")
		if strings.HasPrefix(relPath, prefix+"/") || relPath == prefix {
			return true
		}
	}
	return false
}

// matchesExclude checks whether relPath matches any comma-separated exclude patterns.
// Each pattern is checked as filepath.Match against every path component, and as a
// prefix match (e.g. "docs/*" excludes "docs/plans/foo.md").
func matchesExclude(relPath, excludeGlob string) bool {
	for _, raw := range strings.Split(excludeGlob, ",") {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}
		// Direct glob match against full relative path.
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return true
		}
		// Prefix match: "docs/*" → prefix "docs/".
		prefix := strings.TrimRight(pattern, "*")
		prefix = strings.TrimRight(prefix, "/")
		if prefix != "" && strings.HasPrefix(relPath, prefix+"/") {
			return true
		}
	}
	return false
}

func searchFile(absPath, relPath string, re *regexp.Regexp, contextLines int) []SearchMatch {
	file, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("failed to close file", slog.String("path", absPath), slog.Any("error", err))
		}
	}()

	var allLines []string
	var matchLineNums []int

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		allLines = append(allLines, line)

		if re.MatchString(line) {
			matchLineNums = append(matchLineNums, lineNum)
		}
	}

	var matches []SearchMatch

	for _, ln := range matchLineNums {
		m := SearchMatch{
			File: relPath,
			Line: ln,
			Text: allLines[ln-1],
		}

		if contextLines > 0 {
			start := ln - 1 - contextLines
			if start < 0 {
				start = 0
			}

			end := ln + contextLines
			if end > len(allLines) {
				end = len(allLines)
			}

			m.Context = allLines[start:end]
		}

		matches = append(matches, m)
	}

	return matches
}
