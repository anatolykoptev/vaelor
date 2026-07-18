package deadcode

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestAnalyzeFiltersDead creates 3 symbols (helper, used, main) with 1 edge
// (main->used) and expects only helper to be reported dead.
func TestAnalyzeFiltersDead(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestAnalyze_FuncRefNotDead verifies that a function passed as an argument
// to another function (e.g. Register("name", handler)) is NOT flagged as dead.
func TestAnalyze_FuncRefNotDead(t *testing.T) {
	t.Parallel()
	handler := &parser.Symbol{
		Name: "renderHeading", Kind: parser.KindFunction,
		File: "/app/render.go", StartLine: 10, EndLine: 20,
	}
	initFn := &parser.Symbol{
		Name: "initStealth", Kind: parser.KindFunction,
		File: "/app/stealth.go", StartLine: 1, EndLine: 15,
	}
	register := &parser.Symbol{
		Name: "Register", Kind: parser.KindFunction,
		File: "/app/registry.go", StartLine: 1, EndLine: 5,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/app/main.go", StartLine: 1, EndLine: 10,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{handler, initFn, register, mainSym},
		Edges: []callgraph.CallEdge{
			// main calls Register, passing renderHeading as argument
			{Caller: mainSym, Callee: register, CalleeName: "Register"},
			// renderHeading referenced as argument → resolved as call edge
			{Caller: mainSym, Callee: handler, CalleeName: "renderHeading"},
			// initStealth referenced as argument → resolved as call edge
			{Caller: mainSym, Callee: initFn, CalleeName: "initStealth"},
		},
	}

	result := Analyze(cg, Options{})

	if result.DeadCount != 0 {
		t.Errorf("expected 0 dead functions, got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
}

// TestAnalyze_HookCallbackNotDead verifies that WordPress hook callbacks are
// NOT flagged as dead code when their names are passed via HookCallbacks.
//
// RED without HookCallbacks: both callbacks would be reported dead (no callers).
// GREEN with HookCallbacks: callbacks excluded from dead code results.
func TestAnalyze_HookCallbackNotDead(t *testing.T) {
	t.Parallel()
	// Simulate a WordPress plugin with hook registrations.
	// These functions have ZERO direct callers — only reachable via hooks.
	onInit := &parser.Symbol{
		Name: "on_init", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/main.php", StartLine: 10, EndLine: 20,
	}
	filterTitle := &parser.Symbol{
		Name: "filter_title", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/main.php", StartLine: 25, EndLine: 35,
	}
	reallyDead := &parser.Symbol{
		Name: "unused_helper", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/helpers.php", StartLine: 1, EndLine: 5,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/main.php", StartLine: 1, EndLine: 8,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{onInit, filterTitle, reallyDead, mainSym},
		Edges:   nil, // No direct calls — hooks are the only connection.
	}

	// GREEN: With HookCallbacks, hook callbacks are excluded.
	result := Analyze(cg, Options{
		HookCallbacks: []string{"on_init", "filter_title"},
	})

	// Only unused_helper should be dead. The two hook callbacks must survive.
	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead function (unused_helper), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
	if result.DeadCount == 1 && result.DeadSymbols[0].Name != "unused_helper" {
		t.Errorf("expected dead symbol 'unused_helper', got %q", result.DeadSymbols[0].Name)
	}
}

// TestAnalyze_HookCallbackNotDead_RED proves the test above is meaningful:
// WITHOUT HookCallbacks, the same callbacks ARE reported as dead.
func TestAnalyze_HookCallbackNotDead_RED(t *testing.T) {
	t.Parallel()
	onInit := &parser.Symbol{
		Name: "on_init", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/main.php", StartLine: 10, EndLine: 20,
	}
	filterTitle := &parser.Symbol{
		Name: "filter_title", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/main.php", StartLine: 25, EndLine: 35,
	}
	reallyDead := &parser.Symbol{
		Name: "unused_helper", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/helpers.php", StartLine: 1, EndLine: 5,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/wp-content/plugins/myplugin/main.php", StartLine: 1, EndLine: 8,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{onInit, filterTitle, reallyDead, mainSym},
		Edges:   nil,
	}

	// RED scenario: no HookCallbacks → all 3 non-main functions are dead.
	result := Analyze(cg, Options{})

	if result.DeadCount != 3 {
		t.Errorf("RED proof: expected 3 dead (no hook awareness), got %d", result.DeadCount)
	}

	// Verify the hook callbacks ARE in the dead list.
	deadNames := make(map[string]bool)
	for _, d := range result.DeadSymbols {
		deadNames[d.Name] = true
	}
	if !deadNames["on_init"] {
		t.Error("RED proof: on_init should be dead without HookCallbacks")
	}
	if !deadNames["filter_title"] {
		t.Error("RED proof: filter_title should be dead without HookCallbacks")
	}
}

// TestAnalyze_InjectHookEdges_Integration is an end-to-end test proving that
// InjectHookEdges + deadcode analysis work together: hook callbacks become
// "called" via synthetic edges and are no longer dead.
func TestAnalyze_InjectHookEdges_Integration(t *testing.T) {
	t.Parallel()
	// PHP WordPress plugin symbols.
	onInit := &parser.Symbol{
		Name: "on_init", Kind: parser.KindFunction,
		File: "/plugin/main.php", StartLine: 10, EndLine: 20,
	}
	filterContent := &parser.Symbol{
		Name: "modify_content", Kind: parser.KindFunction,
		File: "/plugin/main.php", StartLine: 25, EndLine: 40,
	}
	genuinelyDead := &parser.Symbol{
		Name: "old_unused", Kind: parser.KindFunction,
		File: "/plugin/legacy.php", StartLine: 1, EndLine: 10,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/plugin/main.php", StartLine: 1, EndLine: 8,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{onInit, filterContent, genuinelyDead, mainSym},
		Edges:   nil, // Start with no edges.
	}

	// Before injection: 3 dead functions (everything except main).
	pre := Analyze(cg, Options{})
	if pre.DeadCount != 3 {
		t.Fatalf("pre-injection: expected 3 dead, got %d", pre.DeadCount)
	}

	// Inject hook edges (simulating what BuildFromRepo does).
	hookRoutes := []callgraph.HookRoute{
		{Method: "ACTION", Path: "init", Handler: "on_init", Side: "server"},
		{Method: "FILTER", Path: "the_content", Handler: "modify_content", Side: "server"},
		{Method: "ACTION", Path: "init", Side: "client", Line: 3},
		{Method: "FILTER", Path: "the_content", Side: "client", Line: 5},
	}
	callgraph.InjectHookEdges(cg, hookRoutes)

	// After injection: only old_unused should be dead.
	post := Analyze(cg, Options{})
	if post.DeadCount != 1 {
		t.Errorf("post-injection: expected 1 dead (old_unused), got %d", post.DeadCount)
		for _, d := range post.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
	if post.DeadCount == 1 && post.DeadSymbols[0].Name != "old_unused" {
		t.Errorf("expected dead 'old_unused', got %q", post.DeadSymbols[0].Name)
	}
}

// TestAnalyze_ServerOnlyHooks verifies that WordPress hooks registered with
// add_action/add_filter (server-side only, no do_action in repo) are NOT
// flagged as dead. This covers WP core hooks like admin_notices, init, etc.
func TestAnalyze_ServerOnlyHooks(t *testing.T) {
	t.Parallel()
	enqueueEditor := &parser.Symbol{
		Name: "enqueue_editor", Kind: parser.KindMethod,
		File: "/plugin/assets.php", StartLine: 10, EndLine: 30,
	}
	renderNotice := &parser.Symbol{
		Name: "render_admin_notice", Kind: parser.KindMethod,
		File: "/plugin/license.php", StartLine: 20, EndLine: 40,
	}
	genuinelyDead := &parser.Symbol{
		Name: "old_unused", Kind: parser.KindFunction,
		File: "/plugin/legacy.php", StartLine: 1, EndLine: 10,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/plugin/main.php", StartLine: 1, EndLine: 8,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{enqueueEditor, renderNotice, genuinelyDead, mainSym},
		Edges:   nil,
	}

	// Inject ONLY server-side hooks (no client-side do_action in repo).
	hookRoutes := []callgraph.HookRoute{
		{Method: "ACTION", Path: "enqueue_block_editor_assets", Handler: "enqueue_editor", Side: "server"},
		{Method: "ACTION", Path: "admin_notices", Handler: "render_admin_notice", Side: "server"},
	}
	callgraph.InjectHookEdges(cg, hookRoutes)

	result := Analyze(cg, Options{})

	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead (old_unused), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
	if result.DeadCount == 1 && result.DeadSymbols[0].Name != "old_unused" {
		t.Errorf("expected dead 'old_unused', got %q", result.DeadSymbols[0].Name)
	}
}

// TestAnalyze_ConstructorNotDead verifies that language constructors are
// not flagged as dead code — they're called implicitly by new ClassName().
func TestAnalyze_ConstructorNotDead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
	}{
		{"__construct", "/app/class.php"},
		{"__init__", "/app/class.py"},
		{"constructor", "/app/class.ts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sym := &parser.Symbol{
				Name: tc.name, Kind: parser.KindMethod,
				File: tc.file, StartLine: 5, EndLine: 15,
			}
			mainSym := &parser.Symbol{
				Name: "main", Kind: parser.KindFunction,
				File: tc.file, StartLine: 1, EndLine: 3,
			}
			cg := &callgraph.CallGraph{
				Symbols: []*parser.Symbol{sym, mainSym},
				Edges:   nil,
			}
			result := Analyze(cg, Options{})
			for _, d := range result.DeadSymbols {
				if d.Name == tc.name {
					t.Errorf("%s should not be flagged as dead (implicit constructor)", tc.name)
				}
			}
		})
	}
}

func TestAnalyze_RustTestAttributeNotDead(t *testing.T) {
	t.Parallel()
	testFn := &parser.Symbol{
		Name: "test_something", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		Attributes: []string{"#[test]"},
	}
	asyncTestFn := &parser.Symbol{
		Name: "test_async_thing", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 25, EndLine: 35,
		Attributes: []string{"#[tokio::test]"},
	}
	reallyDead := &parser.Symbol{
		Name: "unused_helper", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 40, EndLine: 45,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{testFn, asyncTestFn, reallyDead, mainSym},
		Edges:   nil,
	}
	result := Analyze(cg, Options{})
	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead (unused_helper), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
}

func TestAnalyze_RustPubVisibility(t *testing.T) {
	t.Parallel()
	pubFn := &parser.Symbol{
		Name: "new", Kind: parser.KindMethod,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		IsPublic: true,
	}
	privateFn := &parser.Symbol{
		Name: "helper", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 25, EndLine: 30,
		IsPublic: false,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{pubFn, privateFn, mainSym},
		Edges:   nil,
	}
	result := Analyze(cg, Options{})
	// Rust pub functions are now included even with IncludeExported=false (default),
	// because in Rust pub means crate-visibility, not "definitely used externally".
	// Both pubFn (new, exported=true) and privateFn (helper) should appear as dead.
	if result.DeadCount != 2 {
		t.Errorf("expected 2 dead (new + helper), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s (exported=%v, confidence=%s)", d.Name, d.Exported, d.Confidence)
		}
	}
	// pub fn should have medium confidence (may be used by dependent crates).
	for _, d := range result.DeadSymbols {
		if d.Name == "new" && d.Confidence != ConfidenceMedium {
			t.Errorf("expected confidence=medium for pub fn new, got %s", d.Confidence)
		}
	}
}

func TestAnalyze_RustWellKnownTraitMethods(t *testing.T) {
	t.Parallel()
	methods := []string{"fmt", "clone", "drop", "default", "from", "into", "next", "eq", "hash", "poll", "serialize", "deserialize"}
	var symbols []*parser.Symbol
	for i, name := range methods {
		symbols = append(symbols, &parser.Symbol{
			Name: name, Kind: parser.KindMethod,
			Language: "rust", File: "/src/lib.rs",
			StartLine: uint32(i*10 + 1), EndLine: uint32(i*10 + 5),
			Receiver: "SomeTrait for MyType",
		})
	}
	symbols = append(symbols, &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	})
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: nil}
	result := Analyze(cg, Options{})
	if result.DeadCount != 0 {
		t.Errorf("expected 0 dead (all well-known trait methods), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
}

func TestAnalyze_RustTraitImplMethodConfidence(t *testing.T) {
	t.Parallel()
	traitMethod := &parser.Symbol{
		Name: "custom_method", Kind: parser.KindMethod,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		Receiver: "MyTrait for MyType",
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{traitMethod, mainSym},
		Edges:   nil,
	}
	result := Analyze(cg, Options{})
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead, got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Confidence != ConfidenceMedium {
		t.Errorf("trait impl method should be medium confidence, got %q", result.DeadSymbols[0].Confidence)
	}
}

func TestAnalyze_RustTestFileRs(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Name: "helper_in_test", Kind: parser.KindFunction,
		Language: "rust", File: "/src/foo_test.rs", StartLine: 1, EndLine: 10,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym, mainSym},
		Edges:   nil,
	}
	result := Analyze(cg, Options{})
	if result.DeadCount != 0 {
		t.Errorf("expected 0 dead (_test.rs skipped), got %d", result.DeadCount)
	}
}

// TestAnalyze_IsInterfaceEdgeExcludesMethod verifies that a method called via
// interface dispatch (IsInterface=true edge) is not flagged as dead code.
func TestAnalyze_IsInterfaceEdgeExcludesMethod(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: "main.go", StartLine: 1, EndLine: 5},
		{Name: "Greet", Kind: parser.KindMethod, Receiver: "EnglishGreeter", File: "greet.go", StartLine: 1, EndLine: 3},
	}
	edges := []callgraph.CallEdge{
		{Caller: symbols[0], CalleeName: "Greet", Receiver: "Greeter", Line: 3, IsInterface: true},
	}
	rels := []parser.TypeRelationship{
		{Subject: "EnglishGreeter", Target: "Greeter", Kind: parser.RelImplements},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: edges, TypeRels: rels}
	result := Analyze(cg, Options{IncludeExported: true, Relationships: rels})
	for _, ds := range result.DeadSymbols {
		if ds.Name == "Greet" {
			t.Errorf("Greet should not be dead — called via interface dispatch")
		}
	}
}

// TestAnalyze_CrossTypeInterfaceFiltering verifies that when interface method "Greet"
// is dispatched on one type, ALL implementors' Greet methods are excluded from dead code.
func TestAnalyze_CrossTypeInterfaceFiltering(t *testing.T) {
	t.Parallel()
	iface := &parser.Symbol{Name: "Greeter", Kind: parser.KindInterface, File: "a.go", StartLine: 1, EndLine: 3}
	eng := &parser.Symbol{Name: "Greet", Kind: parser.KindMethod, Receiver: "EnglishGreeter", File: "b.go", StartLine: 1, EndLine: 3}
	spa := &parser.Symbol{Name: "Greet", Kind: parser.KindMethod, Receiver: "SpanishGreeter", File: "c.go", StartLine: 1, EndLine: 3}
	main := &parser.Symbol{Name: "main", Kind: parser.KindFunction, File: "main.go", StartLine: 1, EndLine: 5}
	symbols := []*parser.Symbol{iface, eng, spa, main}
	edges := []callgraph.CallEdge{
		{Caller: main, CalleeName: "Greet", Receiver: "Greeter", Line: 3, IsInterface: true},
	}
	rels := []parser.TypeRelationship{
		{Subject: "EnglishGreeter", Target: "Greeter", Kind: parser.RelImplements},
		{Subject: "SpanishGreeter", Target: "Greeter", Kind: parser.RelImplements},
	}
	cg := &callgraph.CallGraph{Symbols: symbols, Edges: edges, TypeRels: rels}
	result := Analyze(cg, Options{IncludeExported: true, Relationships: rels})
	for _, ds := range result.DeadSymbols {
		if ds.Name == "Greet" {
			t.Errorf("Greet on %s should not be dead — interface method on implementor", ds.File)
		}
	}
}

// TestAnalyze_WithNilOxCodes verifies that existing behavior is unchanged when
// OxCodes is nil — no second pass is attempted and the full dead list is returned.
func TestAnalyze_WithNilOxCodes(t *testing.T) {
	t.Parallel()
	helper := &parser.Symbol{
		Name: "helperFn", Kind: parser.KindFunction,
		File: "/src/util.go", StartLine: 1, EndLine: 5,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		File: "/src/main.go", StartLine: 1, EndLine: 10,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{helper, mainSym},
		Edges:   nil,
	}

	// OxCodes is nil — must behave exactly like Options{}.
	result := Analyze(cg, Options{
		OxCodes:  nil,
		Root:     "/src",
		Language: "go",
	})

	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead symbol with nil OxCodes, got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Name != "helperFn" {
		t.Errorf("expected dead symbol 'helperFn', got %q", result.DeadSymbols[0].Name)
	}
}

// TestAnalyzeDeadRatio verifies the ratio calculation.
func TestAnalyzeDeadRatio(t *testing.T) {
	t.Parallel()
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

// TestAnalyze_TSExportedCamelCaseNotHighConfidenceDead is the RED→GREEN
// regression test for the isSymbolExported bug: internal/parser's TypeScript
// (and JavaScript/Kotlin/Swift/PHP/C#/Ruby) handlers never populate
// parser.Symbol.IsPublic, so isSymbolExported used to fall through to the
// Go-only uppercase-first isExported(name) helper. A TS `export function
// fooBar()` (camelCase, no internal callers) was misclassified
// exported=false — high-confidence false-positive dead code — for nearly
// every exported symbol in a TS/JS repo, not just this one name.
//
// FAILS before the fix (isSymbolExported delegates to langutil.IsExportedForDoc
// for non-Rust/non-IsPublic languages): fooBar comes back exported=false,
// confidence=high. PASSES after: exported is language-aware, so fooBar's
// medium/no report reflects TS's "any non-underscore name is public API"
// convention instead of Go's uppercase-first one.
func TestAnalyze_TSExportedCamelCaseNotHighConfidenceDead(t *testing.T) {
	t.Parallel()
	tsFn := &parser.Symbol{
		Name: "fooBar", Kind: parser.KindFunction,
		Language: "typescript", File: "/src/index.ts", StartLine: 10, EndLine: 20,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "typescript", File: "/src/main.ts", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{Symbols: []*parser.Symbol{tsFn, mainSym}, Edges: nil}
	result := Analyze(cg, Options{})
	for _, d := range result.DeadSymbols {
		if d.Name == "fooBar" && d.Confidence == ConfidenceHigh {
			t.Errorf("fooBar (TS exported camelCase fn) flagged high-confidence dead: exported=%v confidence=%s",
				d.Exported, d.Confidence)
		}
	}
}

// TestAnalyze_RustPrivateSnakeCaseNameStillDead pins the Rust-specific carve-out
// in isSymbolExported: unlike TS/JS, internal/parser/handler_rust.go reliably
// computes IsPublic for every symbol kind it emits (hasVisibilityModifier scans
// for the `pub` keyword directly). So a Rust symbol with IsPublic=false is
// genuinely private, not "unknown" — isSymbolExported must trust that and NOT
// fall back to langutil's "any non-underscore name is exported" convention,
// which would misclassify plain private snake_case helpers (fn helper()) as
// exported and hide them from dead-code output. This guards against exactly
// the regression a naive "delegate everything to langutil" fix would cause:
// running it against this test flips the result from dead (correct) to
// not-dead (silences a real finding).
func TestAnalyze_RustPrivateSnakeCaseNameStillDead(t *testing.T) {
	t.Parallel()
	privateFn := &parser.Symbol{
		Name: "compute_totals", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		IsPublic: false,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{Symbols: []*parser.Symbol{privateFn, mainSym}, Edges: nil}
	result := Analyze(cg, Options{})
	found := false
	for _, d := range result.DeadSymbols {
		if d.Name == "compute_totals" {
			found = true
			if d.Exported {
				t.Errorf("compute_totals (private Rust fn, IsPublic=false) reported exported=true, want false")
			}
			if d.Confidence != ConfidenceHigh {
				t.Errorf("compute_totals confidence = %q, want %q (unexported, non-method)", d.Confidence, ConfidenceHigh)
			}
		}
	}
	if !found {
		t.Errorf("compute_totals (private, unused Rust fn) not flagged dead at all")
	}
}
