package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/impact"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// DeltaInput configures a delta review.
type DeltaInput struct {
	Root            string // repo root (absolute path)
	Base            string // base ref (default "HEAD~1")
	Depth           int    // impact traversal depth (default 2)
	Language        string // optional language filter
	IncludeSnippets bool   // include source code snippets around changed symbols
	OxCodes         *oxcodes.Client
}

// DeltaResult is the output of a delta review.
type DeltaResult struct {
	ChangedFiles    []FileDiff       `json:"changed_files"`
	ChangedSymbols  []ChangedSymbol  `json:"changed_symbols"`
	ImpactedSymbols []ImpactedSymbol `json:"impacted_symbols"`
	UntestedSymbols []string         `json:"untested_symbols,omitempty"`
	Snippets        []Snippet        `json:"snippets,omitempty"`
	Risk            RiskGuidance     `json:"risk"`
	Tier            string           `json:"tier"`
}

// ImpactedSymbol is a downstream symbol affected by a change.
type ImpactedSymbol struct {
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Distance   int     `json:"distance"`
	Confidence float64 `json:"confidence"`
	ChangedBy  string  `json:"changed_by"`
}

const defaultDeltaDepth = 2

// DeltaReview runs the full delta review pipeline.
func DeltaReview(ctx context.Context, input DeltaInput) (*DeltaResult, error) {
	if input.Base == "" {
		input.Base = "HEAD~1"
	}
	if input.Depth <= 0 {
		input.Depth = defaultDeltaDepth
	}

	// Step 1: Git diff.
	diffs, err := ChangedFiles(ctx, input.Root, input.Base)
	if err != nil {
		return nil, fmt.Errorf("changed files: %w", err)
	}
	if len(diffs) == 0 {
		return &DeltaResult{Risk: RiskGuidance{RiskLevel: "low"}}, nil
	}

	// Step 2: Build call graph for current state.
	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     input.Root,
		Language: input.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("build call graph: %w", err)
	}

	// Step 3: Intersect diffs with symbols.
	changed := ChangedSymbols(cg.Symbols, diffs, input.Root)

	// Step 4: Impact analysis per changed symbol.
	impactResults := make(map[string]*impact.Result)
	var allImpacted []ImpactedSymbol
	for _, cs := range changed {
		ir := impact.Analyze(ctx, cg, cs.Symbol.Name, impact.Options{
			MaxDepth: input.Depth,
			OxCodes:  input.OxCodes,
			Root:     input.Root,
			Language: input.Language,
		})
		if ir.Found {
			impactResults[cs.Symbol.Name] = ir
			for _, a := range ir.DirectCallers {
				allImpacted = append(allImpacted, ImpactedSymbol{
					Name: a.Name, File: a.File, Distance: a.Distance,
					Confidence: a.Confidence, ChangedBy: cs.Symbol.Name,
				})
			}
			for _, a := range ir.TransitiveCallers {
				allImpacted = append(allImpacted, ImpactedSymbol{
					Name: a.Name, File: a.File, Distance: a.Distance,
					Confidence: a.Confidence, ChangedBy: cs.Symbol.Name,
				})
			}
		}
	}

	// Step 5: Deduplicate impacted symbols.
	allImpacted = dedup(allImpacted)

	// Step 6: TESTED_BY detection — find changed symbols lacking tests.
	// First pass: naming convention (TestXxx → Xxx).
	testedSet := buildTestedSet(cg.Symbols)
	// Second pass: ox-codes scoped search — find test files that reference
	// changed symbols inside function bodies (catches table-driven tests, etc.).
	if input.OxCodes != nil {
		enrichTestedSetViaOxCodes(ctx, input.OxCodes, input.Root, changed, testedSet)
	}
	var untestedSymbols []string
	for _, cs := range changed {
		if !testedSet[cs.Symbol.Name] {
			untestedSymbols = append(untestedSymbols, cs.Symbol.Name)
		}
	}

	// Step 7: Source snippets (optional).
	var snippets []Snippet
	if input.IncludeSnippets && len(changed) > 0 {
		snippets = ExtractSnippets(changed, input.Root)
	}

	// Step 8: Risk guidance.
	risk := GenerateRiskGuidance(RiskInput{
		ChangedSymbols:  changed,
		ImpactResults:   impactResults,
		UntestedSymbols: untestedSymbols,
	})

	return &DeltaResult{
		ChangedFiles:    diffs,
		ChangedSymbols:  changed,
		ImpactedSymbols: allImpacted,
		UntestedSymbols: untestedSymbols,
		Snippets:        snippets,
		Risk:            risk,
		Tier:            cg.Tier,
	}, nil
}

// buildTestedSet returns names of symbols that have at least one test.
func buildTestedSet(symbols []*parser.Symbol) map[string]bool {
	tested := make(map[string]bool)
	for _, s := range symbols {
		name := s.Name
		switch s.Language {
		case "go":
			for _, prefix := range []string{"Test_", "Test", "Benchmark"} {
				if strings.HasPrefix(name, prefix) {
					rest := strings.TrimPrefix(name, prefix)
					if parts := strings.SplitN(rest, "_", 2); len(parts) > 0 && parts[0] != "" {
						tested[parts[0]] = true
					}
				}
			}
		case "python":
			if strings.HasPrefix(name, "test_") {
				tested[strings.TrimPrefix(name, "test_")] = true
			} else if strings.HasPrefix(name, "Test") {
				tested[strings.TrimPrefix(name, "Test")] = true
			}
		}
	}
	return tested
}

// enrichTestedSetViaOxCodes uses ox-codes scoped search to find test functions
// that reference changed symbols inside their bodies.
func enrichTestedSetViaOxCodes(ctx context.Context, oc *oxcodes.Client, root string, changed []ChangedSymbol, testedSet map[string]bool) {
	for _, cs := range changed {
		if testedSet[cs.Symbol.Name] {
			continue
		}
		resp, err := oc.SearchScoped(ctx, oxcodes.ScopedSearchInput{
			Root:       root,
			Pattern:    cs.Symbol.Name,
			Scope:      "function",
			Language:   cs.Symbol.Language,
			MaxResults: 5,
		})
		if err != nil || resp == nil {
			continue
		}
		for _, m := range resp.Matches {
			if langutil.IsTestFile(m.File) {
				testedSet[cs.Symbol.Name] = true
				break
			}
		}
	}
}

func dedup(items []ImpactedSymbol) []ImpactedSymbol {
	seen := make(map[string]bool)
	var out []ImpactedSymbol
	for _, item := range items {
		key := item.Name + ":" + item.File
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}
