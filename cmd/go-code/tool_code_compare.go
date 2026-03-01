package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeCompareInput is the input schema for the code_compare tool.
type CodeCompareInput struct {
	RepoA    string `json:"repo_a" jsonschema_description:"First repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	RepoB    string `json:"repo_b" jsonschema_description:"Second repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Query    string `json:"query,omitempty" jsonschema_description:"What to compare — quality aspects, architectural patterns, specific concerns (default: general comparison)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path filter to limit comparison scope (e.g. internal/auth, pkg/api). NOT for topic focus — use query for that."`
	Language string `json:"language,omitempty" jsonschema_description:"Limit comparison to files of this language (e.g. go, python, rust)"`
}

// registerCodeCompare registers the code_compare MCP tool.
func registerCodeCompare(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcp.AddTool(server, &mcp.Tool{
		Name: "code_compare",
		Description: "Compare two code repositories to find the better implementation. " +
			"Analyzes architecture, code quality, patterns, and identifies missing features. " +
			"Returns JSON with quality verdicts, coverage gaps, architecture insights, " +
			"metrics, and actionable recommendations. Works cross-language.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeCompareInput) (*mcp.CallToolResult, any, error) {
		if input.RepoA == "" || input.RepoB == "" {
			return errResult("repo_a and repo_b are required"), nil, nil
		}
		if input.Query == "" {
			input.Query = "Compare architecture, code quality, patterns, and identify missing features"
		}

		rootA, cleanupA, err := resolveRoot(ctx, input.RepoA, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_a: %s", err)), nil, nil
		}
		defer cleanupA()

		rootB, cleanupB, err := resolveRoot(ctx, input.RepoB, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_b: %s", err)), nil, nil
		}
		defer cleanupB()

		result, err := compare.CompareRepos(ctx, compare.CompareInput{
			RootA: rootA,
			RootB: rootB,
			Query: input.Query,
			Opts: compare.SnapshotOpts{
				Focus:    input.Focus,
				Language: input.Language,
			},
		}, deps.LLM)
		if err != nil {
			return errResult(fmt.Sprintf("compare: %s", err)), nil, nil
		}

		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal result: %s", err)), nil, nil
		}

		return largeTextResult(string(data), "code_compare", outputDir), nil, nil
	})
}
