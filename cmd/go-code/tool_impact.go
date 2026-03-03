package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/prompts"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ImpactInput is the input schema for the impact_analysis tool.
type ImpactInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol   string `json:"symbol" jsonschema_description:"Function or method name to analyze impact for"`
	Depth    int    `json:"depth,omitempty" jsonschema_description:"Max traversal depth for transitive callers (default 5, max 10)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
}

const (
	defaultImpactDepth = 5
	maxImpactDepth     = 10
)

func registerImpact(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "impact_analysis",
		Description: "Analyze the blast radius of changing a function or method. " +
			"Shows direct callers, transitive callers, affected packages, " +
			"and risk classification (low/medium/high). " +
			"Useful before refactoring to understand what might break.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ImpactInput) (*mcp.CallToolResult, error) {
		return handleImpact(ctx, input, deps)
	})
}

func handleImpact(ctx context.Context, input ImpactInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
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
		depth = defaultImpactDepth
	}
	if depth > maxImpactDepth {
		depth = maxImpactDepth
	}

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	result := impact.Analyze(cg, input.Symbol, impact.Options{MaxDepth: depth})

	// Build output with optional narrative.
	type impactOutput struct {
		*impact.Result
		Narrative string `json:"narrative,omitempty"`
	}
	output := impactOutput{Result: result}

	if result.TotalAffected > 0 {
		prefix := fmt.Sprintf("Changed symbol: %s\n\nImpact analysis:\n", input.Symbol)
		output.Narrative = generateNarrative(ctx, deps.LLM, prompts.SystemPromptImpact, result, prefix)
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return textResult(string(data)), nil
}
