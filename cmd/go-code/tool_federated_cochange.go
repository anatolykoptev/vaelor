package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/federate"
	"github.com/anatolykoptev/go-code/internal/mcpmeta"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// federatedCoChangeDefaultWindowHours is the default time window for co-change correlation.
	federatedCoChangeDefaultWindowHours = 24
	// federatedCoChangeDefaultMinPairs is the default minimum co-occurrences to report a pair.
	federatedCoChangeDefaultMinPairs = 2
)

// FederatedCoChangeArgs is the input schema for the federated_cochange tool.
type FederatedCoChangeArgs struct {
	Repos       string  `json:"repos"                    jsonschema_description:"Repo pattern: 'all', a glob like 'acme-*', or a single repo name/absolute path"`
	WindowHours int     `json:"window_hours,omitempty"   jsonschema_description:"Co-change time window in hours (default 24)"`
	MinPairs    int     `json:"min_pairs,omitempty"      jsonschema_description:"Minimum co-occurrences to report a pair (default 2)"`
	MinLift     float64 `json:"min_lift,omitempty"       jsonschema_description:"Optional lift floor (default 0 = no floor, rank by lift only). Raise to filter to stronger-than-chance coupling. Low co-occurrence counts (min_pairs) yield noisier lift — raise min_pairs for higher-confidence pairs."`
}

// FederatedCoChangeResult is the JSON payload returned by the federated_cochange tool.
type FederatedCoChangeResult struct {
	Pairs []federate.CrossPair `json:"pairs"`
	Meta  mcpmeta.Envelope     `json:"_meta"`
}

// handleFederatedCoChangeCore is the testable core of the federated_cochange tool.
func handleFederatedCoChangeCore(ctx context.Context, args FederatedCoChangeArgs, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if args.Repos == "" {
		return errResult("repos is required (e.g. 'all', 'acme-*', or a repo name)"), nil
	}

	window := args.WindowHours
	if window <= 0 {
		window = federatedCoChangeDefaultWindowHours
	}
	minPairs := args.MinPairs
	if minPairs <= 0 {
		minPairs = federatedCoChangeDefaultMinPairs
	}

	t0 := time.Now()

	repos, err := federate.ResolveRepos(args.Repos, deps.LocalRepoDirs)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repos %q: %v", args.Repos, err)), nil
	}
	if len(repos) < 2 {
		return errResult(fmt.Sprintf("federated co-change needs ≥2 repos, %q resolved to %d", args.Repos, len(repos))), nil
	}

	pairs := federate.CrossRepoCoChange(ctx, repos, window, minPairs, args.MinLift)
	// Normalize nil to empty slice so the JSON wire contract is always "pairs": []
	// not "pairs": null. MCP consumers (JS/Python) iterate pairs directly and
	// throw on null.
	if pairs == nil {
		pairs = []federate.CrossPair{}
	}
	out := FederatedCoChangeResult{
		Pairs: pairs,
		Meta:  mcpmeta.Wrap(time.Since(t0), ""),
	}
	body, merr := json.MarshalIndent(out, "", "  ")
	if merr != nil {
		return errResult(fmt.Sprintf("marshal: %s", merr)), nil
	}
	return textResult(string(body)), nil
}

// registerFederatedCoChange registers the federated_cochange tool on the MCP server.
func registerFederatedCoChange(server *mcp.Server, cfg Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name:        "federated_cochange",
		Description: "Find files in DIFFERENT repos that change together (cross-repo co-change) across a workspace. repos='all' | 'acme-*' | a repo name. Surfaces hidden coupling, e.g. a signaling change in one repo that needs a synchronized edit in another.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args FederatedCoChangeArgs) (*mcp.CallToolResult, error) {
		return handleFederatedCoChangeCore(ctx, args, deps)
	})
	_ = cfg // cfg reserved for future use (e.g. WorkspaceDir override)
}
