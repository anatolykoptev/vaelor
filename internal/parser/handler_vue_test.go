package parser_test

import (
	"os"
	"path/filepath"
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
