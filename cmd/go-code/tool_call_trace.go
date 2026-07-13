package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/prompts"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlTraceResponse struct {
	XMLName xml.Name `xml:"response"`
	Trace   xmlTrace `xml:"trace"`
}

type xmlTrace struct {
	Symbol        string         `xml:"symbol,attr"`
	Direction     string         `xml:"direction,attr"`
	TotalNodes    int            `xml:"totalNodes,attr"`
	MaxDepth      int            `xml:"maxDepth,attr"`
	Resolved      int            `xml:"resolved,attr"`
	Unresolved    int            `xml:"unresolved,attr"`
	ResolvedRatio float64        `xml:"resolvedRatio,attr"`
	Tier          string         `xml:"tier,attr,omitempty"`
	Nodes         []xmlTraceNode `xml:"node"`
	Narrative     *xmlCDATA      `xml:"narrative,omitempty"`
}

type xmlTraceNode struct {
	Kind      string         `xml:"kind,attr"`
	Name      string         `xml:"name,attr"`
	File      string         `xml:"file,attr"`
	Line      uint32         `xml:"line,attr"`
	End       uint32         `xml:"end,attr,omitempty"`
	CallLine  uint32         `xml:"callLine,attr,omitempty"`
	Cycle     bool           `xml:"cycle,attr,omitempty"`
	Signature *xmlCDATA      `xml:"signature,omitempty"`
	Children  []xmlTraceNode `xml:"node,omitempty"`
}

func convertTraceNodes(nodes []callgraph.CallChainNode) []xmlTraceNode {
	result := make([]xmlTraceNode, len(nodes))
	for i, n := range nodes {
		xn := xmlTraceNode{
			CallLine: n.CallLine,
			Cycle:    n.Cycle,
		}
		if n.Symbol != nil {
			xn.Kind = string(n.Symbol.Kind)
			xn.Name = n.Symbol.Name
			xn.File = n.Symbol.File
			xn.Line = n.Symbol.StartLine
			xn.End = n.Symbol.EndLine
			if n.Symbol.Signature != "" {
				xn.Signature = &xmlCDATA{Inner: wrapCDATA(n.Symbol.Signature)}
			}
		}
		if len(n.Children) > 0 {
			xn.Children = convertTraceNodes(n.Children)
		}
		result[i] = xn
	}
	return result
}

// CallTraceInput is the input schema for the call_trace tool.
type CallTraceInput struct {
	Repo      string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Symbol    string `json:"symbol" jsonschema_description:"Function or method name to trace (e.g. CompareRepos, Server.Serve)"`
	Depth     int    `json:"depth,omitempty" jsonschema_description:"Max trace depth (default 5, max 10)"`
	Direction string `json:"direction,omitempty" jsonschema_description:"Trace direction: callees (what does X call?) or callers (who calls X?). Default: callees"`
	Focus     string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`
	Language  string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
	Compact   bool   `json:"compact,omitempty" jsonschema_description:"When true, return only the call tree without LLM narrative (faster, fewer tokens)"`

	FieldAccess bool `json:"field_access,omitempty" jsonschema_description:"When true, include heuristic argument-reference call sites (struct field accesses, identifier args) as callees even when they don't resolve to a known function — legacy permissive behaviour. Default false: only true call expressions and resolved function references are reported."`
}

type callTraceOutput struct {
	Symbol    string                    `json:"symbol"`
	Direction string                    `json:"direction"`
	CallTree  []callgraph.CallChainNode `json:"call_tree"`
	Stats     traceStats                `json:"stats"`
	Tier      string                    `json:"tier,omitempty"`
	Narrative string                    `json:"narrative,omitempty"`
}

type traceStats struct {
	TotalNodes    int     `json:"total_nodes"`
	MaxDepth      int     `json:"max_depth"`
	Resolved      int     `json:"resolved"`
	Unresolved    int     `json:"unresolved"`
	ResolvedRatio float64 `json:"resolved_ratio"`
}

const defaultTraceDepth = 5

// normalizeCallTraceDirection maps the tool's documented direction values
// (forward/reverse/callees/callers) to the canonical values expected by
// internal/callgraph.Trace ("callers" for reverse, "callees" otherwise).
func normalizeCallTraceDirection(direction string) string {
	switch direction {
	case "reverse", "callers":
		return "callers"
	case "forward", "callees":
		return "callees"
	default:
		return "callees"
	}
}

// registerCallTrace registers the call_trace MCP tool.
func registerCallTrace(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "call_trace",
		Description: "Trace the execution path of a function through a codebase. " +
			"Shows what happens when a function is called (callees) or who calls it (callers). " +
			"Returns a call tree with resolved cross-file references and an LLM-generated " +
			"narrative explanation of the execution flow. " +
			"Type-aware for Go repos: resolves interface calls to concrete implementations via go/types. " +
			"Suggests semantically similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallTraceInput) (*mcp.CallToolResult, error) {
		return handleCallTrace(ctx, input, deps, sem, outputDir)
	})
}

func handleCallTrace(ctx context.Context, input CallTraceInput, deps analyze.Deps, sem *SemanticDeps, outputDir string) (*mcp.CallToolResult, error) {
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

	depth := input.Depth
	if depth <= 0 {
		depth = defaultTraceDepth
	}

	direction := normalizeCallTraceDirection(input.Direction)

	result, err := callgraph.TraceRepo(ctx, callgraph.TraceRepoInput{
		Root:               root,
		Symbol:             input.Symbol,
		Focus:              input.Focus,
		Language:           input.Language,
		IncludeFieldAccess: input.FieldAccess,
		Opts: callgraph.TraceOpts{
			Direction: direction,
			MaxDepth:  depth,
			CrossRefs: deps.Refs,
			Repo:      root,
		},
	})
	if err != nil {
		return errResult(fmt.Sprintf("trace: %s", err)), nil
	}

	if result.Root == nil {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(formatToolErrorWithSuggestions("call_trace", msg, suggestions)), nil
		}
		return errResult(msg), nil
	}

	// Speculative resolution: enrich unresolved call sites via ox-codes text search.
	if deps.OxCodes != nil && result.Unresolved > 0 {
		callgraph.ResolveSpeculative(ctx, deps.OxCodes, root, input.Language, result.Tree)
	}

	output := buildCallTraceOutput(ctx, input.Symbol, direction, result, deps, input.Compact)

	resp := xmlTraceResponse{
		Trace: xmlTrace{
			Symbol:        output.Symbol,
			Direction:     output.Direction,
			TotalNodes:    output.Stats.TotalNodes,
			MaxDepth:      output.Stats.MaxDepth,
			Resolved:      output.Stats.Resolved,
			Unresolved:    output.Stats.Unresolved,
			ResolvedRatio: output.Stats.ResolvedRatio,
			Tier:          output.Tier,
			Nodes:         convertTraceNodes(output.CallTree),
		},
	}
	if output.Narrative != "" {
		resp.Trace.Narrative = &xmlCDATA{Inner: wrapCDATA(output.Narrative)}
	}

	return xmlMarshalResult(resp, "call_trace", outputDir), nil
}

func buildCallTraceOutput(ctx context.Context, symbol, direction string, result *callgraph.TraceResult, deps analyze.Deps, compact bool) callTraceOutput {
	total := result.Resolved + result.Unresolved
	var ratio float64
	if total > 0 {
		ratio = float64(result.Resolved) / float64(total)
	}

	output := callTraceOutput{
		Symbol:    symbol,
		Direction: direction,
		CallTree:  result.Tree,
		Stats: traceStats{
			TotalNodes:    result.TotalNodes,
			MaxDepth:      result.MaxDepth,
			Resolved:      result.Resolved,
			Unresolved:    result.Unresolved,
			ResolvedRatio: ratio,
		},
		Tier: result.Tier,
	}

	// LLM narrative (optional, non-fatal). Skipped in compact mode.
	if !compact && result.TotalNodes > 1 {
		prefix := fmt.Sprintf("Entry function: %s\nDirection: %s\n\nCall tree:\n", symbol, direction)
		output.Narrative = generateNarrative(ctx, deps.LLM, prompts.SystemPromptCallTrace, result.Tree, prefix)
	}

	return output
}
