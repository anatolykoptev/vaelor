// Package impact computes blast radius for changing a symbol.
package impact

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
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
	OxCodes  *oxcodes.Client // optional: enables hidden caller search
	Root     string          // required when OxCodes is set
	Language string          // optional: limit search to this language
}

// AffectedSymbol represents a caller that would be affected by a change.
type AffectedSymbol struct {
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Package    string  `json:"package"`
	Distance   int     `json:"distance"`
	Confidence float64 `json:"confidence"`
}

// Result is the output of an impact analysis.
type Result struct {
	Symbol            string           `json:"symbol"`
	Found             bool             `json:"found"`
	DirectCallers     []AffectedSymbol `json:"direct_callers"`
	TransitiveCallers []AffectedSymbol `json:"transitive_callers"`
	HiddenCallers     []HiddenCaller   `json:"hidden_callers,omitempty"`
	TotalAffected     int              `json:"total_affected"`
	AffectedPackages  []string         `json:"affected_packages"`
	BlastRadius       string           `json:"blast_radius"` // none, low, medium, high
	RiskScore         float64          `json:"risk_score"`
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
		result.BlastRadius = "none"
		return result
	}
	result.Found = true

	callerIndex := buildCallerIndex(cg.Edges)
	pkgSet := traverseCallers(target, callerIndex, opts.MaxDepth, result)

	result.TotalAffected = len(result.DirectCallers) + len(result.TransitiveCallers)

	if opts.OxCodes != nil && opts.Root != "" {
		result.HiddenCallers = FindHiddenCallers(ctx, opts.OxCodes, opts.Root, symbolName, opts.Language)
		result.TotalAffected += len(result.HiddenCallers)
	}

	for pkg := range pkgSet {
		result.AffectedPackages = append(result.AffectedPackages, pkg)
	}
	sort.Strings(result.AffectedPackages)

	result.BlastRadius = classifyBlastRadius(result.TotalAffected, len(result.AffectedPackages))
	result.RiskScore = float64(result.TotalAffected) * (1.0 + float64(len(result.AffectedPackages))*packageRiskMultiplier)

	return result
}

// findTarget returns the first function/method with the given name.
func findTarget(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

// buildCallerIndex creates a reverse map: callee → edges where it's called.
func buildCallerIndex(edges []callgraph.CallEdge) map[*parser.Symbol][]callgraph.CallEdge {
	idx := make(map[*parser.Symbol][]callgraph.CallEdge)
	for _, e := range edges {
		if e.Callee != nil {
			idx[e.Callee] = append(idx[e.Callee], e)
		}
	}
	return idx
}

type bfsItem struct {
	sym   *parser.Symbol
	depth int
}

// traverseCallers runs BFS from target through the caller index,
// populating result.DirectCallers and result.TransitiveCallers.
// Returns the set of affected packages.
func traverseCallers(target *parser.Symbol, callerIndex map[*parser.Symbol][]callgraph.CallEdge,
	maxDepth int, result *Result) map[string]bool {
	visited := map[*parser.Symbol]bool{target: true}
	queue := []bfsItem{{target, 0}}
	pkgSet := make(map[string]bool)

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		for _, edge := range callerIndex[item.sym] {
			caller := edge.Caller
			if caller == nil || visited[caller] {
				continue
			}
			visited[caller] = true

			distance := item.depth + 1
			affected := makeAffected(caller, distance)
			pkgSet[affected.Package] = true

			if distance == 1 {
				result.DirectCallers = append(result.DirectCallers, affected)
			} else {
				result.TransitiveCallers = append(result.TransitiveCallers, affected)
			}

			queue = append(queue, bfsItem{caller, distance})
		}
	}

	return pkgSet
}

// makeAffected creates an AffectedSymbol with confidence decaying by distance.
func makeAffected(sym *parser.Symbol, distance int) AffectedSymbol {
	confidence := 1.0 - float64(distance-1)*confidenceDecayPerHop
	if confidence < minConfidence {
		confidence = minConfidence
	}
	return AffectedSymbol{
		Name:       sym.Name,
		File:       sym.File,
		Package:    filepath.Dir(sym.File),
		Distance:   distance,
		Confidence: confidence,
	}
}

func classifyBlastRadius(callers, packages int) string {
	if callers == 0 {
		return "none"
	}
	if callers <= lowMaxCallers && packages <= lowMaxPackages {
		return "low"
	}
	if callers <= medMaxCallers && packages <= medMaxPackages {
		return "medium"
	}
	return "high"
}
