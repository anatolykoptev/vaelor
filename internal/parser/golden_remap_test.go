package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestTSLangRemapGolden freezes the plain-TypeScript (tsLang) symbol-remap path
// output for the astro/svelte/vue corpus. parseWithTSAndRemap is the top-1%-
// centrality core these three handlers share; Phase 1 extracts its remap core
// into parseVirtualWithRemap and re-expresses parseWithTSAndRemap as a thin
// tsLang delegation. The golden is byte-captured on the pre-extraction tree
// (UPDATE_GOLDEN=1), so a post-extraction divergence in any symbol's
// language/name/kind/line fails here — the byte-identical proof for the
// extract-and-share.
func TestTSLangRemapGolden(t *testing.T) {
	t.Parallel()
	got := snapshotRemapCorpus(t)
	goldenPath := filepath.Join("testdata", "golden", "tslang_remap.txt")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden updated: %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run once with UPDATE_GOLDEN=1 to capture): %v", err)
	}
	if got != string(want) {
		t.Errorf("tsLang remap path diverged from golden.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// snapshotRemapCorpus parses every astro/svelte/vue fixture through ParseFile
// (which routes to the handlers that call parseWithTSAndRemap) and serialises
// each emitted symbol as "<dir>/<file>|<lang>|<name>|<kind>|<start>|<end>",
// sorted for determinism.
func snapshotRemapCorpus(t *testing.T) string {
	t.Helper()
	var lines []string
	for _, dir := range []string{"astro", "svelte", "vue"} {
		base := filepath.Join("testdata", dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatalf("read dir %s: %v", base, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(base, e.Name())
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			result, err := parser.ParseFile(e.Name(), src, parser.ParseOpts{IncludeImports: true})
			if err != nil {
				t.Fatalf("parse %s: %v", p, err)
			}
			for _, s := range result.Symbols {
				lines = append(lines, fmt.Sprintf("%s/%s|%s|%s|%s|%d|%d",
					dir, e.Name(), s.Language, s.Name, s.Kind, s.StartLine, s.EndLine))
			}
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}
