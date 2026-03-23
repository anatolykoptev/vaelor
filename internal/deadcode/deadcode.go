// Package deadcode detects functions and methods with zero incoming calls.
package deadcode

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// interfaceInfo tracks which types implement interfaces and which method names
// belong to interfaces, used to filter out interface method implementations.
type interfaceInfo struct {
	// implementors maps type name → true for types that implement any interface.
	implementors map[string]bool
	// interfaceMethodNames maps method name → true for methods declared on interfaces.
	interfaceMethodNames map[string]bool
}

// Confidence levels for dead code classification.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Options configures dead code detection.
type Options struct {
	IncludeExported bool                      // include exported (public) symbols — usually false positives
	IncludeTests    bool                      // include test-file symbols
	HookCallbacks   []string                  // function names registered as WordPress hook callbacks
	Relationships   []parser.TypeRelationship // type relationships for interface-aware filtering
	// OxCodes is an optional ox-codes client. When set, a second pass searches
	// for string references to apparent dead symbols, reducing false positives
	// caused by callbacks, reflection, and config-driven dispatch.
	OxCodes  *oxcodes.Client
	Root     string // repo root path (required for ox-codes queries)
	Language string // primary language (for scoped search)
	// Ctx is used for cancellation when OxCodes is set. A nil Ctx means
	// context.Background() is used.
	Ctx context.Context //nolint:containedctx
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

	ifaceInfo := buildInterfaceInfo(cg.Symbols, opts.Relationships, cg.Edges)
	dead := collectDeadSymbols(funcSymbols, called, hookSet, ifaceInfo, opts)

	// Second pass: check if "dead" symbols have string references via ox-codes.
	// This catches callbacks, reflection, and config-driven dispatch that the
	// tree-sitter call graph cannot see.
	if opts.OxCodes != nil && opts.Root != "" {
		ctx := opts.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		dead = filterByStringRefs(ctx, opts.OxCodes, opts.Root, opts.Language, dead)
	}

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
	if isCppImplicitMethod(sym) {
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
	ifaceInfo *interfaceInfo,
	opts Options,
) []DeadSymbol {
	var dead []DeadSymbol
	for _, sym := range funcSymbols {
		if called[sym] || hookSet[sym.Name] || shouldSkipSymbol(sym, opts) {
			continue
		}
		if isInterfaceImpl(sym, ifaceInfo) {
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

// buildInterfaceInfo extracts interface method names and implementing types
// from parsed symbols, type relationships, and proven interface dispatch edges.
func buildInterfaceInfo(symbols []*parser.Symbol, rels []parser.TypeRelationship, edges []callgraph.CallEdge) *interfaceInfo {
	info := &interfaceInfo{
		implementors:         make(map[string]bool),
		interfaceMethodNames: make(map[string]bool),
	}

	// Source 1: proven interface dispatches from go/types or SCIP.
	for _, e := range edges {
		if e.IsInterface && e.CalleeName != "" {
			info.interfaceMethodNames[e.CalleeName] = true
			if e.Receiver != "" {
				info.implementors[e.Receiver] = true
			}
		}
	}

	// Collect interface/trait method names from interface symbols.
	// Interface symbols have Kind=KindInterface; their methods are separate
	// symbols with the same Receiver.
	ifaceTypes := make(map[string]bool)
	for _, sym := range symbols {
		if sym.Kind == parser.KindInterface {
			ifaceTypes[sym.Name] = true
		}
	}
	// Methods whose Receiver is an interface type are interface method signatures.
	for _, sym := range symbols {
		if sym.Kind == parser.KindMethod && ifaceTypes[sym.Receiver] {
			info.interfaceMethodNames[sym.Name] = true
		}
	}

	// From relationships: types that implement/extend interfaces.
	for _, rel := range rels {
		if rel.Kind == parser.RelImplements || rel.Kind == parser.RelExtends {
			info.implementors[rel.Subject] = true
		}
		// Also: Go embedded interfaces (rel.Kind == RelEmbeds where target is interface)
		if rel.Kind == parser.RelEmbeds && ifaceTypes[rel.Target] {
			info.implementors[rel.Subject] = true
		}
	}

	return info
}

// isInterfaceImpl returns true if a method is likely implementing an interface.
// A method matches if: (a) its receiver type implements an interface AND
// (b) the method name matches a known interface method name.
func isInterfaceImpl(sym *parser.Symbol, info *interfaceInfo) bool {
	if info == nil || sym.Kind != parser.KindMethod {
		return false
	}
	if sym.Receiver == "" {
		return false
	}
	return info.implementors[sym.Receiver] && info.interfaceMethodNames[sym.Name]
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
