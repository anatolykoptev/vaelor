package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/semhealth"
)

// Default and validation constants for find_duplicates.
const (
	// defaultDupLimit is the maximum number of duplicate groups returned when
	// the caller does not specify a limit. Matches the maxExactDupPairs cap in
	// embeddings/store_dup.go to keep output volume consistent across tiers.
	defaultDupLimit = 50

	// validTiers are the accepted tier filter values. Kept as constants so callers
	// cannot pass free-form strings into the formatted output.
	dupTierExact     = "exact"
	dupTierVeryClose = "very-close"
	dupTierRelated   = "related"
)

// tierOrder is the display sequence for tier section headers. exact first,
// very-close second, related last — matches AnalyzeTriage's merge order.
var tierOrder = []string{dupTierExact, dupTierVeryClose, dupTierRelated}

// FindDuplicatesInput is the input schema for the find_duplicates tool.
type FindDuplicatesInput struct {
	Repo            string `json:"repo" jsonschema_description:"Repository path or identifier to scan for intra-repo semantic duplicates (GitHub slug, full URL, or absolute local path)"`
	IncludeSameFile bool   `json:"include_same_file,omitempty" jsonschema_description:"Include same-file near-duplicates (default false — overloads and helpers in the same file are excluded)"`
	Tier            string `json:"tier,omitempty" jsonschema_description:"Filter output to a single tier: exact | very-close | related (default: all tiers)"`
	Limit           int    `json:"limit,omitempty" jsonschema_description:"Maximum number of duplicate groups to report (default 50)"`
}

// registerFindDuplicates registers the find_duplicates MCP tool.
//
// The tool surfaces intra-repo semantic duplicate candidates grouped in three tiers
// (exact body-hash clones, very-close, related) after the Phase-2 filter chain has
// removed false-positive classes:
//   - interface-implementation siblings (multiple types implementing the same interface)
//   - caller/callee CALLS pairs (look similar due to shared vocabulary, not duplication)
//   - test-mirror functions (test code mirrors production code by design)
//   - same-file overloads (within-file helper variants, unless include_same_file=true)
//   - low-signal symbol kinds (const, var, field, import)
//
// The tool is operator-invoked — not run automatically — because it queries pgvector
// with a self-join (O(n²) up to semhealthMaxFuncs guard). Output lists candidate pairs
// for human or agent review; the tool does not auto-flag or auto-fix.
//
// Disabled (no-op) when DATABASE_URL is not configured or the embedding store is nil.
func registerFindDuplicates(server *mcp.Server, deps SemanticDeps) {
	if deps.Store == nil {
		slog.Info("find_duplicates: DATABASE_URL not set or EMBED_URL not configured — tool disabled")
		return
	}

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "find_duplicates",
		Description: "Operator-invoked: find pairs of symbols in ONE repo that are semantically near-identical. " +
			"Targets the 'agent re-implemented ProcessX as HandleX' class of drift, which is invisible to " +
			"byte-level diff but shows high cosine similarity in the embedding space. " +
			"Three tiers: 'exact' (body-hash clones — textually identical), 'very-close' (similarity ≥ 0.88, " +
			"strong refactor candidate), 'related' (similarity ≥ 0.80, worth human review). " +
			"False-positive filters run before output: interface-implementation siblings, caller/callee CALLS " +
			"pairs, test mirrors, same-file overloads, and low-signal kinds (const/var/field/import). " +
			"The filter-drop breakdown in the output header shows which filter is doing the most work — " +
			"use it to assess precision. " +
			"Output is a triage list for human or agent review, not an auto-flag list. " +
			"Requires EMBED_URL + DATABASE_URL. Skips repos with > 5000 indexed symbols (O(n²) self-join guard).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in FindDuplicatesInput) (*mcp.CallToolResult, error) {
		return handleFindDuplicates(ctx, deps, in)
	})
}

// handleFindDuplicates is the extracted handler, callable from tests.
func handleFindDuplicates(ctx context.Context, deps SemanticDeps, in FindDuplicatesInput) (*mcp.CallToolResult, error) {
	if in.Repo == "" {
		return errResult("find_duplicates: repo is required"), nil
	}

	// Validate the tier filter early so we fail fast before any DB work.
	if in.Tier != "" && in.Tier != dupTierExact && in.Tier != dupTierVeryClose && in.Tier != dupTierRelated {
		return errResult(fmt.Sprintf(
			"find_duplicates: unknown tier %q — valid values: exact, very-close, related",
			in.Tier,
		)), nil
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultDupLimit
	}

	root, cleanup, err := resolveRoot(ctx, in.Repo, "", deps.AnalyzeDeps)
	if err != nil {
		return nil, fmt.Errorf("find_duplicates: %w", err)
	}
	defer cleanup()

	repoKey := codegraph.GraphNameFor(root)

	// Compute totalFuncs via embeddings.Store.Stats — a single COUNT(*) GROUP BY
	// query that returns the total symbol count for this repo_key. This over-counts
	// relative to countFuncs (which filters to function/method kinds only), making
	// it a conservative upper bound: if the total symbol count exceeds
	// semhealthMaxFuncs (5000), there are definitely too many functions to self-join
	// safely. The cost is a possible false skip on repos with 5000 total symbols but
	// fewer than 5000 functions; the benefit is avoiding an expensive full parse.
	//
	// compare.BuildSnapshot + countFuncs would be more accurate but adds 30-90s of
	// AST parsing per call — unacceptable for an operator-invoked MCP tool.
	totalSymbols := 0
	if stats, statsErr := deps.Store.Stats(ctx); statsErr == nil {
		totalSymbols = stats[repoKey]
	} else {
		slog.Warn("find_duplicates: Stats failed, using 1 as conservative default",
			slog.String("repo_key", repoKey),
			slog.Any("error", statsErr),
		)
		// Use 1 as a non-zero sentinel so AnalyzeTriage is not gated by totalFuncs==0.
		// The semhealthMaxFuncs guard inside AnalyzeTriage handles large repos.
		totalSymbols = 1
	}

	// Typed-nil guard: a nil *embeddings.Expander assigned to the GraphPairFilter
	// interface creates a non-nil interface wrapping a nil pointer, which would panic
	// inside the AGE graph filters. Explicit nil preserves graceful-degradation
	// (filter is a no-op when gf is nil). Mirror of collectSemanticDupGroups in
	// tool_code_health_stages.go.
	var gf semhealth.GraphPairFilter
	if deps.Expander != nil {
		gf = deps.Expander
	}

	res := semhealth.AnalyzeTriage(
		ctx,
		deps.Store,
		gf,
		repoKey, // graphName == repoKey for AGE
		repoKey,
		totalSymbols,
		semhealth.TriageOpts{IncludeSameFile: in.IncludeSameFile, Root: root},
	)

	if res == nil {
		return textResult(fmt.Sprintf(
			"find_duplicates: repo not indexed or no embeddings for %q — run semantic indexing first",
			in.Repo,
		)), nil
	}

	return textResult(formatTriage(res, in.Tier, limit)), nil
}

// formatTriage renders a TriageResult as a human-readable triage report.
//
// Format:
//
//	candidates=N reported=M (exact=a very-close=b related=c)  filtered: tests=x same_file=y kind=z calls_edge=w interface_sibling=v
//
//	=== exact ===
//	symA (file:line, kind) ↔ symB (file:line, kind)  [exact]  sim=1.00
//
//	=== very-close ===
//	...
//
// When res.TimedOut is true, a warning line is prepended:
//
//	⚠ search incomplete (some queries timed out) — results may be partial
//
// tierFilter, when non-empty, restricts output to groups of that tier.
// limit caps the total number of groups rendered.
//
// The function is pure (no side effects) to make it unit-testable without
// a live DB or context.
func formatTriage(res *semhealth.TriageResult, tierFilter string, limit int) string {
	if res == nil || (len(res.Groups) == 0 && res.Candidates == 0) {
		if res != nil && res.TimedOut {
			return "⚠ search incomplete (some queries timed out) — results may be partial\nno semantic duplicates found in partial result set"
		}
		return "no semantic duplicates found (no candidates returned by pgvector)"
	}

	groups := filterAndLimitGroups(res.Groups, tierFilter, limit)

	var sb strings.Builder
	if res.TimedOut {
		fmt.Fprint(&sb, "⚠ search incomplete (some queries timed out) — results may be partial\n")
	}
	if len(groups) == 0 {
		fmt.Fprintf(&sb, "no semantic duplicates found in %q tier (candidates=%d before filtering)", tierFilter, res.Candidates)
		return sb.String()
	}

	fmt.Fprint(&sb, formatTriageSummary(res, tierFilter))
	fmt.Fprint(&sb, formatTriageTiers(groups))
	return sb.String()
}

// filterAndLimitGroups applies the tier filter and limit cap to a group slice.
func filterAndLimitGroups(groups []semhealth.DupGroup, tierFilter string, limit int) []semhealth.DupGroup {
	if tierFilter != "" {
		filtered := groups[:0:0]
		for _, g := range groups {
			if g.Tier == tierFilter {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}
	if limit <= 0 {
		limit = defaultDupLimit
	}
	if len(groups) > limit {
		groups = groups[:limit]
	}
	return groups
}

// formatTriageSummary renders the header line with candidate and filter-drop counts.
func formatTriageSummary(res *semhealth.TriageResult, tierFilter string) string {
	reported := countReported(res.Groups, tierFilter)
	exact := res.ReportedByTier[dupTierExact]
	veryClose := res.ReportedByTier[dupTierVeryClose]
	related := res.ReportedByTier[dupTierRelated]

	filterParts := buildFilterParts(res.Dropped)

	return fmt.Sprintf(
		"candidates=%d reported=%d (exact=%d very-close=%d related=%d)  filtered: %s\n",
		res.Candidates, reported, exact, veryClose, related,
		strings.Join(filterParts, " "),
	)
}

// countReported returns the number of groups matching tierFilter, or all groups
// when tierFilter is empty.
func countReported(groups []semhealth.DupGroup, tierFilter string) int {
	if tierFilter == "" {
		return len(groups)
	}
	n := 0
	for _, g := range groups {
		if g.Tier == tierFilter {
			n++
		}
	}
	return n
}

// buildFilterParts converts the Dropped map to sorted "key=value" strings.
func buildFilterParts(dropped map[string]int) []string {
	keys := make([]string, 0, len(dropped))
	for k := range dropped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, dropped[k]))
	}
	return parts
}

// formatTriageTiers renders group lines organised by tier in canonical order.
func formatTriageTiers(groups []semhealth.DupGroup) string {
	byTier := make(map[string][]semhealth.DupGroup, 3) //nolint:mnd
	for _, g := range groups {
		byTier[g.Tier] = append(byTier[g.Tier], g)
	}

	var sb strings.Builder
	for _, tier := range tierOrder {
		tierGroups := byTier[tier]
		if len(tierGroups) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n=== %s ===\n", tier)
		for _, g := range tierGroups {
			fmt.Fprint(&sb, formatDupGroupLine(g))
		}
	}
	return sb.String()
}

// formatDupGroupLine renders a single DupGroup as a triage line.
//
// For a 2-symbol group (the common case) the format is:
//
//	symA (file:line, kind) ↔ symB (file:line, kind)  [tier]  sim=0.93
//
// For groups with more than 2 symbols, each symbol is listed on its own
// indented line followed by the shared tier and similarity.
func formatDupGroupLine(g semhealth.DupGroup) string {
	sim := fmt.Sprintf("%.2f", g.AvgSimilarity)

	if len(g.Symbols) == 2 { //nolint:mnd
		a, b := g.Symbols[0], g.Symbols[1]
		return fmt.Sprintf(
			"  %s (%s:%d, %s) ↔ %s (%s:%d, %s)  [%s]  sim=%s\n",
			a.Name, a.File, a.Line, a.Kind,
			b.Name, b.File, b.Line, b.Kind,
			g.Tier, sim,
		)
	}

	// Multi-member group (3+ symbols via union-find chain).
	var sb strings.Builder
	fmt.Fprintf(&sb, "  [%s] sim=%s group (%d symbols):\n", g.Tier, sim, len(g.Symbols))
	for _, s := range g.Symbols {
		fmt.Fprintf(&sb, "    %s (%s:%d, %s)\n", s.Name, s.File, s.Line, s.Kind)
	}
	return sb.String()
}
