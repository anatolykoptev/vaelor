package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/parser"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UnderstandInput is the input schema for the understand tool.
type UnderstandInput struct {
	Repo           string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol         string `json:"symbol" jsonschema_description:"Function or method name to analyze in depth"`
	Focus          string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope"`
	Language       string `json:"language,omitempty" jsonschema_description:"Limit to files of this language"`
	IncludeCallers bool   `json:"include_callers,omitempty" jsonschema_description:"Include who calls this symbol (default: false)"`
}

func registerUnderstand(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "understand",
		Description: "Deep-dive into a single symbol. Aggregates: symbol info + callees + callers + complexity. " +
			"Returns type-aware results for Go repos (interface dispatch resolution). " +
			"Use instead of separate call_trace + symbol_search + code_graph calls. " +
			"Suggests similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UnderstandInput) (*mcp.CallToolResult, error) {
		return handleUnderstand(ctx, input, deps, sem)
	})
}

func handleUnderstand(ctx context.Context, input UnderstandInput, deps analyze.Deps, sem *SemanticDeps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	matches := compound.FindSymbol(cg.Symbols, input.Symbol)

	if len(matches) == 0 {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(fmt.Sprintf("<response tool=\"understand\">\n"+
				"  <error>%s</error>\n%s\n</response>", escapeXML(msg), suggestions)), nil
		}
		return errResult(msg), nil
	}

	if len(matches) > 1 {
		return understandAmbiguousResult(input.Symbol, matches)
	}

	result := compound.Understand(matches[0], cg, compound.UnderstandOpts{
		IncludeCallers: input.IncludeCallers,
	})

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	return textResult(string(data)), nil
}

// understandAmbiguousResult returns a JSON response listing ambiguous symbol matches.
func understandAmbiguousResult(name string, symbols []*parser.Symbol) (*mcp.CallToolResult, error) {
	refs := make([]*compound.MatchRef, 0, len(symbols))
	for _, sym := range symbols {
		refs = append(refs, &compound.MatchRef{
			Name:     sym.Name,
			Kind:     string(sym.Kind),
			File:     sym.File,
			Line:     sym.StartLine,
			Receiver: sym.Receiver,
		})
	}

	type ambiguousResponse struct {
		Error   string              `json:"error"`
		Matches []*compound.MatchRef `json:"matches"`
	}
	resp := ambiguousResponse{
		Error:   fmt.Sprintf("symbol %q is ambiguous (%d matches) — provide more context via focus= or use a qualified name", name, len(symbols)),
		Matches: refs,
	}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	return textResult(string(data)), nil
}
