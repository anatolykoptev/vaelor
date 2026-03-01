package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DeadCodeInput is the input schema for the dead_code tool.
type DeadCodeInput struct {
	Repo            string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language        string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python)"`
	IncludeExported bool   `json:"include_exported,omitempty" jsonschema_description:"Include exported/public functions (usually false positives, default: false)"`
	Focus           string `json:"focus,omitempty" jsonschema_description:"Optional focus area for the LLM narrative"`
}

func registerDeadCode(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "dead_code",
		Description: "Detect functions and methods with zero incoming calls. " +
			"Filters out entry points (main, init), test functions, and exported symbols " +
			"to reduce false positives. Shows confidence levels: high (unexported), " +
			"medium (methods, may satisfy interfaces), low (exported).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeadCodeInput) (*mcp.CallToolResult, any, error) {
		return handleDeadCode(ctx, input, deps)
	})
}

func handleDeadCode(ctx context.Context, input DeadCodeInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
	}
	defer cleanup()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil, nil
	}

	result := deadcode.Analyze(cg, deadcode.Options{
		IncludeExported: input.IncludeExported,
	})

	// Build output with optional LLM narrative.
	type deadCodeOutput struct {
		*deadcode.Result
		Narrative string `json:"narrative,omitempty"`
	}
	output := deadCodeOutput{Result: result}

	if deps.LLM != nil && result.DeadCount > 0 {
		resultJSON, _ := json.Marshal(result)
		prompt := "Repository dead code analysis:\n" + string(resultJSON)
		if input.Focus != "" {
			prompt = fmt.Sprintf("Focus area: %s\n\n%s", input.Focus, prompt)
		}
		narrative, narErr := deps.LLM.Complete(ctx, prompts.SystemPromptDeadCode, prompt)
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
}
