package parser_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestRuneVariants verifies dotted variants ($state.raw, $derived.by, etc.) are classified.
func TestRuneVariants(t *testing.T) {
	src := []byte(`<script>
  let raw = $state.raw([]);
  let eager = $state.eager(0);
  let sum = $derived.by(() => 1);
  $effect.pre(() => {});
  $effect.root(() => {});
  $effect.tracking();
  $effect.pending();
  let id = $props.id();
  $inspect.trace(count);
  let h = $host();
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
		{"eager", "state"},
		{"sum", "derived"},
		{"id", "props"},
		{"h", "host"},
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

	// $effect.pre, $effect.root, $effect.tracking, $effect.pending produce standalone rune symbols.
	if len(runeSymbolsWithKind(result.Symbols, "effect")) < 4 {
		t.Errorf("expected at least 4 effect rune symbols (pre+root+tracking+pending), got %d",
			len(runeSymbolsWithKind(result.Symbols, "effect")))
	}
	// $inspect.trace produces an inspect rune symbol.
	if len(runeSymbolsWithKind(result.Symbols, "inspect")) < 1 {
		t.Errorf("expected at least 1 inspect rune ($inspect.trace), got 0")
	}
	// $host produces a host rune symbol.
	if len(runeSymbolsWithKind(result.Symbols, "host")) < 1 {
		t.Errorf("expected at least 1 host rune ($host), got 0")
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

// TestRuneAntiPatterns verifies that Svelte 4 double-dollar variables, internal
// helpers ($.xxx), and chained $inspect.with calls are NOT classified as runes.
func TestRuneAntiPatterns(t *testing.T) {
	// $$slots / $$props — Svelte 4 legacy double-dollar variables.
	src := []byte(`<script>
  let s = $$slots;
  let p = $$props;
  let r = $$restProps;
</script>`)
	result, err := parser.ParseFile("legacy.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, sym := range result.Symbols {
		if sym.Kind == parser.KindRune {
			t.Errorf("$$-prefixed legacy var %q must NOT be classified as KindRune", sym.Name)
		}
	}

	// $.proxy / $.computed — Svelte 5 internal helpers.
	src2 := []byte(`<script>
  let x = $.proxy(obj);
  let y = $.computed(() => 1);
</script>`)
	result2, err := parser.ParseFile("internals.svelte", src2, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile internals: %v", err)
	}
	for _, sym := range result2.Symbols {
		if sym.Kind == parser.KindRune {
			t.Errorf("internal helper %q ($.xxx) must NOT be classified as KindRune", sym.Name)
		}
	}

	// $inspect(value).with(callback) — only the inner $inspect call is a rune.
	// The chained .with(callback) is NOT an independent rune.
	src3 := []byte(`<script>
  $inspect(count).with(console.log);
</script>`)
	result3, err := parser.ParseFile("inspect_chain.svelte", src3, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile inspect_chain: %v", err)
	}
	runeSyms := runeSymbolsWithKind(result3.Symbols, "inspect")
	// Exactly one rune symbol — from $inspect(count), NOT from the .with chain.
	if len(runeSyms) != 1 {
		t.Errorf("$inspect(count).with: expected 1 inspect rune (from $inspect), got %d: %v",
			len(runeSyms), runeSymbolNames(result3.Symbols))
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
// TestRuneDualEmit verifies that bound rune declarations emit BOTH the variable name
// and the rune token name as separate KindRune symbols with the same line/kind.
//
// This enables both:
//   - symbol_search query="count"  -> finds the declaration site
//   - symbol_search query="$state" -> finds every $state site in the repo
func TestRuneDualEmit(t *testing.T) {
	src := []byte(`<script>
  let count = $state(0);
  let doubled = $derived(count * 2);
  let sum = $derived.by(() => count + 1);
</script>`)

	result, err := parser.ParseFile("dual_emit.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Build a multi-map: name -> list of symbols.
	byName := make(map[string][]*parser.Symbol)
	for _, s := range result.Symbols {
		byName[s.Name] = append(byName[s.Name], s)
	}

	// let count = $state(0) -> must emit "count" AND "$state"
	if _, ok := byName["count"]; !ok {
		t.Errorf("missing symbol %q; got %v", "count", runeSymbolNames(result.Symbols))
	}
	stateSyms, hasState := byName["$state"]
	if !hasState {
		t.Errorf("missing symbol %q; got %v", "$state", runeSymbolNames(result.Symbols))
	} else {
		for _, s := range stateSyms {
			if s.Kind != parser.KindRune {
				t.Errorf("$state symbol Kind=%q, want rune", s.Kind)
			}
			if s.RuneKind != "state" {
				t.Errorf("$state symbol RuneKind=%q, want state", s.RuneKind)
			}
		}
	}

	// let doubled = $derived(count * 2) -> must emit "doubled" AND "$derived"
	if _, ok := byName["doubled"]; !ok {
		t.Errorf("missing symbol %q; got %v", "doubled", runeSymbolNames(result.Symbols))
	}
	derivedSyms, hasDerived := byName["$derived"]
	if !hasDerived {
		t.Errorf("missing symbol %q; got %v", "$derived", runeSymbolNames(result.Symbols))
	} else {
		for _, s := range derivedSyms {
			if s.RuneKind != "derived" {
				t.Errorf("$derived symbol RuneKind=%q, want derived", s.RuneKind)
			}
		}
	}

	// let sum = $derived.by(...) -> must emit "sum" AND "$derived" (normalised from $derived.by)
	if _, ok := byName["sum"]; !ok {
		t.Errorf("missing symbol %q; got %v", "sum", runeSymbolNames(result.Symbols))
	}
	if sumSyms, ok := byName["sum"]; ok {
		for _, s := range sumSyms {
			if s.RuneKind != "derived" {
				t.Errorf("sum symbol RuneKind=%q, want derived", s.RuneKind)
			}
		}
	}

	// The $state symbol must share StartLine with the "count" symbol.
	countSyms := byName["count"]
	if len(countSyms) > 0 && hasState {
		countLine := countSyms[0].StartLine
		found := false
		for _, s := range stateSyms {
			if s.StartLine == countLine {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no $state symbol on line %d (count line); $state symbols: %v",
				countLine, stateSyms)
		}
	}
}
