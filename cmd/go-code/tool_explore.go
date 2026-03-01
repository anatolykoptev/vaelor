package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/explore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExploreInput is the input schema for the explore tool.
type ExploreInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory or glob to focus analysis on"`
}

func registerExplore(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "explore",
		Description: "Quick structured overview of a repository. " +
			"Returns file/symbol counts, language breakdown, top symbols by call frequency, " +
			"dead code summary, and package list. " +
			"Use as a first step when encountering an unfamiliar codebase. " +
			"Fast (no LLM calls) — purely static analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ExploreInput) (*mcp.CallToolResult, any, error) {
		return handleExplore(ctx, input, deps)
	})
}

func handleExplore(ctx context.Context, input ExploreInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
	}
	defer cleanup()

	result, err := explore.Run(ctx, explore.Input{
		Root:     root,
		Language: input.Language,
		Focus:    input.Focus,
	})
	if err != nil {
		return errResult(fmt.Sprintf("explore: %s", err)), nil, nil
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
