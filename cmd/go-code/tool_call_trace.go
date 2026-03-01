package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/prompts"
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
	Nodes         []xmlTraceNode `xml:"node"`
	Narrative     xmlCDATA       `xml:"narrative,omitempty"`
}

type xmlTraceNode struct {
	Kind      string         `xml:"kind,attr"`
	Name      string         `xml:"name,attr"`
	File      string         `xml:"file,attr"`
	Line      uint32         `xml:"line,attr"`
	End       uint32         `xml:"end,attr,omitempty"`
	CallLine  uint32         `xml:"callLine,attr,omitempty"`
	Cycle     bool           `xml:"cycle,attr,omitempty"`
	Signature xmlCDATA       `xml:"signature,omitempty"`
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
				xn.Signature = xmlCDATA{Inner: wrapCDATA(n.Symbol.Signature)}
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
	Focus     string `json:"focus,omitempty" jsonschema_description:"Subdirectory to limit analysis to"`
	Language  string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
}

type callTraceOutput struct {
	Symbol    string                    `json:"symbol"`
	Direction string                    `json:"direction"`
	CallTree  []callgraph.CallChainNode `json:"call_tree"`
	Stats     traceStats                `json:"stats"`
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

// registerCallTrace registers the call_trace MCP tool.
func registerCallTrace(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcp.AddTool(server, &mcp.Tool{
		Name: "call_trace",
		Description: "Trace the execution path of a function through a codebase. " +
			"Shows what happens when a function is called (callees) or who calls it (callers). " +
			"Returns a call tree with resolved cross-file references and an LLM-generated " +
			"narrative explanation of the execution flow.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallTraceInput) (*mcp.CallToolResult, any, error) {
		return handleCallTrace(ctx, input, deps, outputDir)
	})
}

func handleCallTrace(ctx context.Context, input CallTraceInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil, nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
	}
	defer cleanup()

	depth := input.Depth
	if depth <= 0 {
		depth = defaultTraceDepth
	}

	direction := input.Direction
	if direction == "" {
		direction = "callees"
	}

	result, err := callgraph.TraceRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Symbol:   input.Symbol,
		Focus:    input.Focus,
		Language: input.Language,
		Opts: callgraph.TraceOpts{
			Direction: direction,
			MaxDepth:  depth,
		},
	})
	if err != nil {
		return errResult(fmt.Sprintf("trace: %s", err)), nil, nil
	}

	if result.Root == nil {
		return errResult(fmt.Sprintf("symbol %q not found in repository", input.Symbol)), nil, nil
	}

	output := buildCallTraceOutput(ctx, input.Symbol, direction, result, deps)

	resp := xmlTraceResponse{
		Trace: xmlTrace{
			Symbol:        output.Symbol,
			Direction:     output.Direction,
			TotalNodes:    output.Stats.TotalNodes,
			MaxDepth:      output.Stats.MaxDepth,
			Resolved:      output.Stats.Resolved,
			Unresolved:    output.Stats.Unresolved,
			ResolvedRatio: output.Stats.ResolvedRatio,
			Nodes:         convertTraceNodes(output.CallTree),
		},
	}
	if output.Narrative != "" {
		resp.Trace.Narrative = xmlCDATA{Inner: wrapCDATA(output.Narrative)}
	}

	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
	}

	return largeTextResult(xml.Header+string(data), "call_trace", outputDir), nil, nil
}

func buildCallTraceOutput(ctx context.Context, symbol, direction string, result *callgraph.TraceResult, deps analyze.Deps) callTraceOutput {
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
	}

	// LLM narrative (optional, non-fatal).
	if deps.LLM != nil && result.TotalNodes > 1 {
		treeJSON, _ := json.Marshal(result.Tree)
		prompt := fmt.Sprintf("Entry function: %s\nDirection: %s\n\nCall tree:\n%s",
			symbol, direction, string(treeJSON))
		narrative, narErr := deps.LLM.Complete(ctx, prompts.SystemPromptCallTrace, prompt)
		if narErr == nil {
			output.Narrative = narrative
		}
	}

	return output
}
