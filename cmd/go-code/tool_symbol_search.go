package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SymbolSearchInput is the input schema for the symbol_search tool.
type SymbolSearchInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo) or absolute local path"`

	// Query is the symbol name or pattern to search for (supports wildcards: Auth*, *Handler).
	Query string `json:"query" jsonschema_description:"Symbol name or pattern to search (supports wildcards: Auth* or *Handler)"`

	// Kind filters by symbol kind: function, method, type, struct, interface, const, var.
	Kind string `json:"kind,omitempty" jsonschema_description:"Filter by kind: function | method | type | struct | interface | const | var (default: all)"`

	// Language filters to files of a specific language.
	Language string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python)"`

	// IncludeBody includes the full function/type body in results.
	IncludeBody bool `json:"include_body,omitempty" jsonschema_description:"Include the full source body in results (default: false, only signatures)"`
}

// registerSymbolSearch registers the symbol_search MCP tool.
// Searches for symbols across a repository's AST index.
func registerSymbolSearch(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "symbol_search",
		Description: "Search for functions, types, methods, constants, or variables across a repository. " +
			"Uses tree-sitter AST parsing for accurate symbol extraction (no grep heuristics). " +
			"Supports wildcard patterns (Auth*, *Handler), kind filtering, and language filtering. " +
			"Optionally returns full source bodies for matched symbols.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SymbolSearchInput) (*mcp.CallToolResult, noOutput, error) {
		if input.Repo == "" {
			return errResult("repo is required"), noOutput{}, nil
		}
		if input.Query == "" {
			return errResult("query is required"), noOutput{}, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), noOutput{}, nil
		}
		defer cleanup()

		symbols, err := analyze.SearchSymbols(ctx, analyze.SymbolSearchInput{
			Root:        root,
			Query:       input.Query,
			Kind:        parser.NodeKind(input.Kind),
			Language:    input.Language,
			IncludeBody: input.IncludeBody,
		})
		if err != nil {
			return errResult(fmt.Sprintf("symbol search: %s", err)), noOutput{}, nil
		}

		return textResult(formatSymbolSearchResult(input.Query, symbols)), noOutput{}, nil
	})
}

// formatSymbolSearchResult formats matched symbols as a readable list.
func formatSymbolSearchResult(query string, symbols []*parser.Symbol) string {
	if len(symbols) == 0 {
		return fmt.Sprintf("No symbols found matching %q.\n", query)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d symbol(s) matching %q:\n\n", len(symbols), query)

	for _, sym := range symbols {
		fmt.Fprintf(&sb, "[%s] %s\n", sym.Kind, sym.Name)
		fmt.Fprintf(&sb, "  File: %s (lines %d-%d)\n", sym.File, sym.StartLine, sym.EndLine)
		if sym.Signature != "" {
			fmt.Fprintf(&sb, "  Signature: %s\n", sym.Signature)
		}
		if sym.DocComment != "" {
			fmt.Fprintf(&sb, "  Doc: %s\n", sym.DocComment)
		}
		if sym.Body != "" {
			fmt.Fprintf(&sb, "  Body:\n```\n%s\n```\n", sym.Body)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
