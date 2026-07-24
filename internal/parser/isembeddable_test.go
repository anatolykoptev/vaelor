package parser_test

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestIsEmbeddableKind verifies the single predicate that gates which symbol
// kinds enter the semantic embedding index. The bulk, incremental, and cache
// paths in internal/embeddings all call this predicate; a divergence between
// them causes add-then-delete churn on re-index.
//
// Falsification: revert IsEmbeddableKind to return true for only
// KindFunction/KindMethod (the pre-fix behaviour) → the type-kind assertions
// go RED.
func TestIsEmbeddableKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind parser.NodeKind
		want bool
	}{
		// Indexed — the historical set plus type-level symbols (#655).
		{parser.KindFunction, true},
		{parser.KindMethod, true},
		{parser.KindType, true},
		{parser.KindStruct, true},
		{parser.KindInterface, true},
		{parser.KindClass, true},

		// Excluded — high volume / low retrieval value / structural.
		{parser.KindConst, false},
		{parser.KindVar, false},
		{parser.KindModule, false},
		{parser.KindImport, false},
		{parser.KindRune, false},

		// Unknown / empty kind must be false (defensive — never index an
		// unrecognised kind, even if a future handler emits one).
		{"", false},
		{"field", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			if got := parser.IsEmbeddableKind(tt.kind); got != tt.want {
				t.Errorf("IsEmbeddableKind(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}
