package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/coupling"
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
	MinLift     float64 `json:"min_lift,omitempty"       jsonschema_description:"Optional raw-lift pre-filter floor (default 0 = no filter). Ranking is by Wilson lower bound on directional confidence — not affected by min_lift. Raise min_pairs for higher-confidence pairs."`
}

// FederatedCoChangeResult is the JSON payload returned by the federated_cochange tool.
type FederatedCoChangeResult struct {
	Pairs []coupling.VerifiedPair `json:"pairs"`
	Meta  mcpmeta.Envelope        `json:"_meta"`
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

	rawPairs := federate.CrossRepoCoChange(ctx, repos, window, minPairs, args.MinLift)

	// Build slug→root map for stage-2 route verification.
	roots := make(map[string]string, len(repos))
	for _, r := range repos {
		roots[r.Slug] = r.Root
	}
	verified := coupling.VerifyPairs(ctx, rawPairs, roots, coupling.NewRouteVerifier())

	// Normalize nil to empty slice so the JSON wire contract is always "pairs": []
	// not "pairs": null. MCP consumers (JS/Python) iterate pairs directly and
	// throw on null.
	if verified == nil {
		verified = []coupling.VerifiedPair{}
	}
	out := FederatedCoChangeResult{
		Pairs: verified,
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
		Description: "Find files in DIFFERENT repos that change together (cross-repo co-change) across a workspace. Ranked by Wilson lower bound on directional confidence (support-aware, continuous, never saturates): a thin coincidence (co=2, n=2) ranks well below a well-supported coupling (co=8, n=10) because Wilson penalizes small sample sizes — more evidence always wins. Ubiquitous stop-word files (CHANGELOGs, lockfiles, generated files touched in >85% of windows) are filtered out as noise before scoring. g2/significance are informational (un-capped Dunning log-likelihood); confidence_level derives from the Wilson score. min_lift is an optional raw effect-size pre-filter (not emitted in results). repos='all' | 'acme-*' | a repo name. Surfaces hidden coupling, e.g. a signaling change in one repo that needs a synchronized edit in another.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args FederatedCoChangeArgs) (*mcp.CallToolResult, error) {
		return handleFederatedCoChangeCore(ctx, args, deps)
	})
	_ = cfg // cfg reserved for future use (e.g. WorkspaceDir override)
}
