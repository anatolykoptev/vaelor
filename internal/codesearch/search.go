package codesearch

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
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

	var matches []SearchMatch

	for _, f := range ir.Files {
		if ctx.Err() != nil {
			break
		}

		if input.FileGlob != "" {
			matched, _ := filepath.Match(input.FileGlob, filepath.Base(f.Path))
			if !matched {
				continue
			}
		}

		if input.ExcludeGlob != "" && matchesExclude(f.RelPath, input.ExcludeGlob) {
			continue
		}

		fileMatches := searchFile(f.Path, f.RelPath, re, input.ContextLines)
		for _, m := range fileMatches {
			matches = append(matches, m)
			if len(matches) >= input.MaxResults {
				return matches, nil
			}
		}
	}

	return matches, nil
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
	defer file.Close()

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
