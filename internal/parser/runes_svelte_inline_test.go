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
// and the line-disambiguated rune token name as separate KindRune symbols.
//
// Secondary symbol names use the format "$token:L<line>" (e.g. "$state:L2") so that
// multiple $state bindings in the same file produce distinct DB rows under the
// (repo_key, file_path, symbol_name) PRIMARY KEY.
//
// This enables both:
//   - symbol_search query="count"  -> finds the declaration site via variable name
//   - symbol_search query="$state" -> finds every $state site via trigram/ILIKE match on "$state:L<n>"
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

	// Helper: collect all symbols whose Name starts with a given prefix.
	byPrefix := func(prefix string) []*parser.Symbol {
		var out []*parser.Symbol
		for _, s := range result.Symbols {
			if len(s.Name) >= len(prefix) && s.Name[:len(prefix)] == prefix {
				out = append(out, s)
			}
		}
		return out
	}
	byExact := func(name string) []*parser.Symbol {
		var out []*parser.Symbol
		for _, s := range result.Symbols {
			if s.Name == name {
				out = append(out, s)
			}
		}
		return out
	}

	// let count = $state(0) -> "count" (exact) + "$state:L2" (token)
	if syms := byExact("count"); len(syms) != 1 {
		t.Errorf("expected 1 symbol named \"count\", got %d: %v", len(syms), runeSymbolNames(result.Symbols))
	}
	stateTokenSyms := byPrefix("$state:L")
	if len(stateTokenSyms) != 1 {
		t.Errorf("expected 1 token symbol matching \"$state:L*\", got %d: %v",
			len(stateTokenSyms), runeSymbolNames(result.Symbols))
	} else {
		s := stateTokenSyms[0]
		if s.Kind != parser.KindRune {
			t.Errorf("$state token symbol Kind=%q, want rune", s.Kind)
		}
		if s.RuneKind != "state" {
			t.Errorf("$state token symbol RuneKind=%q, want state", s.RuneKind)
		}
		// Token symbol must share StartLine with "count" primary symbol.
		countSyms := byExact("count")
		if len(countSyms) > 0 && s.StartLine != countSyms[0].StartLine {
			t.Errorf("$state:L token StartLine=%d, want %d (count line)",
				s.StartLine, countSyms[0].StartLine)
		}
	}

	// let doubled = $derived(count * 2) + let sum = $derived.by(...) -> 2 "$derived:L*" tokens
	derivedTokenSyms := byPrefix("$derived:L")
	if len(derivedTokenSyms) != 2 {
		t.Errorf("expected 2 token symbols matching \"$derived:L*\" (doubled + sum), got %d: %v",
			len(derivedTokenSyms), runeSymbolNames(result.Symbols))
	} else {
		for _, s := range derivedTokenSyms {
			if s.RuneKind != "derived" {
				t.Errorf("$derived token symbol RuneKind=%q, want derived", s.RuneKind)
			}
		}
	}

	// Primary variable symbols must still be present.
	for _, name := range []string{"count", "doubled", "sum"} {
		if syms := byExact(name); len(syms) != 1 {
			t.Errorf("expected 1 primary symbol %q, got %d: %v", name, len(syms), runeSymbolNames(result.Symbols))
		}
	}
}

// TestRuneDualEmitMultiState is the BLOCKER regression test: a file with 2+ $state
// bindings must emit a distinct secondary token symbol for EACH one.
// Before the fix: pipeline dedup (key=file+":"+name) and DB PK (repo,file,symbol_name)
// both collapsed them to one row. After the fix: each gets "$state:L<line>" so all survive.
func TestRuneDualEmitMultiState(t *testing.T) {
	src := []byte(`<script>
  let a = $state(0);
  let b = $state(1);
  let c = $state("hello");
</script>`)

	result, err := parser.ParseFile("multi_state.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Collect all "$state:L*" token symbols.
	var stateTokenSyms []*parser.Symbol
	for _, s := range result.Symbols {
		if len(s.Name) > 7 && s.Name[:8] == "$state:L" {
			stateTokenSyms = append(stateTokenSyms, s)
		}
	}

	// 3 $state bindings -> 3 distinct token symbols: "$state:L2", "$state:L3", "$state:L4"
	if len(stateTokenSyms) != 3 {
		t.Errorf("expected 3 $state:L* token symbols (one per binding), got %d: %v",
			len(stateTokenSyms), runeSymbolNames(result.Symbols))
	}

	// All 3 must have distinct Names (distinct lines).
	seen := make(map[string]bool)
	for _, s := range stateTokenSyms {
		if seen[s.Name] {
			t.Errorf("duplicate token symbol Name=%q; all: %v", s.Name, runeSymbolNames(result.Symbols))
		}
		seen[s.Name] = true
		if s.Kind != parser.KindRune {
			t.Errorf("token symbol %q Kind=%q, want rune", s.Name, s.Kind)
		}
		if s.RuneKind != "state" {
			t.Errorf("token symbol %q RuneKind=%q, want state", s.Name, s.RuneKind)
		}
	}

	// Primary symbols must still be present: "a", "b", "c"
	for _, name := range []string{"a", "b", "c"} {
		found := false
		for _, s := range result.Symbols {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing primary symbol %q; all: %v", name, runeSymbolNames(result.Symbols))
		}
	}
}
