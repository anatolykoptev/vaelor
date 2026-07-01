package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/mcpmeta"
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
	FieldAccess    bool   `json:"field_access,omitempty" jsonschema_description:"When true, include heuristic argument-reference call sites (struct field accesses, identifier args) as callees even when they don't resolve to a known function — legacy permissive behaviour. Default false: only true call expressions and resolved function references are reported."`
}

func registerUnderstand(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps, graphStore *codegraph.Store) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "understand",
		Description: "Deep-dive into a single symbol. Aggregates: symbol info + callees + callers + complexity. " +
			"Returns type-aware results for Go repos (interface dispatch resolution). " +
			"Use instead of separate call_trace + symbol_search + code_graph calls. " +
			"Suggests similar symbols when the target is not found. " +
			"When a code_graph snapshot exists: shows tested_by (test functions covering this symbol) " +
			"and dead_code_score (CE reranker confidence that this function is unused, if applicable).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UnderstandInput) (*mcp.CallToolResult, error) {
		return handleUnderstand(ctx, input, deps, sem, graphStore)
	})
}

func handleUnderstand(ctx context.Context, input UnderstandInput, deps analyze.Deps, sem *SemanticDeps, graphStore *codegraph.Store) (*mcp.CallToolResult, error) {
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

	t0 := time.Now()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:               root,
		Language:           input.Language,
		IncludeFieldAccess: input.FieldAccess,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	matches := filterByFocus(compound.FindSymbol(cg.Symbols, input.Symbol), input.Focus)

	if len(matches) == 0 {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(formatToolErrorWithSuggestions("understand", msg, suggestions)), nil
		}
		return errResult(msg), nil
	}

	if len(matches) > 1 {
		return understandAmbiguousResult(input.Symbol, matches, deps.PathMappings)
	}

	opts := compound.UnderstandOpts{
		IncludeCallers: input.IncludeCallers,
		OxCodes:        deps.OxCodes,
		Root:           root,
		Repo:           input.Repo,
	}
	// Avoid the typed-nil-interface trap: only assign Learnings when the
	// store is actually configured, so opts.Learnings == nil behaves correctly.
	if deps.Learnings != nil {
		opts.Learnings = deps.Learnings
	}
	if deps.Graph != nil {
		opts.Graph = deps.Graph
	}
	if deps.Refs != nil {
		opts.Refs = deps.Refs
	}
	if graphStore != nil {
		opts.DeadCodeScores = graphStore
		opts.SymbolRanker = graphStore
	}
	result := compound.Understand(ctx, matches[0], cg, opts)

	// Reverse-map container-internal paths back to host-side paths so callers
	// see clickable file locations matching their local checkout.
	if len(deps.PathMappings) > 0 {
		result.Symbol.File = reverseToHost(result.Symbol.File, deps.PathMappings)
		for i := range result.Callees {
			result.Callees[i].File = reverseToHost(result.Callees[i].File, deps.PathMappings)
		}
		for i := range result.Callers {
			result.Callers[i].File = reverseToHost(result.Callers[i].File, deps.PathMappings)
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	// understand is a terminal call — no chaining hint.
	env := mcpmeta.Wrap(time.Since(t0), "")
	if sha := deps.IndexedSHA(ctx, codegraph.GraphNameFor(root)); sha != "" {
		env = mcpmeta.WithFreshness(env, root, sha)
	}
	return metaResult(string(data), env), nil
}

// filterByFocus narrows a symbol list to those whose file path matches focus.
// Strategy: exact → suffix → substring. Empty focus returns all symbols unchanged.
func filterByFocus(symbols []*parser.Symbol, focus string) []*parser.Symbol {
	if focus == "" {
		return symbols
	}
	var exact []*parser.Symbol
	for _, sym := range symbols {
		if sym.File == focus {
			exact = append(exact, sym)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	var suffix []*parser.Symbol
	for _, sym := range symbols {
		if strings.HasSuffix(sym.File, focus) {
			suffix = append(suffix, sym)
		}
	}
	if len(suffix) > 0 {
		return suffix
	}
	var sub []*parser.Symbol
	for _, sym := range symbols {
		if strings.Contains(sym.File, focus) {
			sub = append(sub, sym)
		}
	}
	return sub
}

// understandAmbiguousResult returns a JSON response listing ambiguous symbol matches.
// mappings is used to reverse-translate container-internal paths to host paths.
func understandAmbiguousResult(name string, symbols []*parser.Symbol, mappings []analyze.PathMapping) (*mcp.CallToolResult, error) {
	refs := make([]*compound.MatchRef, 0, len(symbols))
	for _, sym := range symbols {
		refs = append(refs, &compound.MatchRef{
			Name:     sym.Name,
			Kind:     string(sym.Kind),
			File:     reverseToHost(sym.File, mappings),
			Line:     sym.StartLine,
			Receiver: sym.Receiver,
		})
	}

	type ambiguousResponse struct {
		Error   string               `json:"error"`
		Matches []*compound.MatchRef `json:"matches"`
	}
	resp := ambiguousResponse{
		Error:   fmt.Sprintf("symbol %q is ambiguous (%d matches) — provide more context via focus= or use a qualified name", name, len(symbols)),
		Matches: refs,
	}
	return jsonMarshalResult(resp), nil
}
