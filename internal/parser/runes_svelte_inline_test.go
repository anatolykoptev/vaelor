package parser_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestRuneVariants verifies dotted variants ($state.raw, $derived.by, etc.) are classified.
func TestRuneVariants(t *testing.T) {
	src := []byte(`<script>
  let raw = $state.raw([]);
  let sum = $derived.by(() => 1);
  $effect.pre(() => {});
  $effect.root(() => {});
  let id = $props.id();
  $inspect.with(x, console.log);
</script>`)

	result, err := parser.ParseFile("variant.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	byName := make(map[string]*parser.Symbol, len(result.Symbols))
	for _, s := range result.Symbols {
		byName[s.Name] = s
	}

	variantCases := []struct {
		name     string
		runeKind string
	}{
		{"raw", "state"},
		{"sum", "derived"},
		{"id", "props"},
	}
	for _, c := range variantCases {
		sym, ok := byName[c.name]
		if !ok {
			t.Errorf("missing symbol %q; got %v", c.name, runeSymbolNames(result.Symbols))
			continue
		}
		if sym.Kind != parser.KindRune {
			t.Errorf("symbol %q: Kind = %q, want rune", c.name, sym.Kind)
		}
		if sym.RuneKind != c.runeKind {
			t.Errorf("symbol %q: RuneKind = %q, want %q", c.name, sym.RuneKind, c.runeKind)
		}
	}

	// $effect.pre and $effect.root produce standalone rune symbols.
	if len(runeSymbolsWithKind(result.Symbols, "effect")) < 2 {
		t.Errorf("expected at least 2 effect rune symbols (pre+root), got %d", len(runeSymbolsWithKind(result.Symbols, "effect")))
	}
}

// TestRunesNotInTypeScript verifies the rune detector does NOT fire on plain .ts files.
func TestRunesNotInTypeScript(t *testing.T) {
	src := []byte(`let count = $state(0);
let doubled = $derived(count * 2);
$effect(() => console.log(count));
`)
	result, err := parser.ParseFile("notasvelte.ts", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, s := range result.Symbols {
		if s.Kind == parser.KindRune {
			t.Errorf("KindRune must not appear in .ts files, got symbol %q", s.Name)
		}
	}
}

// TestRuneEffectAnonymous verifies standalone $effect(...) produces a KindRune symbol.
func TestRuneEffectAnonymous(t *testing.T) {
	src := []byte(`<script>
  $effect(() => console.log("hello"));
</script>`)
	result, err := parser.ParseFile("effect_anon.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	found := false
	for _, s := range result.Symbols {
		if s.Kind == parser.KindRune && s.RuneKind == "effect" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected KindRune with RuneKind=effect for standalone $effect; got %v", runeSymbolNames(result.Symbols))
	}
}

// TestRuneLineNumbers verifies StartLine is remapped to original .svelte coordinates.
func TestRuneLineNumbers(t *testing.T) {
	src := []byte(`<script>
  let count = $state(0);
  let doubled = $derived(count * 2);
</script>`)
	result, err := parser.ParseFile("lines.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	byName := make(map[string]*parser.Symbol, len(result.Symbols))
	for _, s := range result.Symbols {
		byName[s.Name] = s
	}
	if s, ok := byName["count"]; ok {
		if s.StartLine != 2 {
			t.Errorf("count.StartLine = %d, want 2", s.StartLine)
		}
	} else {
		t.Errorf("missing symbol 'count'")
	}
	if s, ok := byName["doubled"]; ok {
		if s.StartLine != 3 {
			t.Errorf("doubled.StartLine = %d, want 3", s.StartLine)
		}
	} else {
		t.Errorf("missing symbol 'doubled'")
	}
}
