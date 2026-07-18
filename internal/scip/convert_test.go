package scip_test

import (
	"testing"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
)

func TestConvertToEdges_Empty(t *testing.T) {
	t.Parallel()
	idx := &gocodescip.Index{}
	edges := gocodescip.ConvertToEdges(idx)
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(edges))
	}
}

func TestConvertToEdges_SimpleCall(t *testing.T) {
	t.Parallel()
	// main at line 0 (1-indexed: 1), greet at line 5 (1-indexed: 6)
	// reference to greet at line 2 (inside main body)
	doc := &sciplib.Document{
		RelativePath: "main.go",
		Occurrences: []*sciplib.Occurrence{
			{
				Range:       []int32{0, 5, 9},
				Symbol:      "go . testpkg main().",
				SymbolRoles: int32(sciplib.SymbolRole_Definition),
			},
			{
				Range:       []int32{5, 5, 10},
				Symbol:      "go . testpkg greet().",
				SymbolRoles: int32(sciplib.SymbolRole_Definition),
			},
			// reference to greet at line 2 (inside main, which spans lines 0-4)
			{
				Range:  []int32{2, 4, 9},
				Symbol: "go . testpkg greet().",
			},
		},
		Symbols: []*sciplib.SymbolInformation{
			{Symbol: "go . testpkg main().", Kind: sciplib.SymbolInformation_Function, DisplayName: "main"},
			{Symbol: "go . testpkg greet().", Kind: sciplib.SymbolInformation_Function, DisplayName: "greet"},
		},
	}

	idx := &gocodescip.Index{Documents: []*sciplib.Document{doc}}
	edges := gocodescip.ConvertToEdges(idx)

	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(edges), edges)
	}

	e := edges[0]
	if e.CallerName != "main" {
		t.Errorf("CallerName: got %q, want %q", e.CallerName, "main")
	}
	if e.CalleeName != "greet" {
		t.Errorf("CalleeName: got %q, want %q", e.CalleeName, "greet")
	}
	if e.CallerFile != "main.go" {
		t.Errorf("CallerFile: got %q, want %q", e.CallerFile, "main.go")
	}
	if e.CalleeFile != "main.go" {
		t.Errorf("CalleeFile: got %q, want %q", e.CalleeFile, "main.go")
	}
	if e.Line != 3 { // 0-indexed line 2 → 1-indexed line 3
		t.Errorf("Line: got %d, want 3", e.Line)
	}
	if e.CallerLine != 1 { // 0-indexed line 0 → 1-indexed line 1
		t.Errorf("CallerLine: got %d, want 1", e.CallerLine)
	}
}

func TestConvertToEdges_SkipLocalSymbols(t *testing.T) {
	t.Parallel()
	doc := &sciplib.Document{
		RelativePath: "main.go",
		Occurrences: []*sciplib.Occurrence{
			{
				Range:       []int32{0, 5, 9},
				Symbol:      "go . testpkg main().",
				SymbolRoles: int32(sciplib.SymbolRole_Definition),
			},
			// local symbol reference — should be skipped
			{
				Range:  []int32{2, 4, 9},
				Symbol: "local 1",
			},
		},
		Symbols: []*sciplib.SymbolInformation{
			{Symbol: "go . testpkg main().", Kind: sciplib.SymbolInformation_Function, DisplayName: "main"},
		},
	}

	idx := &gocodescip.Index{Documents: []*sciplib.Document{doc}}
	edges := gocodescip.ConvertToEdges(idx)

	if len(edges) != 0 {
		t.Fatalf("expected 0 edges (local symbols skipped), got %d: %+v", len(edges), edges)
	}
}

func TestConvertToEdges_SkipSelfCalls(t *testing.T) {
	t.Parallel()
	// Reference to a symbol that has no definition (external) — still skipped for caller resolution
	doc := &sciplib.Document{
		RelativePath: "main.go",
		Occurrences: []*sciplib.Occurrence{
			{
				Range:       []int32{0, 5, 9},
				Symbol:      "go . testpkg main().",
				SymbolRoles: int32(sciplib.SymbolRole_Definition),
			},
			// reference to an external symbol we can't resolve
			{
				Range:  []int32{2, 4, 9},
				Symbol: "go . external Foo().",
			},
		},
		Symbols: []*sciplib.SymbolInformation{
			{Symbol: "go . testpkg main().", Kind: sciplib.SymbolInformation_Function, DisplayName: "main"},
		},
	}

	idx := &gocodescip.Index{Documents: []*sciplib.Document{doc}}
	edges := gocodescip.ConvertToEdges(idx)

	// external symbol not in defMap → edge still emitted with empty CalleeFile
	// but CallerName must be resolved
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge for external callee, got %d", len(edges))
	}
	if edges[0].CalleeFile != "" {
		t.Errorf("expected empty CalleeFile for external symbol, got %q", edges[0].CalleeFile)
	}
}

func TestConvertToEdges_MultiDocument(t *testing.T) {
	t.Parallel()
	docA := &sciplib.Document{
		RelativePath: "a.go",
		Occurrences: []*sciplib.Occurrence{
			{
				Range:       []int32{0, 5, 8},
				Symbol:      "go . pkg foo().",
				SymbolRoles: int32(sciplib.SymbolRole_Definition),
			},
		},
		Symbols: []*sciplib.SymbolInformation{
			{Symbol: "go . pkg foo().", Kind: sciplib.SymbolInformation_Function, DisplayName: "foo"},
		},
	}
	docB := &sciplib.Document{
		RelativePath: "b.go",
		Occurrences: []*sciplib.Occurrence{
			{
				Range:       []int32{0, 5, 9},
				Symbol:      "go . pkg bar().",
				SymbolRoles: int32(sciplib.SymbolRole_Definition),
			},
			// call to foo from bar
			{
				Range:  []int32{2, 4, 7},
				Symbol: "go . pkg foo().",
			},
		},
		Symbols: []*sciplib.SymbolInformation{
			{Symbol: "go . pkg bar().", Kind: sciplib.SymbolInformation_Function, DisplayName: "bar"},
		},
	}

	idx := &gocodescip.Index{Documents: []*sciplib.Document{docA, docB}}
	edges := gocodescip.ConvertToEdges(idx)

	if len(edges) != 1 {
		t.Fatalf("expected 1 cross-file edge, got %d: %+v", len(edges), edges)
	}

	e := edges[0]
	if e.CallerName != "bar" {
		t.Errorf("CallerName: got %q, want %q", e.CallerName, "bar")
	}
	if e.CalleeName != "foo" {
		t.Errorf("CalleeName: got %q, want %q", e.CalleeName, "foo")
	}
	if e.CallerFile != "b.go" {
		t.Errorf("CallerFile: got %q, want %q", e.CallerFile, "b.go")
	}
	if e.CalleeFile != "a.go" {
		t.Errorf("CalleeFile: got %q, want %q", e.CalleeFile, "a.go")
	}
}
