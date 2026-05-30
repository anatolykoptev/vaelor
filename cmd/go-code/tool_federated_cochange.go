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
	Repos       string  `json:"repos"                    jsonschema_description:"Repo pattern: 'all', a glob like 'oxpulse-*', or a single repo name/absolute path"`
	WindowHours int     `json:"window_hours,omitempty"   jsonschema_description:"Co-change time window in hours (default 24)"`
	MinPairs    int     `json:"min_pairs,omitempty"      jsonschema_description:"Minimum co-occurrences to report a pair (default 2)"`
	MinLift     float64 `json:"min_lift,omitempty"       jsonschema_description:"Optional raw-lift pre-filter floor (default 0 = no floor). Ranking is by support tier first (co_changes >= 3 outrank low-support pairs) then G² significance; min_lift only pre-filters by raw effect-size before ranking. Low co-occurrence counts yield noisier values — raise min_pairs for higher-confidence pairs."`
}

// FederatedCoChangeResult is the JSON payload returned by the federated_cochange tool.
type FederatedCoChangeResult struct {
	Pairs []federate.CrossPair `json:"pairs"`
	Meta  mcpmeta.Envelope     `json:"_meta"`
}

// handleFederatedCoChangeCore is the testable core of the federated_cochange tool.
func handleFederatedCoChangeCore(ctx context.Context, args FederatedCoChangeArgs, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if args.Repos == "" {
		return errResult("repos is required (e.g. 'all', 'oxpulse-*', or a repo name)"), nil
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
		Description: "Find files in DIFFERENT repos that change together (cross-repo co-change) across a workspace. Ranked by log-likelihood (G²) co-change significance, well-supported pairs first: pairs with co_changes >= 3 always outrank low-support pairs regardless of raw G², preventing perfect rare coincidences from burying loose genuine couplings. Within each support tier, G² ranks by statistical significance. min_lift is an optional raw effect-size pre-filter (not emitted in results). repos='all' | 'oxpulse-*' | a repo name. Surfaces hidden coupling, e.g. a signaling change in one repo that needs a synchronized edit in another.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args FederatedCoChangeArgs) (*mcp.CallToolResult, error) {
		return handleFederatedCoChangeCore(ctx, args, deps)
	})
	_ = cfg // cfg reserved for future use (e.g. WorkspaceDir override)
}
