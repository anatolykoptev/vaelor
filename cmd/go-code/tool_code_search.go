package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeSearchInput is the input schema for the code_search tool.
type CodeSearchInput struct {
	Repo          string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Pattern       string `json:"pattern,omitempty" jsonschema_description:"Search pattern (literal string or regex). Use pattern or query."`
	Query         string `json:"query,omitempty" jsonschema_description:"Alias for pattern — use either query or pattern"`
	IsRegex       bool   `json:"is_regex,omitempty" jsonschema_description:"Treat pattern as regular expression (default: literal)"`
	FileGlob      string `json:"file_glob,omitempty" jsonschema_description:"File glob filter (e.g. '*.go', '*.py')"`
	Path          string `json:"path,omitempty" jsonschema_description:"Directory path filter — alias for file_glob (e.g. 'internal/query'). Converted to file_glob automatically."`
	Language      string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python, typescript)"`
	ContextLines  int    `json:"context_lines,omitempty" jsonschema_description:"Number of context lines before/after each match (default: 2)"`
	MaxResults    int    `json:"max_results,omitempty" jsonschema_description:"Maximum number of matches to return (default: 50, max: 200)"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty" jsonschema_description:"Case-sensitive matching (default: true). Set false for case-insensitive."`
	ExcludeGlob   string `json:"exclude_glob,omitempty" jsonschema_description:"Comma-separated glob patterns to exclude files (e.g. 'docs/*,vendor/*'). Matches against relative paths."`
	Scope         string `json:"scope,omitempty" jsonschema_description:"AST scope filter: function_bodies, comments, strings, type_definitions, imports. Requires language."`
	Structural    bool   `json:"structural,omitempty" jsonschema_description:"Treat pattern as structural AST pattern with $WILDCARDS (e.g. 'if $ERR != nil { return $ERR }'). Requires language."`
	Expand        string `json:"expand,omitempty" jsonschema_description:"Expand matches to enclosing AST symbol: 'function' (enclosing function/method) or 'block' (function/struct/class/impl). Returns full symbol body."`
	MaxTokens     int    `json:"max_tokens,omitempty" jsonschema_description:"Maximum token budget for expanded bodies. Matches exceeding this are skipped. Estimate: 1 token ≈ 4 chars."`
}

type xmlSearchResponse struct {
	XMLName xml.Name  `xml:"response"`
	Search  xmlSearch `xml:"search"`
}

type xmlSearch struct {
	Pattern string           `xml:"pattern,attr"`
	IsRegex bool             `xml:"isRegex,attr"`
	Matches int              `xml:"matches,attr"`
	Items   []xmlSearchMatch `xml:"match"`
}

type xmlSearchMatch struct {
	File     string            `xml:"file,attr"`
	Line     int               `xml:"line,attr"`
	Text     xmlCDATA          `xml:"text"`
	Context  []xmlCDATA        `xml:"ctx,omitempty"`
	Expanded *xmlExpandedBlock `xml:"expanded,omitempty"`
}

type xmlExpandedBlock struct {
	SymbolName string   `xml:"symbol,attr"`
	SymbolKind string   `xml:"kind,attr"`
	LineStart  int      `xml:"lineStart,attr"`
	LineEnd    int      `xml:"lineEnd,attr"`
	Body       xmlCDATA `xml:"body"`
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

	// Route to ox-codes for scoped or structural search (no Go fallback for new features).
	if input.Scope != "" && deps.OxCodes != nil {
		return handleScopedSearch(ctx, input, root, deps.OxCodes, outputDir)
	}
	if input.Structural && deps.OxCodes != nil {
		return handleStructuralSearch(ctx, input, root, deps.OxCodes, outputDir)
	}

	// When expand is requested, use ox-codes directly and return expanded format.
	if input.Expand != "" && deps.OxCodes != nil {
		oxMatches, err := grepSearchOx(ctx, input, root, deps.OxCodes)
		if err != nil {
			return errResult(fmt.Sprintf("search: %s", err)), nil
		}
		return xmlMarshalResult(formatExpandedSearchXML(input, oxMatches), "code_search", outputDir), nil
	}

	matches, err := grepSearch(ctx, input, root, deps.OxCodes)
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

// grepSearch runs grep via ox-codes with fallback to Go codesearch.
func grepSearch(ctx context.Context, input CodeSearchInput, root string, client *oxcodes.Client) ([]codesearch.SearchMatch, error) {
	searchInput := buildCodeSearchInput(input, root)

	if client != nil {
		oxResult, err := client.Search(ctx, oxcodes.SearchInput{
			Root:          searchInput.Root,
			Pattern:       searchInput.Pattern,
			IsRegex:       searchInput.IsRegex,
			FileGlob:      searchInput.FileGlob,
			ExcludeGlob:   searchInput.ExcludeGlob,
			ContextLines:  searchInput.ContextLines,
			MaxResults:    searchInput.MaxResults,
			CaseSensitive: searchInput.CaseSensitive,
			Language:      searchInput.Language,
		})
		if err == nil {
			return convertOxMatches(oxResult.Matches), nil
		}
		slog.Warn("ox-codes search failed, falling back to Go codesearch", "err", err)
	}

	return codesearch.Search(ctx, searchInput)
}

// grepSearchOx runs grep via ox-codes with expand support, returning raw ox matches.
func grepSearchOx(ctx context.Context, input CodeSearchInput, root string, client *oxcodes.Client) ([]oxcodes.SearchMatch, error) {
	searchInput := buildCodeSearchInput(input, root)
	// Only request markdown when expand is active — otherwise body is empty.
	format := ""
	if input.Expand != "" {
		format = "markdown"
	}
	oxResult, err := client.Search(ctx, oxcodes.SearchInput{
		Root:          searchInput.Root,
		Pattern:       searchInput.Pattern,
		IsRegex:       searchInput.IsRegex,
		FileGlob:      searchInput.FileGlob,
		ExcludeGlob:   searchInput.ExcludeGlob,
		ContextLines:  searchInput.ContextLines,
		MaxResults:    searchInput.MaxResults,
		CaseSensitive: searchInput.CaseSensitive,
		Language:      searchInput.Language,
		Expand:        input.Expand,
		MaxTokens:     input.MaxTokens,
		Format:        format,
	})
	if err != nil {
		return nil, err
	}
	return oxResult.Matches, nil
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
