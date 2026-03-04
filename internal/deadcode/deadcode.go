// Package deadcode detects functions and methods with zero incoming calls.
package deadcode

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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

// httpHandlerPatterns identify HTTP handler functions by signature.
var httpHandlerPatterns = []string{
	"http.ResponseWriter",
	"*http.Request",
	"gin.Context",
	"echo.Context",
	"fiber.Ctx",
	"chi.Router",
}

// wellKnownInterfaceMethods are method names commonly required by interfaces.
var wellKnownInterfaceMethods = map[string]bool{
	"ServeHTTP":     true,
	"String":        true,
	"Error":         true,
	"MarshalJSON":   true,
	"UnmarshalJSON": true,
	"Close":         true,
	"Read":          true,
	"Write":         true,
	"Len":           true,
	"Less":          true,
	"Swap":          true,
}

// constructorNames are method names that serve as class constructors in various
// languages. They are called implicitly by `new ClassName()` and should never
// be flagged as dead code.
var constructorNames = map[string]bool{
	"__construct": true, // PHP
	"__init__":    true, // Python
	"constructor": true, // JS/TS class
}

// isHTTPHandler checks if a symbol's signature indicates it's an HTTP handler.
func isHTTPHandler(sym *parser.Symbol) bool {
	sig := sym.Signature
	for _, pattern := range httpHandlerPatterns {
		if strings.Contains(sig, pattern) {
			return true
		}
	}
	return false
}

// isWellKnownInterfaceMethod checks if the function name matches a well-known interface method.
func isWellKnownInterfaceMethod(sym *parser.Symbol) bool {
	return sym.Kind == parser.KindMethod && wellKnownInterfaceMethods[sym.Name]
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
func collectDeadSymbols(funcSymbols []*parser.Symbol, called map[*parser.Symbol]bool, hookSet map[string]bool, opts Options) []DeadSymbol {
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

// isEntryPoint returns true for well-known entry point functions.
func isEntryPoint(name string) bool {
	switch name {
	case "main", "init", "TestMain":
		return true
	}
	return false
}

// isTestFunc returns true for Go test/benchmark/example/fuzz functions.
func isTestFunc(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			// Must be followed by uppercase letter or underscore (Go convention).
			next := rune(name[len(prefix)])
			if unicode.IsUpper(next) || next == '_' {
				return true
			}
		}
	}
	return false
}

// isTestFile returns true if the file path ends with _test.go or similar test patterns.
func isTestFile(file string) bool {
	lower := strings.ToLower(file)
	testSuffixes := []string{
		"_test.go", "_test.py", "_test.rs",
		".test.ts", ".test.js",
		".spec.ts", ".spec.js",
	}
	for _, suf := range testSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/")
}

// isExported returns true if the name starts with an uppercase letter (Go convention)
// or is otherwise considered public API.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// classifyConfidence assigns a confidence level based on symbol properties.
func classifyConfidence(sym *parser.Symbol, exported bool) string {
	if exported {
		return ConfidenceLow
	}
	if sym.Kind == parser.KindMethod {
		return ConfidenceMedium
	}
	if sym.Receiver != "" && strings.Contains(sym.Receiver, " for ") {
		return ConfidenceMedium
	}
	return ConfidenceHigh
}

// lines returns the number of source lines for a symbol.
func lines(sym *parser.Symbol) int {
	if sym.EndLine >= sym.StartLine {
		return int(sym.EndLine-sym.StartLine) + 1
	}
	return 1
}
