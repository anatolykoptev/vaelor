package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codesearch"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeSearchInput is the input schema for the code_search tool.
type CodeSearchInput struct {
	Repo         string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Pattern      string `json:"pattern,omitempty" jsonschema_description:"Search pattern (literal string or regex). Use pattern or query."`
	Query        string `json:"query,omitempty" jsonschema_description:"Alias for pattern — use either query or pattern"`
	IsRegex      bool   `json:"is_regex,omitempty" jsonschema_description:"Treat pattern as regular expression (default: literal)"`
	FileGlob     string `json:"file_glob,omitempty" jsonschema_description:"File glob filter (e.g. '*.go', '*.py')"`
	Path         string `json:"path,omitempty" jsonschema_description:"Directory path filter — alias for file_glob (e.g. 'internal/query'). Converted to file_glob automatically."`
	Language     string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python, typescript)"`
	ContextLines  int   `json:"context_lines,omitempty" jsonschema_description:"Number of context lines before/after each match (default: 2)"`
	MaxResults    int   `json:"max_results,omitempty" jsonschema_description:"Maximum number of matches to return (default: 50, max: 200)"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty" jsonschema_description:"Case-sensitive matching (default: true). Set false for case-insensitive."`
	ExcludeGlob   string `json:"exclude_glob,omitempty" jsonschema_description:"Comma-separated glob patterns to exclude files (e.g. 'docs/*,vendor/*'). Matches against relative paths."`
}

type xmlSearchResponse struct {
	XMLName xml.Name        `xml:"response"`
	Search  xmlSearch       `xml:"search"`
}

type xmlSearch struct {
	Pattern string          `xml:"pattern,attr"`
	IsRegex bool            `xml:"isRegex,attr"`
	Matches int             `xml:"matches,attr"`
	Items   []xmlSearchMatch `xml:"match"`
}

type xmlSearchMatch struct {
	File    string     `xml:"file,attr"`
	Line    int        `xml:"line,attr"`
	Text    xmlCDATA   `xml:"text"`
	Context []xmlCDATA `xml:"ctx,omitempty"`
}

func registerCodeSearch(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_search",
		Description: "Search for code patterns within a repository. " +
			"Supports literal strings and regular expressions. " +
			"Returns matching lines with file paths, line numbers, and surrounding context. " +
			"Use for finding: TODO comments, error messages, function calls, string literals, " +
			"API endpoints, configuration patterns, or any text pattern in source code. " +
			"Falls back to semantic search when no matches found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeSearchInput) (*mcp.CallToolResult, error) {
		return handleCodeSearch(ctx, input, deps, sem, outputDir)
	})
}

func handleCodeSearch(ctx context.Context, input CodeSearchInput, deps analyze.Deps, sem *SemanticDeps, outputDir string) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	normalizeCodeSearchInput(&input)
	if input.Pattern == "" {
		return errResult("pattern is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	searchInput := buildCodeSearchInput(input, root)
	matches, err := codesearch.Search(ctx, searchInput)
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	// Semantic fallback when grep finds nothing.
	if len(matches) == 0 {
		if suggestions := semanticSuggest(ctx, sem, root, input.Pattern, input.Language); suggestions != "" {
			return textResult(fmt.Sprintf("<response tool=\"code_search\">\n"+
				"  <search pattern=\"%s\" matches=\"0\"/>\n"+
				"%s\n</response>", escapeXML(input.Pattern), suggestions)), nil
		}
	}

	return xmlMarshalResult(formatCodeSearchXML(input, matches), "code_search", outputDir), nil
}

// normalizeCodeSearchInput resolves aliases and sets defaults.
func normalizeCodeSearchInput(input *CodeSearchInput) {
	if input.Pattern == "" && input.Query != "" {
		input.Pattern = input.Query
	}
	if input.Path != "" && input.FileGlob == "" {
		input.FileGlob = input.Path + "/**"
	}
}

const (
	defaultContextLines = 2
	maxContextLines     = 10
	defaultMaxResults   = 50
	maxResultsCap       = 200
)

// buildCodeSearchInput converts MCP input to internal codesearch.SearchInput.
func buildCodeSearchInput(input CodeSearchInput, root string) codesearch.SearchInput {
	contextLines := input.ContextLines
	if contextLines <= 0 {
		contextLines = defaultContextLines
	}
	if contextLines > maxContextLines {
		contextLines = maxContextLines
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > maxResultsCap {
		maxResults = maxResultsCap
	}

	caseSensitive := true
	if input.CaseSensitive != nil {
		caseSensitive = *input.CaseSensitive
	}

	return codesearch.SearchInput{
		Root:          root,
		Pattern:       input.Pattern,
		IsRegex:       input.IsRegex,
		FileGlob:      input.FileGlob,
		ExcludeGlob:   input.ExcludeGlob,
		Language:      input.Language,
		ContextLines:  contextLines,
		MaxResults:    maxResults,
		CaseSensitive: caseSensitive,
	}
}

// formatCodeSearchXML builds the XML response for code_search results.
func formatCodeSearchXML(input CodeSearchInput, matches []codesearch.SearchMatch) xmlSearchResponse {
	resp := xmlSearchResponse{
		Search: xmlSearch{
			Pattern: input.Pattern,
			IsRegex: input.IsRegex,
			Matches: len(matches),
			Items:   make([]xmlSearchMatch, len(matches)),
		},
	}
	for i, m := range matches {
		item := xmlSearchMatch{
			File: m.File,
			Line: m.Line,
			Text: xmlCDATA{Inner: wrapCDATA(m.Text)},
		}
		for _, c := range m.Context {
			item.Context = append(item.Context, xmlCDATA{Inner: wrapCDATA(c)})
		}
		resp.Search.Items[i] = item
	}
	return resp
}
