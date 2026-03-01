package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeSearchInput is the input schema for the code_search tool.
type CodeSearchInput struct {
	Repo         string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Pattern      string `json:"pattern" jsonschema_description:"Search pattern (literal string or regex)"`
	Query        string `json:"query,omitempty" jsonschema_description:"Alias for pattern — use either query or pattern"`
	IsRegex      bool   `json:"is_regex,omitempty" jsonschema_description:"Treat pattern as regular expression (default: literal)"`
	FileGlob     string `json:"file_glob,omitempty" jsonschema_description:"File glob filter (e.g. '*.go', '*.py')"`
	Language     string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python, typescript)"`
	ContextLines  int   `json:"context_lines,omitempty" jsonschema_description:"Number of context lines before/after each match (default: 2)"`
	MaxResults    int   `json:"max_results,omitempty" jsonschema_description:"Maximum number of matches to return (default: 50, max: 200)"`
	CaseSensitive *bool `json:"case_sensitive,omitempty" jsonschema_description:"Case-sensitive matching (default: true). Set false for case-insensitive."`
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
	File    string   `xml:"file,attr"`
	Line    int      `xml:"line,attr"`
	Text    string   `xml:"text"`
	Context []string `xml:"ctx,omitempty"`
}

func registerCodeSearch(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcp.AddTool(server, &mcp.Tool{
		Name: "code_search",
		Description: "Search for code patterns within a repository. " +
			"Supports literal strings and regular expressions. " +
			"Returns matching lines with file paths, line numbers, and surrounding context. " +
			"Use for finding: TODO comments, error messages, function calls, string literals, " +
			"API endpoints, configuration patterns, or any text pattern in source code.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeSearchInput) (*mcp.CallToolResult, any, error) {
		return handleCodeSearch(ctx, input, deps, outputDir)
	})
}

func handleCodeSearch(ctx context.Context, input CodeSearchInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}
	// Allow "query" as alias for "pattern".
	if input.Pattern == "" && input.Query != "" {
		input.Pattern = input.Query
	}
	if input.Pattern == "" {
		return errResult("pattern is required"), nil, nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
	}
	defer cleanup()

	contextLines := input.ContextLines
	if contextLines <= 0 {
		contextLines = 2
	}

	const maxContextLines = 10
	if contextLines > maxContextLines {
		contextLines = maxContextLines
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	const maxResultsCap = 200
	if maxResults > maxResultsCap {
		maxResults = maxResultsCap
	}

	caseSensitive := true
	if input.CaseSensitive != nil {
		caseSensitive = *input.CaseSensitive
	}

	matches, err := codesearch.Search(ctx, codesearch.SearchInput{
		Root:          root,
		Pattern:       input.Pattern,
		IsRegex:       input.IsRegex,
		FileGlob:      input.FileGlob,
		Language:      input.Language,
		ContextLines:  contextLines,
		MaxResults:    maxResults,
		CaseSensitive: caseSensitive,
	})
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil, nil
	}

	resp := xmlSearchResponse{
		Search: xmlSearch{
			Pattern: input.Pattern,
			IsRegex: input.IsRegex,
			Matches: len(matches),
			Items:   make([]xmlSearchMatch, len(matches)),
		},
	}
	for i, m := range matches {
		resp.Search.Items[i] = xmlSearchMatch{
			File:    m.File,
			Line:    m.Line,
			Text:    m.Text,
			Context: m.Context,
		}
	}

	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
	}

	return largeTextResult(xml.Header+string(data), "code_search", outputDir), nil, nil
}
