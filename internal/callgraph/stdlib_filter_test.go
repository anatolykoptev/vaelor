package callgraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestFilterStdlibCalls_RemovesUnresolvedStdlib(t *testing.T) {
	resolvedSym := &parser.Symbol{Name: "Clone", Kind: parser.KindFunction, File: "/src/main.go"}
	edges := []CallEdge{
		// Unresolved stdlib method — should be filtered.
		{CalleeName: "clone", Line: 10},
		{CalleeName: "unwrap", Line: 11},
		{CalleeName: "to_string", Line: 12},
		// Resolved project method named "Clone" — must be kept even though
		// "Clone" is in the stdlib set, because Callee is non-nil.
		{Callee: resolvedSym, CalleeName: "Clone", Line: 13},
		// Unresolved non-stdlib name — kept.
		{CalleeName: "myCustomFunc", Line: 14},
	}

	filtered := FilterStdlibCalls(edges)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 edges after filtering, got %d: %+v", len(filtered), filtered)
	}

	// The resolved "Clone" edge must survive.
	hasClone := false
	hasCustom := false
	for _, e := range filtered {
		if e.CalleeName == "Clone" && e.Callee != nil {
			hasClone = true
		}
		if e.CalleeName == "myCustomFunc" {
			hasCustom = true
		}
	}
	if !hasClone {
		t.Error("resolved project method 'Clone' was filtered out — should be kept")
	}
	if !hasCustom {
		t.Error("unresolved non-stdlib 'myCustomFunc' was filtered out — should be kept")
	}
}

func TestFilterStdlibCalls_EmptyInput(t *testing.T) {
	result := FilterStdlibCalls(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty result for nil input, got %d edges", len(result))
	}
}

func TestFilterStdlibCalls_AllStdlib(t *testing.T) {
	edges := []CallEdge{
		{CalleeName: "clone"},
		{CalleeName: "iter"},
		{CalleeName: "map"},
	}
	filtered := FilterStdlibCalls(edges)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 edges (all stdlib), got %d: %+v", len(filtered), filtered)
	}
}
