package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

// registerCallTrace registers the call_trace MCP tool.
func registerCallTrace(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "call_trace",
		Description: "Trace the execution path of a function through a codebase. " +
			"Shows what happens when a function is called (callees) or who calls it (callers). " +
			"Returns a call tree with resolved cross-file references and an LLM-generated " +
			"narrative explanation of the execution flow.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallTraceInput) (*mcp.CallToolResult, any, error) {
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
			depth = 5
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

		total := result.Resolved + result.Unresolved
		var ratio float64
		if total > 0 {
			ratio = float64(result.Resolved) / float64(total)
		}

		output := callTraceOutput{
			Symbol:    input.Symbol,
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
				input.Symbol, direction, string(treeJSON))
			narrative, narErr := deps.LLM.Complete(ctx, llm.SystemPromptCallTrace, prompt)
			if narErr == nil {
				output.Narrative = narrative
			}
		}

		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}
