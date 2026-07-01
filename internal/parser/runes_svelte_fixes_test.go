package parser_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestRuneDestructuredProps verifies that destructured $props() emits a KindRune
// symbol for EACH destructured binding name (RuneKind="props"), in addition to
// the "$props" token symbol. This is the Phase-2 destructured-$props() capability:
// before it, only the "$props" token was emitted and the individual prop names
// were not discoverable.
//
// The table exercises every destructuring form the doc comment on
// destructuredBindingNames claims to support, so the behaviour matrix is TESTED,
// not just described. The rename+default form (pair_pattern > assignment_pattern)
// is the review HIGH: it dropped its binding pre-fix.
func TestRuneDestructuredProps(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // props-rune symbol names that MUST be present
	}{
		{
			name: "multi-name shorthand + default",
			src:  "<script>\n  let { name = \"anon\", count } = $props();\n</script>",
			want: []string{"$props", "name", "count"},
		},
		{
			// pair_pattern{ value: assignment_pattern{ left: identifier } } — the HIGH.
			// The local binding is the alias `n`; assert it is emitted.
			name: "renamed binding with default",
			src:  "<script>\n  let { name: n = \"anon\" } = $props();\n</script>",
			want: []string{"$props", "n"},
		},
		{
			// pair_pattern{ value: identifier } — already handled; locks in coverage.
			name: "renamed binding",
			src:  "<script>\n  let { key: alias } = $props();\n</script>",
			want: []string{"$props", "alias"},
		},
		{
			// rest_pattern{ identifier } — already handled; locks in coverage.
			name: "rest element",
			src:  "<script>\n  let { a, ...rest } = $props();\n</script>",
			want: []string{"$props", "a", "rest"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := parser.ParseFile("destruct.svelte", []byte(c.src), parser.ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			propSyms := runeSymbolsWithKind(result.Symbols, "props")
			byName := make(map[string]*parser.Symbol, len(propSyms))
			for _, s := range propSyms {
				byName[s.Name] = s
			}
			for _, want := range c.want {
				s, ok := byName[want]
				if !ok {
					t.Errorf("missing props rune %q; got %v", want, runeSymbolNames(result.Symbols))
					continue
				}
				if s.Kind != parser.KindRune {
					t.Errorf("%q: Kind = %q, want rune", want, s.Kind)
				}
				if s.RuneKind != "props" {
					t.Errorf("%q: RuneKind = %q, want props", want, s.RuneKind)
				}
			}
		})
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
	// Dual-emit: expect 2 symbols: "stop" (variable) and "$inspect:L<line>" (token).
	if len(inspectSyms) < 2 {
		t.Fatalf("expected at least 2 inspect runes for assignment-form chain (variable + token), got %d: %v",
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

// TestRuneDualEmitStatementForm verifies that two $effect statements (expression_statement
// form, no variable binding) in the same file produce distinct KindRune symbols.
//
// This is the most common real-world $effect usage. Before the fix, both statements
// produced Name="$effect", causing a (repo_key, file_path, symbol_name) PRIMARY KEY
// collision in the DB. After the fix, each emits "$effect:L<n>" with its own line number.
//
// Also covers $effect.pre, which uses the same expression_statement code path.
func TestRuneDualEmitStatementForm(t *testing.T) {
	src := []byte(`<script>
  $effect(() => { console.log('a'); });
  $effect(() => { console.log('b'); });
  $effect.pre(() => { console.log('pre-a'); });
  $effect.pre(() => { console.log('pre-b'); });
</script>`)
	result, err := parser.ParseFile("dual_stmt.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Collect all effect rune symbols that have the ":L" suffix (statement form).
	var stmtSyms []*parser.Symbol
	for _, s := range result.Symbols {
		if s.Kind == parser.KindRune && s.RuneKind == "effect" && strings.Contains(s.Name, ":L") {
			stmtSyms = append(stmtSyms, s)
		}
	}

	// 2 $effect + 2 $effect.pre statements → 4 distinct line-suffixed symbols.
	if len(stmtSyms) != 4 {
		t.Fatalf("expected 4 effect:L* symbols (2 $effect + 2 $effect.pre), got %d: %v",
			len(stmtSyms), runeSymbolNames(result.Symbols))
	}

	// All Names must be distinct (no PK collision).
	seen := make(map[string]bool, len(stmtSyms))
	for _, s := range stmtSyms {
		if seen[s.Name] {
			t.Errorf("duplicate statement-form symbol Name=%q (PK collision); all: %v",
				s.Name, runeSymbolNames(result.Symbols))
		}
		seen[s.Name] = true
		if s.Kind != parser.KindRune {
			t.Errorf("symbol %q: Kind=%q, want rune", s.Name, s.Kind)
		}
		if s.RuneKind != "effect" {
			t.Errorf("symbol %q: RuneKind=%q, want effect", s.Name, s.RuneKind)
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
	// Dual-emit: bound $state(0) emits 2 symbols (variable name + "$state:L<n>" token).
	if len(stateSyms) < 2 {
		t.Fatalf("expected at least 2 state runes in .svelte.ts (variable + token), got %d: %v",
			len(stateSyms), runeSymbolNames(result.Symbols))
	}
	for _, s := range stateSyms {
		if s.Kind != parser.KindRune {
			t.Errorf("state rune %q: Kind = %q, want rune", s.Name, s.Kind)
		}
	}
}
