package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestRuneTokenNameNormalization is a table-driven test for the runeTokenName
// normalization behavior. It verifies that dotted rune variants produce the
// expected root token name embedded in the secondary symbol Name (e.g. "$state:L<n>").
//
// runeTokenName itself is unexported, so we test its observable output via ParseFile:
// each bound rune declaration emits a secondary symbol with Name="$<root>:L<line>".
// This covers the full normalisation path including dotted variants.
func TestRuneTokenNameNormalization(t *testing.T) {
	cases := []struct {
		name          string
		declaration   string // e.g. "let x = $state.raw([]);"
		wantTokenRoot string // e.g. "$state" — prefix of the secondary symbol name
		wantRuneKind  string // e.g. "state"
	}{
		{"state_plain", "let x = $state(0);", "$state", "state"},
		{"state_raw", "let x = $state.raw([]);", "$state", "state"},
		{"state_eager", "let x = $state.eager(0);", "$state", "state"},
		{"derived_plain", "let x = $derived(1+1);", "$derived", "derived"},
		{"derived_by", "let x = $derived.by(() => 1);", "$derived", "derived"},
		{"effect_plain", "let x = $effect(() => {});", "$effect", "effect"},
		{"props_plain", "let x = $props();", "$props", "props"},
		{"props_id", "let x = $props.id();", "$props", "props"},
		{"host_plain", "let x = $host();", "$host", "host"},
		{"bindable_plain", "let x = $bindable(0);", "$bindable", "bindable"},
		{"inspect_plain", "let x = $inspect(1);", "$inspect", "inspect"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(fmt.Sprintf("<script>\n  %s\n</script>", c.declaration))
			result, err := parser.ParseFile("token_norm_test.svelte", src, parser.ParseOpts{})
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}

			// Look for a secondary symbol whose Name starts with "$<root>:L"
			prefix := c.wantTokenRoot + ":L"
			var tokenSym *parser.Symbol
			for _, s := range result.Symbols {
				if strings.HasPrefix(s.Name, prefix) {
					tokenSym = s
					break
				}
			}

			if tokenSym == nil {
				t.Fatalf("no secondary symbol with prefix %q found; all symbols: %v",
					prefix, runeSymbolNames(result.Symbols))
			}

			if tokenSym.Kind != parser.KindRune {
				t.Errorf("token symbol %q Kind=%q, want rune", tokenSym.Name, tokenSym.Kind)
			}
			if tokenSym.RuneKind != c.wantRuneKind {
				t.Errorf("token symbol %q RuneKind=%q, want %q", tokenSym.Name, tokenSym.RuneKind, c.wantRuneKind)
			}
		})
	}
}
