package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SymbolSearchInput is the input schema for the symbol_search tool.
type SymbolSearchInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	// Query is the symbol name or pattern to search for.
	Query string `json:"query,omitempty" jsonschema_description:"Symbol name or pattern to search (supports wildcards: Auth* or *Handler)"`

	// Symbol is an alias for Query — matches call_trace/impact_analysis naming.
	Symbol string `json:"symbol,omitempty" jsonschema_description:"Alias for query — symbol name or pattern (supports wildcards: Auth* or *Handler)"`

	// Kind filters by symbol kind: function, method, type, struct, interface, const, var.
	Kind string `json:"kind,omitempty" jsonschema_description:"Filter by kind: function | method | type | struct | interface | const | var (default: all)"`

	// Language filters to files of a specific language.
	Language string `json:"language,omitempty" jsonschema_description:"Limit search to files of this language (e.g. go, python)"`

	// IncludeBody includes the full function/type body in results.
	IncludeBody bool `json:"include_body,omitempty" jsonschema_description:"Include the full source body in results (default: false, only signatures)"`

	// Limit caps the number of results returned.
	Limit int `json:"limit,omitempty" jsonschema_description:"Maximum number of results to return. Default 100, max 500."`
}

type xmlSymbolSearchResponse struct {
	XMLName xml.Name          `xml:"response"`
	Symbols xmlSymbolResults  `xml:"symbols"`
}

type xmlSymbolResults struct {
	Query string              `xml:"query,attr"`
	Count int                 `xml:"count,attr"`
	Items []xmlSymSearchItem  `xml:"symbol"`
}

type xmlSymSearchItem struct {
	Kind       string `xml:"kind,attr"`
	Name       string `xml:"name,attr"`
	File       string `xml:"file,attr"`
	Line       uint32 `xml:"line,attr"`
	End        uint32 `xml:"end,attr"`
	Complexity int    `xml:"complexity,attr,omitempty"`
	Signature  xmlCDATA `xml:"signature,omitempty"`
	Doc        string   `xml:"doc,omitempty"`
	Body       xmlCDATA `xml:"body,omitempty"`
}

// registerSymbolSearch registers the symbol_search MCP tool.
// Searches for symbols across a repository's AST index.
func registerSymbolSearch(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "symbol_search",
		Description: "Search for functions, types, methods, constants, or variables across a repository. " +
			"Uses tree-sitter AST parsing for accurate symbol extraction (no grep heuristics). " +
			"Supports wildcard patterns (Auth*, *Handler), kind filtering, and language filtering. " +
			"Optionally returns full source bodies for matched symbols. " +
			"Falls back to semantic search when no AST matches found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SymbolSearchInput) (*mcp.CallToolResult, error) {
		// Allow "symbol" as alias for "query" — matches call_trace/impact_analysis naming.
		if input.Symbol != "" && input.Query == "" {
			input.Query = input.Symbol
		}

		if input.Repo == "" {
			return errResult("repo is required"), nil
		}
		if input.Query == "" && input.Kind != "" {
			input.Query = "*"
		}
		if input.Query == "" {
			return errResult("query (or symbol) is required"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
		}
		defer cleanup()

		symbols, err := analyze.SearchSymbols(ctx, analyze.SymbolSearchInput{
			Root:        root,
			Query:       input.Query,
			Kind:        parser.NodeKind(input.Kind),
			Language:    input.Language,
			IncludeBody: input.IncludeBody,
			Limit:       input.Limit,
		})
		if err != nil {
			return errResult(fmt.Sprintf("symbol search: %s", err)), nil
		}

		if len(symbols) == 0 {
			if suggestions := semanticSuggest(ctx, sem, root, input.Query, input.Language); suggestions != "" {
				return textResult(fmt.Sprintf("<response tool=\"symbol_search\">\n"+
					"  <symbols query=\"%s\" count=\"0\"/>\n"+
					"%s\n</response>", escapeXML(input.Query), suggestions)), nil
			}
			return textResult(fmt.Sprintf("No symbols found matching %q.", input.Query)), nil
		}
		return largeTextResult(formatSymbolSearchXML(input.Query, symbols), "symbol_search", outputDir), nil
	})
}

// formatSymbolSearchXML formats matched symbols as XML output.
func formatSymbolSearchXML(query string, symbols []*parser.Symbol) string {
	resp := xmlSymbolSearchResponse{
		Symbols: xmlSymbolResults{
			Query: query,
			Count: len(symbols),
			Items: make([]xmlSymSearchItem, len(symbols)),
		},
	}
	for i, sym := range symbols {
		item := xmlSymSearchItem{
			Kind:       string(sym.Kind),
			Name:       sym.Name,
			File:       sym.File,
			Line:       sym.StartLine,
			End:        sym.EndLine,
			Complexity: sym.Complexity,
			Doc:        sym.DocComment,
		}
		if sym.Signature != "" {
			item.Signature = xmlCDATA{Inner: wrapCDATA(sym.Signature)}
		}
		if sym.Body != "" {
			item.Body = xmlCDATA{Inner: wrapCDATA(sym.Body)}
		}
		resp.Symbols.Items[i] = item
	}
	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return fmt.Sprintf("<error>%s</error>", err.Error())
	}
	return xml.Header + string(data)
}
