// Package impact computes blast radius for changing a symbol.
package impact

import (
	"context"
	"log/slog"
	"sort"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// Blast radius classification thresholds.
const (
	lowMaxCallers  = 5
	lowMaxPackages = 2
	medMaxCallers  = 20
	medMaxPackages = 5
)

// Options configures the impact analysis.
type Options struct {
	MaxDepth int
	OxCodes  *oxcodes.Client  // optional: enables hidden caller search
	Root     string           // required when OxCodes is set
	Language string           // optional: limit search to this language
	Refs     graphx.CrossRefs // optional: enables TESTED_BY edge lookup
	Repo     string           // required when Refs is set (repo key for graph queries)
}

// AffectedSymbol represents a caller that would be affected by a change.
type AffectedSymbol struct {
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Package    string  `json:"package"`
	Distance   int     `json:"distance"`
	Confidence float64 `json:"confidence"`
	Community  int     `json:"community"`
}

// Result is the output of an impact analysis.
type Result struct {
	Symbol             string           `json:"symbol"`
	Found              bool             `json:"found"`
	DirectCallers      []AffectedSymbol `json:"direct_callers"`
	TransitiveCallers  []AffectedSymbol `json:"transitive_callers"`
	HiddenCallers      []HiddenCaller   `json:"hidden_callers,omitempty"`
	TotalAffected      int              `json:"total_affected"`
	AffectedPackages   []string         `json:"affected_packages"`
	CommunitiesCrossed int              `json:"communities_crossed"`
	BlastRadius        string           `json:"blast_radius"` // none, low, medium, high
	RiskScore          float64          `json:"risk_score"`
	// TestsCovering are test functions that directly cover the target symbol.
	// Populated when the graph has TESTED_BY edges; empty otherwise.
	TestsCovering []graphx.SymbolRef `json:"tests_covering,omitempty"`
}

const (
	defaultMaxDepth       = 5
	minConfidence         = 0.1
	confidenceDecayPerHop = 0.2
	packageRiskMultiplier = 0.1
)

// Analyze computes blast radius for changing the named symbol.
// Uses reverse BFS from the target to find all callers.
// When opts.OxCodes is set, also searches for hidden callers via string references.
func Analyze(ctx context.Context, cg *callgraph.CallGraph, symbolName string, opts Options) *Result {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}

	result := &Result{Symbol: symbolName}

	target := findTarget(cg.Symbols, symbolName)
	if target == nil {
		// Fall through to file-level USES index for Astro components.
		// symbolName may be a relative file path (e.g. "src/components/Breadcrumbs.astro").
		if len(cg.UsesIndex) > 0 {
			appendUsesCallers(cg.UsesIndex, symbolName, result)
		}
		if result.TotalAffected == 0 {
			result.BlastRadius = "none"
			return result
		}
		result.Found = true
		result.BlastRadius = classifyBlastRadius(result.TotalAffected, len(result.AffectedPackages))
		result.RiskScore = float64(result.TotalAffected)
		return result
	}
	result.Found = true

	communityMap := buildCommunityMap(cg)
	callerIndex := buildCallerIndex(cg.Edges)
	pkgSet := traverseCallers(target, callerIndex, communityMap, opts.MaxDepth, result)

	result.TotalAffected = len(result.DirectCallers) + len(result.TransitiveCallers)

	if opts.OxCodes != nil && opts.Root != "" {
		result.HiddenCallers = FindHiddenCallers(ctx, opts.OxCodes, opts.Root, symbolName, opts.Language)
		result.TotalAffected += len(result.HiddenCallers)
	}

	for pkg := range pkgSet {
		result.AffectedPackages = append(result.AffectedPackages, pkg)
	}
	sort.Strings(result.AffectedPackages)

	// Count distinct communities among affected callers.
	commSet := make(map[int]bool)
	for _, c := range result.DirectCallers {
		commSet[c.Community] = true
	}
	for _, c := range result.TransitiveCallers {
		commSet[c.Community] = true
	}
	result.CommunitiesCrossed = len(commSet)

	result.BlastRadius = classifyBlastRadius(result.TotalAffected, len(result.AffectedPackages))

	communityRisk := 0.0
	if result.CommunitiesCrossed > 1 {
		communityRisk = float64(result.CommunitiesCrossed-1) * 0.15
	}
	result.RiskScore = float64(result.TotalAffected) * (1.0 + float64(len(result.AffectedPackages))*packageRiskMultiplier + communityRisk)

	if opts.Refs != nil {
		// Graph is keyed by resolved Root (container path), not user-facing Repo.
		repoKey := opts.Root
		if repoKey == "" {
			repoKey = opts.Repo
		}
		if repoKey != "" {
			tests, err := opts.Refs.TestedBy(ctx, repoKey, target.Name, target.File)
			if err != nil {
				slog.Debug("impact: TestedBy lookup failed", slog.Any("error", err))
			} else {
				result.TestsCovering = tests
			}
		}
	}

	return result
}
