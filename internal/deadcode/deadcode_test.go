package deadcode

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestAnalyzeFiltersDead creates 3 symbols (helper, used, main) with 1 edge
// (main->used) and expects only helper to be reported dead.
func TestAnalyzeFiltersDead(t *testing.T) {
	helper := &parser.Symbol{
		Name: "helper", Kind: parser.KindFunction,
		File: "/src/util.go", StartLine: 1, EndLine: 5,
	}
	used := &parser.Symbol{
		Name: "usedFunc", Kind: parser.KindFunction,
		File: "/src/util.go", StartLine: 10, EndLine: 20,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/src/main.go", StartLine: 1, EndLine: 30,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{helper, used, mainSym},
		Edges: []callgraph.CallEdge{
			{Caller: mainSym, Callee: used, CalleeName: "usedFunc"},
		},
	}

	result := Analyze(cg, Options{})

	if result.TotalFunctions != 3 {
		t.Fatalf("expected 3 total functions, got %d", result.TotalFunctions)
	}
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead symbol, got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Name != "helper" {
		t.Errorf("expected dead symbol 'helper', got %q", result.DeadSymbols[0].Name)
	}
	if result.DeadSymbols[0].Confidence != ConfidenceHigh {
		t.Errorf("expected confidence %q, got %q", ConfidenceHigh, result.DeadSymbols[0].Confidence)
	}
	if result.DeadSymbols[0].Lines != 5 {
		t.Errorf("expected 5 lines, got %d", result.DeadSymbols[0].Lines)
	}
}

// TestAnalyzeSkipsExported verifies that exported symbols are skipped by default
// and included when IncludeExported=true.
func TestAnalyzeSkipsExported(t *testing.T) {
	exported := &parser.Symbol{
		Name: "PublicFunc", Kind: parser.KindFunction,
		File: "/src/api.go", StartLine: 1, EndLine: 10,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/src/main.go", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{exported, mainSym},
		Edges:   nil, // No calls to PublicFunc.
	}

	// Default: exported symbols skipped.
	result := Analyze(cg, Options{})
	if result.DeadCount != 0 {
		t.Fatalf("expected 0 dead (exported skipped), got %d", result.DeadCount)
	}

	// With IncludeExported: exported symbol should appear.
	result = Analyze(cg, Options{IncludeExported: true})
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead (exported included), got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Name != "PublicFunc" {
		t.Errorf("expected dead symbol 'PublicFunc', got %q", result.DeadSymbols[0].Name)
	}
	if !result.DeadSymbols[0].Exported {
		t.Error("expected Exported=true")
	}
	if result.DeadSymbols[0].Confidence != ConfidenceLow {
		t.Errorf("expected confidence %q for exported, got %q", ConfidenceLow, result.DeadSymbols[0].Confidence)
	}
}

// TestAnalyzeSkipsTestFuncs verifies that test functions are filtered out.
func TestAnalyzeSkipsTestFuncs(t *testing.T) {
	testFunc := &parser.Symbol{
		Name: "TestFoo", Kind: parser.KindFunction,
		File: "/src/foo_test.go", StartLine: 1, EndLine: 10,
	}
	benchFunc := &parser.Symbol{
		Name: "BenchmarkBar", Kind: parser.KindFunction,
		File: "/src/bar_test.go", StartLine: 1, EndLine: 10,
	}
	helperInTest := &parser.Symbol{
		Name: "setupDB", Kind: parser.KindFunction,
		File: "/src/foo_test.go", StartLine: 20, EndLine: 30,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/src/main.go", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{testFunc, benchFunc, helperInTest, mainSym},
		Edges:   nil,
	}

	// Default: test funcs and test file helpers skipped.
	result := Analyze(cg, Options{})
	if result.DeadCount != 0 {
		t.Fatalf("expected 0 dead (test funcs/files skipped), got %d", result.DeadCount)
	}

	// With IncludeTests: helpers in test files should appear
	// (but TestFoo / BenchmarkBar are still test functions and get filtered).
	result = Analyze(cg, Options{IncludeTests: true})
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead (test helper included), got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Name != "setupDB" {
		t.Errorf("expected dead symbol 'setupDB', got %q", result.DeadSymbols[0].Name)
	}
}

// TestAnalyzeMethodConfidence verifies that methods get "medium" confidence.
func TestAnalyzeMethodConfidence(t *testing.T) {
	method := &parser.Symbol{
		Name: "doWork", Kind: parser.KindMethod,
		File: "/src/worker.go", StartLine: 10, EndLine: 25,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/src/main.go", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{method, mainSym},
		Edges:   nil,
	}

	result := Analyze(cg, Options{})

	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead symbol, got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Kind != "method" {
		t.Errorf("expected kind 'method', got %q", result.DeadSymbols[0].Kind)
	}
	if result.DeadSymbols[0].Confidence != ConfidenceMedium {
		t.Errorf("expected confidence %q for method, got %q", ConfidenceMedium, result.DeadSymbols[0].Confidence)
	}
}

// TestAnalyzeSortsByFile verifies output is sorted by file path then line.
func TestAnalyzeSortsByFile(t *testing.T) {
	sym1 := &parser.Symbol{
		Name: "bHelper", Kind: parser.KindFunction,
		File: "/src/b.go", StartLine: 1, EndLine: 5,
	}
	sym2 := &parser.Symbol{
		Name: "aHelper", Kind: parser.KindFunction,
		File: "/src/a.go", StartLine: 10, EndLine: 15,
	}
	sym3 := &parser.Symbol{
		Name: "aHelper2", Kind: parser.KindFunction,
		File: "/src/a.go", StartLine: 1, EndLine: 5,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/src/main.go", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym1, sym2, sym3, mainSym},
		Edges:   nil,
	}

	result := Analyze(cg, Options{})

	if result.DeadCount != 3 {
		t.Fatalf("expected 3 dead symbols, got %d", result.DeadCount)
	}
	// Expected order: a.go:1, a.go:10, b.go:1
	if result.DeadSymbols[0].Name != "aHelper2" {
		t.Errorf("first dead should be aHelper2, got %q", result.DeadSymbols[0].Name)
	}
	if result.DeadSymbols[1].Name != "aHelper" {
		t.Errorf("second dead should be aHelper, got %q", result.DeadSymbols[1].Name)
	}
	if result.DeadSymbols[2].Name != "bHelper" {
		t.Errorf("third dead should be bHelper, got %q", result.DeadSymbols[2].Name)
	}
}

// TestAnalyze_HTTPHandlerNotDead verifies that HTTP handler functions are not flagged as dead.
func TestAnalyze_HTTPHandlerNotDead(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "handleUserCreate", Kind: parser.KindFunction, File: "/app/handlers.go", StartLine: 10, EndLine: 20,
			Signature: "func handleUserCreate(w http.ResponseWriter, r *http.Request)"},
		{Name: "handleHealth", Kind: parser.KindFunction, File: "/app/handlers.go", StartLine: 30, EndLine: 35,
			Signature: "func handleHealth(w http.ResponseWriter, r *http.Request)"},
		{Name: "reallyDead", Kind: parser.KindFunction, File: "/app/util.go", StartLine: 1, EndLine: 5,
			Signature: "func reallyDead()"},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: nil}

	result := Analyze(cg, Options{})
	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead function (reallyDead), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s (%s)", d.Name, d.Confidence)
		}
	}
}

// TestAnalyze_InterfaceMethodNotDead verifies that well-known interface methods are not flagged as dead.
func TestAnalyze_InterfaceMethodNotDead(t *testing.T) {
	symbols := []*parser.Symbol{
		{Name: "ServeHTTP", Kind: parser.KindMethod, File: "/app/server.go", StartLine: 10, EndLine: 20,
			Signature: "func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)"},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: nil}

	result := Analyze(cg, Options{})
	for _, d := range result.DeadSymbols {
		if d.Name == "ServeHTTP" {
			t.Error("ServeHTTP should not be flagged as dead (interface method)")
		}
	}
}

// TestAnalyzeDeadRatio verifies the ratio calculation.
func TestAnalyzeDeadRatio(t *testing.T) {
	mainSym := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "/src/main.go", StartLine: 1, EndLine: 5}
	used := &parser.Symbol{Name: "used", Kind: parser.KindFunction, File: "/src/a.go", StartLine: 1, EndLine: 5}
	dead1 := &parser.Symbol{Name: "dead1", Kind: parser.KindFunction, File: "/src/a.go", StartLine: 10, EndLine: 15}
	dead2 := &parser.Symbol{Name: "dead2", Kind: parser.KindFunction, File: "/src/b.go", StartLine: 1, EndLine: 5}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{mainSym, used, dead1, dead2},
		Edges: []callgraph.CallEdge{
			{Caller: mainSym, Callee: used, CalleeName: "used"},
		},
	}

	result := Analyze(cg, Options{})

	// main is entry point, used is called, dead1+dead2 are dead → 2/4 = 0.5
	if result.DeadCount != 2 {
		t.Fatalf("expected 2 dead, got %d", result.DeadCount)
	}
	if result.DeadRatio != 0.5 {
		t.Errorf("expected ratio 0.5, got %f", result.DeadRatio)
	}
}
