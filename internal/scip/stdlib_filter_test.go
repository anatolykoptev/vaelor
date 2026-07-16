package scip

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
)

func TestStdlibFilterSkipsUnresolvedStdlibCalls(t *testing.T) {
	idx := &Index{
		Documents: []*sciplib.Document{
			{
				RelativePath: "main.rs",
				Symbols: []*sciplib.SymbolInformation{
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/MyFunc().",
						DisplayName: "MyFunc",
						Kind:        sciplib.SymbolInformation_Function,
					},
				},
				Occurrences: []*sciplib.Occurrence{
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/MyFunc().",
						SymbolRoles: int32(sciplib.SymbolRole_Definition),
						Range:       []int32{0, 0},
					},
					// Reference to clone (stdlib) at line 2 — should be filtered
					{
						Symbol:      "rust-analyzer cargo std 1.0.0 core/option/Option#clone().",
						SymbolRoles: 0,
						Range:       []int32{1, 5},
					},
					// Reference to MyFunc (project) at line 3 — should be kept
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/MyFunc().",
						SymbolRoles: 0,
						Range:       []int32{2, 5},
					},
				},
			},
		},
	}

	edges := ConvertToEdges(idx)

	var nonImplEdges []goanalysis.TypedEdge
	for _, e := range edges {
		if !e.IsInterface {
			nonImplEdges = append(nonImplEdges, e)
		}
	}

	if len(nonImplEdges) != 1 {
		t.Fatalf("expected 1 call edge (clone filtered), got %d: %+v", len(nonImplEdges), nonImplEdges)
	}
	if nonImplEdges[0].CalleeName != "MyFunc" {
		t.Errorf("expected callee MyFunc, got %s", nonImplEdges[0].CalleeName)
	}
}

func TestStdlibFilterKeepsProjectMethodsWithStdlibName(t *testing.T) {
	idx := &Index{
		Documents: []*sciplib.Document{
			{
				RelativePath: "main.rs",
				Symbols: []*sciplib.SymbolInformation{
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/MyFunc().",
						DisplayName: "MyFunc",
						Kind:        sciplib.SymbolInformation_Function,
					},
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/Custom#clone().",
						DisplayName: "clone",
						Kind:        sciplib.SymbolInformation_Method,
					},
				},
				Occurrences: []*sciplib.Occurrence{
					// Definition of MyFunc at line 1
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/MyFunc().",
						SymbolRoles: int32(sciplib.SymbolRole_Definition),
						Range:       []int32{0, 0},
					},
					// Definition of Custom::clone at line 5 (in a different file/section)
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/Custom#clone().",
						SymbolRoles: int32(sciplib.SymbolRole_Definition),
						Range:       []int32{4, 0},
					},
					// Reference to project's clone method at line 2 — should be kept
					{
						Symbol:      "rust-analyzer cargo mycrate 0.1.0 main.rs/Custom#clone().",
						SymbolRoles: 0,
						Range:       []int32{1, 5},
					},
				},
			},
		},
	}

	edges := ConvertToEdges(idx)

	var nonImplEdges []goanalysis.TypedEdge
	for _, e := range edges {
		if !e.IsInterface {
			nonImplEdges = append(nonImplEdges, e)
		}
	}

	if len(nonImplEdges) != 1 {
		t.Fatalf("expected 1 edge (project clone kept), got %d", len(nonImplEdges))
	}
	if nonImplEdges[0].CalleeName != "clone" {
		t.Errorf("expected callee 'clone', got %s", nonImplEdges[0].CalleeName)
	}
}
