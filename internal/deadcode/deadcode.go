// Package deadcode detects functions and methods with zero incoming calls.
package deadcode

import (
	"path/filepath"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// Confidence levels for dead code classification.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Options configures dead code detection.
type Options struct {
	IncludeExported bool     // include exported (public) symbols — usually false positives
	IncludeTests    bool     // include test-file symbols
	HookCallbacks   []string // function names registered as WordPress hook callbacks
}

// DeadSymbol is a function/method with zero incoming calls.
type DeadSymbol struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	Package    string `json:"package"`
	StartLine  int    `json:"start_line"`
	Lines      int    `json:"lines"`
	Exported   bool   `json:"exported"`
	Confidence string `json:"confidence"` // high, medium, low
}

// Result is the output of dead code analysis.
type Result struct {
	TotalFunctions int          `json:"total_functions"`
	DeadCount      int          `json:"dead_count"`
	DeadRatio      float64      `json:"dead_ratio"`
	DeadSymbols    []DeadSymbol `json:"dead_symbols"`
}

// Analyze detects functions/methods with zero incoming calls in the call graph.
// It filters out entry points (main, init, TestMain), test functions,
// and optionally exported symbols to reduce false positives.
func Analyze(cg *callgraph.CallGraph, opts Options) *Result {
	called := buildCalledSet(cg.Edges)
	funcSymbols := filterFuncSymbols(cg.Symbols)

	// Build hook callback set for fast lookup.
	hookSet := make(map[string]bool, len(opts.HookCallbacks))
	for _, name := range opts.HookCallbacks {
		hookSet[name] = true
	}

	dead := collectDeadSymbols(funcSymbols, called, hookSet, opts)

	sort.Slice(dead, func(i, j int) bool {
		if dead[i].File != dead[j].File {
			return dead[i].File < dead[j].File
		}
		return dead[i].StartLine < dead[j].StartLine
	})

	total := len(funcSymbols)
	deadCount := len(dead)
	var ratio float64
	if total > 0 {
		ratio = float64(deadCount) / float64(total)
	}

	return &Result{
		TotalFunctions: total,
		DeadCount:      deadCount,
		DeadRatio:      ratio,
		DeadSymbols:    dead,
	}
}

// filterFuncSymbols returns only function and method symbols from the list.
func filterFuncSymbols(symbols []*parser.Symbol) []*parser.Symbol {
	var funcs []*parser.Symbol
	for _, sym := range symbols {
		if sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod {
			funcs = append(funcs, sym)
		}
	}
	return funcs
}

// shouldSkipSymbol returns true if the symbol should be excluded from dead code analysis.
func shouldSkipSymbol(sym *parser.Symbol, opts Options) bool {
	if isEntryPoint(sym.Name) || isTestFunc(sym.Name) {
		return true
	}
	if hasTestAttribute(sym) {
		return true
	}
	if constructorNames[sym.Name] {
		return true
	}
	if sym.Language == "python" && pythonDunderMethods[sym.Name] {
		return true
	}
	if isPythonFrameworkEntryPoint(sym) {
		return true
	}
	if isHTTPHandler(sym) || isWellKnownInterfaceMethod(sym) {
		return true
	}
	if isRustWellKnownMethod(sym) {
		return true
	}
	if !opts.IncludeTests && isTestFile(sym.File) {
		return true
	}
	if !opts.IncludeExported && isSymbolExported(sym) {
		return true
	}
	return false
}

// collectDeadSymbols finds all uncalled symbols that are not excluded by filters.
func collectDeadSymbols(
	funcSymbols []*parser.Symbol,
	called map[*parser.Symbol]bool,
	hookSet map[string]bool,
	opts Options,
) []DeadSymbol {
	var dead []DeadSymbol
	for _, sym := range funcSymbols {
		if called[sym] || hookSet[sym.Name] || shouldSkipSymbol(sym, opts) {
			continue
		}
		exported := isSymbolExported(sym)
		dead = append(dead, DeadSymbol{
			Name:       sym.Name,
			Kind:       string(sym.Kind),
			File:       sym.File,
			Package:    filepath.Dir(sym.File),
			StartLine:  int(sym.StartLine),
			Lines:      lines(sym),
			Exported:   exported,
			Confidence: classifyConfidence(sym, exported),
		})
	}
	return dead
}

// buildCalledSet returns the set of symbols that appear as callees in any edge.
func buildCalledSet(edges []callgraph.CallEdge) map[*parser.Symbol]bool {
	called := make(map[*parser.Symbol]bool)
	for _, e := range edges {
		if e.Callee != nil {
			called[e.Callee] = true
		}
	}
	return called
}
