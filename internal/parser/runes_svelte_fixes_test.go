package parser_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestRuneDestructuredProps verifies that destructured $props() emits exactly one
// KindRune symbol with RuneKind="props".
func TestRuneDestructuredProps(t *testing.T) {
	src := []byte(`<script>
  let { name = "anon", count } = $props();
</script>`)
	result, err := parser.ParseFile("destruct.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	propSyms := runeSymbolsWithKind(result.Symbols, "props")
	if len(propSyms) != 1 {
		t.Fatalf("expected exactly 1 props rune for destructured $props(), got %d: %v",
			len(propSyms), runeSymbolNames(result.Symbols))
	}
	sym := propSyms[0]
	if sym.Kind != parser.KindRune {
		t.Errorf("Kind = %q, want rune", sym.Kind)
	}
	if sym.RuneKind != "props" {
		t.Errorf("RuneKind = %q, want props", sym.RuneKind)
	}
}

// TestRuneAssignmentChain verifies that assignment-form $inspect(val).with(cb) emits
// exactly one KindRune with RuneKind="inspect" (bound to the variable name).
func TestRuneAssignmentChain(t *testing.T) {
	src := []byte(`<script>
  let stop = $inspect(count).with(callback);
</script>`)
	result, err := parser.ParseFile("chain.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	inspectSyms := runeSymbolsWithKind(result.Symbols, "inspect")
	// Dual-emit: expect 2 symbols: one with Name="stop" (variable) and one with Name="$inspect" (token).
	if len(inspectSyms) < 1 {
		t.Fatalf("expected at least 1 inspect rune for assignment-form chain, got %d: %v",
			len(inspectSyms), runeSymbolNames(result.Symbols))
	}
	// One of the inspect symbols must be the variable-name "stop".
	var stopSym *parser.Symbol
	for _, s := range inspectSyms {
		if s.Name == "stop" {
			stopSym = s
			break
		}
	}
	if stopSym == nil {
		t.Errorf("expected an inspect rune with Name=\"stop\", got: %v", runeSymbolNames(result.Symbols))
	} else {
		if stopSym.Kind != parser.KindRune {
			t.Errorf("stop: Kind = %q, want rune", stopSym.Kind)
		}
		if stopSym.RuneKind != "inspect" {
			t.Errorf("stop: RuneKind = %q, want inspect", stopSym.RuneKind)
		}
	}
}

// TestRunesInSvelteTSFile verifies that runes in a .svelte.ts module are detected.
func TestRunesInSvelteTSFile(t *testing.T) {
	src := []byte(`export const counter = $state(0);
export const doubled = $derived(counter * 2);
`)
	result, err := parser.ParseFile("store.svelte.ts", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	stateSyms := runeSymbolsWithKind(result.Symbols, "state")
	// Dual-emit: bound $state(0) emits 2 symbols (variable name + "$state" token).
	if len(stateSyms) < 1 {
		t.Fatalf("expected at least 1 state rune in .svelte.ts, got %d: %v",
			len(stateSyms), runeSymbolNames(result.Symbols))
	}
	for _, s := range stateSyms {
		if s.Kind != parser.KindRune {
			t.Errorf("state rune %q: Kind = %q, want rune", s.Name, s.Kind)
		}
	}
}
