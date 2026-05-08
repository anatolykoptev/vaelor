package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codesearch"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleScopedSearch routes to ox-codes /search/scoped.
func handleScopedSearch(ctx context.Context, input CodeSearchInput, root string, client *oxcodes.Client, outputDir string, mappings []analyze.PathMapping) (*mcp.CallToolResult, error) {
	maxResults := clampMaxResults(input.MaxResults)
	caseSensitive := true
	if input.CaseSensitive != nil {
		caseSensitive = *input.CaseSensitive
	}

	// Only request markdown when expand is active — otherwise body is empty.
	format := ""
	if input.Expand != "" {
		format = "markdown"
	}
	result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root:          root,
		Pattern:       input.Pattern,
		Scope:         input.Scope,
		Language:      input.Language,
		IsRegex:       input.IsRegex,
		MaxResults:    maxResults,
		CaseSensitive: caseSensitive,
		ExcludeGlob:   input.ExcludeGlob,
		Expand:        input.Expand,
		MaxTokens:     input.MaxTokens,
		Format:        format,
	})
	if err != nil {
		return errResult(fmt.Sprintf("scoped search: %s", err)), nil
	}

	if input.Expand != "" {
		return xmlMarshalResult(formatExpandedSearchXML(input, result.Matches), "code_search", outputDir), nil
	}
	matches := convertOxMatches(result.Matches)
	return xmlMarshalResult(formatCodeSearchXML(input, matches, mappings), "code_search", outputDir), nil
}

// handleStructuralSearch routes to ox-codes /search/structural.
func handleStructuralSearch(ctx context.Context, input CodeSearchInput, root string, client *oxcodes.Client, outputDir string, mappings []analyze.PathMapping) (*mcp.CallToolResult, error) {
	maxResults := clampMaxResults(input.MaxResults)

	// Only request markdown when expand is active — otherwise body is empty.
	format := ""
	if input.Expand != "" {
		format = "markdown"
	}
	result, err := client.SearchStructural(ctx, oxcodes.StructuralSearchInput{
		Root:        root,
		Pattern:     input.Pattern,
		Language:    input.Language,
		MaxResults:  maxResults,
		ExcludeGlob: input.ExcludeGlob,
		Expand:      input.Expand,
		MaxTokens:   input.MaxTokens,
		Format:      format,
	})
	if err != nil {
		return errResult(fmt.Sprintf("structural search: %s", err)), nil
	}

	if input.Expand != "" {
		return xmlMarshalResult(formatExpandedSearchXML(input, result.Matches), "code_search", outputDir), nil
	}
	matches := convertOxMatches(result.Matches)
	return xmlMarshalResult(formatCodeSearchXML(input, matches, mappings), "code_search", outputDir), nil
}

// convertOxMatches converts ox-codes matches to codesearch.SearchMatch.
func convertOxMatches(oxMatches []oxcodes.SearchMatch) []codesearch.SearchMatch {
	matches := make([]codesearch.SearchMatch, len(oxMatches))
	for i, m := range oxMatches {
		matches[i] = codesearch.SearchMatch{
			File:    m.File,
			Line:    m.Line,
			Text:    m.Text,
			Context: m.Context,
		}
	}
	return matches
}

// formatExpandedSearchXML builds the XML response for expanded search results.
func formatExpandedSearchXML(input CodeSearchInput, matches []oxcodes.SearchMatch) xmlSearchResponse {
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
			if c != "" {
				item.Context = append(item.Context, xmlCDATA{Inner: wrapCDATA(c)})
			}
		}
		if m.Expanded != nil {
			exp := &xmlExpandedBlock{
				SymbolName: m.Expanded.SymbolName,
				SymbolKind: m.Expanded.SymbolKind,
				LineStart:  m.Expanded.LineStart,
				LineEnd:    m.Expanded.LineEnd,
			}
			if m.Expanded.Body != "" {
				exp.Body = &xmlCDATA{Inner: wrapCDATA(m.Expanded.Body)}
			}
			item.Expanded = exp
		}
		resp.Search.Items[i] = item
	}
	return resp
}

// clampMaxResults applies defaults and caps to max_results.
func clampMaxResults(n int) int {
	if n <= 0 {
		return defaultMaxResults
	}
	if n > maxResultsCap {
		return maxResultsCap
	}
	return n
}
