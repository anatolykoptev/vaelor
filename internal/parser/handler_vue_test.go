package parser_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestParseVueScriptSetupTs(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "vue", "script_setup_ts.vue"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("script_setup_ts.vue", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "vue" {
		t.Errorf("Language = %q, want vue", result.Language)
	}

	// increment is defined on line 10 of the .vue file.
	var increment *parser.Symbol
	for _, s := range result.Symbols {
		if s.Name == "increment" {
			increment = s
			break
		}
	}
	if increment == nil {
		t.Fatalf("no symbol named 'increment'; symbols: %v", vueSymbolNames(result.Symbols))
	}
	if increment.Kind != parser.KindFunction {
		t.Errorf("increment.Kind = %q, want function", increment.Kind)
	}
	// increment is on line 10 of the original .vue file (1-indexed).
	if increment.StartLine != 10 {
		t.Errorf("increment.StartLine = %d, want 10", increment.StartLine)
	}
}

func TestParseVueBothScripts(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "vue", "both_scripts.vue"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("both_scripts.vue", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "vue" {
		t.Errorf("Language = %q, want vue", result.Language)
	}

	names := vueSymbolNames(result.Symbols)

	// greet is in the <script setup> block.
	if !vueContainsName(names, "greet") {
		t.Errorf("missing 'greet' symbol from <script setup>; symbols: %v", names)
	}
}

// vueCallGarbled reports whether a call site carries a cross-region
// error-recovery signature: a receiver spanning the </script> boundary into the
// markup, containing a newline, or an unbalanced brace — the shape the pre-#409
// whole-file CallsQuery produced when template bytes reached the TS parser.
func vueCallGarbled(c parser.CallSite) bool {
	return strings.ContainsAny(c.Receiver, "\n{}") || strings.Contains(c.Receiver, "</script>")
}

// TestVueCallsCleanSet is the correctness gate for issue #409: Vue call
// extraction must come from the isolated <script> region (ScriptCalls), never a
// raw CallsQuery over the whole .vue file. The whole-file path relied on
// tree-sitter-typescript swallowing <template> as an opaque glimmer_template node;
// when that swallow degrades (template markup reaching the TS parser, or a grammar
// version change) it surfaces GARBLED cross-region calls — a template identifier
// captured with a receiver spanning </script> into the markup.
//
// Part 1 pins the clean set on an idiomatic SFC (identical before and after the
// fix — <template> is glimmer-swallowed, so no regression). Part 2 is the RED->
// GREEN proof: on a Vue source whose template expression reaches the TS parser,
// the old path leaked a garbled `greet` call (recv="ref(0)\n</script>\n<p"); the
// two-region split emits only the clean script call.
func TestVueCallsCleanSet(t *testing.T) {
	t.Parallel()

	// Part 1 — idiomatic <script setup> + <template> (mustache / @event / v-for
	// method calls). Extracted set is EXACTLY the clean script-region calls; Vue
	// template-expression extraction is deferred (#409), so no template call leaks
	// as garbage and no edge is duplicated.
	fx, err := os.ReadFile(filepath.Join("testdata", "vue", "script_and_template.vue"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cs, err := parser.ExtractCalls("script_and_template.vue", fx, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	counts := map[string]int{}
	for _, c := range cs {
		if c.IsArgRef {
			t.Errorf("unexpected argref %q (recv=%q); the clean Vue set is script calls only", c.Name, c.Receiver)
		}
		if vueCallGarbled(c) {
			t.Errorf("garbled call in idiomatic SFC: %q recv=%q (#409 cross-region capture)", c.Name, c.Receiver)
		}
		counts[c.Name]++
	}
	want := map[string]int{"ref": 1, "fetchUser": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("idiomatic .vue call set = %v, want %v (clean script-region set, no template garble, no duplicate edge)", counts, want)
	}

	// Part 2 — RED->GREEN guard. A Vue template expression reaching the TS parser
	// (the condition the pre-#409 whole-file CallsQuery had no guard against) must
	// NOT surface a garbled template call. The isolated <script>-region extraction
	// makes the leak impossible.
	degraded := []byte("<script setup>\nconst x = ref(0)\n</script>\n<p>{{ user.greet() }}</p>\n")
	ds, err := parser.ExtractCalls("degraded.vue", degraded, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls(degraded): %v", err)
	}
	for _, c := range ds {
		if vueCallGarbled(c) {
			t.Errorf("garbled cross-region call leaked: %q recv=%q (whole-file CallsQuery over raw .vue; #409)", c.Name, c.Receiver)
		}
		if c.Name == "greet" {
			t.Errorf("template call %q leaked from the markup region (recv=%q); Vue calls must come from ScriptCalls only (#409)", c.Name, c.Receiver)
		}
	}
}

func vueSymbolNames(syms []*parser.Symbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.Name)
	}
	return names
}

func vueContainsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
