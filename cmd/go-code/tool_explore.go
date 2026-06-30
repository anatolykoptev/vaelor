package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/explore"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExploreInput is the input schema for the explore tool.
type ExploreInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope (e.g. internal/auth), or space-separated keywords to filter by path components (e.g. 'auth middleware')"`
}

func registerExplore(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "explore",
		Description: "Quick structured overview of a repository. " +
			"Returns file/symbol counts, language breakdown, top symbols by call frequency, " +
			"dead code summary, package list, health score (A-F), dependency freshness, and vulnerability count. " +
			"Use as a first step when encountering an unfamiliar codebase. " +
			"Fast (no LLM calls) — purely static analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ExploreInput) (*mcp.CallToolResult, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
		}
		defer cleanup()

		result, err := explore.Run(ctx, explore.Input{
			Root:     root,
			Language: input.Language,
			Focus:    input.Focus,
		})
		if err != nil {
			return errResult(fmt.Sprintf("explore: %s", err)), nil
		}

		type exploreFreshnessSummary struct {
			DepFreshnessRatio float64 `json:"dep_freshness_ratio"`
			VulnDeps          int     `json:"vuln_deps"`
			TotalDeps         int     `json:"total_deps"`
		}
		type exploreOutput struct {
			*explore.Result
			Freshness *exploreFreshnessSummary `json:"freshness,omitempty"`
		}

		var fresh *compare.FreshnessStats
		{
			fctx, fcancel := context.WithTimeout(ctx, 5*time.Second)
			defer fcancel()
			fresh, _, _ = compare.CollectFreshness(fctx, root)
		}

		output := exploreOutput{Result: result}
		if fresh != nil {
			output.Freshness = &exploreFreshnessSummary{
				DepFreshnessRatio: fresh.DepFreshnessRatio,
				VulnDeps:          fresh.VulnDeps,
				TotalDeps:         fresh.TotalDeps,
			}
		}

		data, err := json.Marshal(output)
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil
		}

		return textResult(string(data)), nil
	})
}
