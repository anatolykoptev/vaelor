package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestSvelteRunesSingleParsePath is the byte-identical regression guard for the
// issue #401 change that collapsed the former double tree-sitter parse of a
// .svelte component's <script> bytes (one parse for the tags query, a second for
// rune classification) into a single parse whose one tree drives both.
//
// It pins the COMPLETE set of KindRune symbols emitted for runes_basic.svelte —
// every rune name, its RuneKind, and its remapped (original-file) start/end line —
// via the public ParseFile path (which routes to svelteHandler.Parse ->
// parseSvelteWithRunes). Any drift in the single-parse rune output, remap, or
// ordering fails here.
func TestSvelteRunesSingleParsePath(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("testdata", "svelte", "runes_basic.svelte"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := parser.ParseFile("runes_basic.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if result.Language != "svelte" {
		t.Errorf("Language = %q, want svelte", result.Language)
	}

	var got []string
	for _, s := range result.Symbols {
		if s.Kind != parser.KindRune {
			continue
		}
		if s.Language != "svelte" {
			t.Errorf("rune %q: Language = %q, want svelte", s.Name, s.Language)
		}
		got = append(got, fmt.Sprintf("%s|%s|%d|%d", s.Name, s.RuneKind, s.StartLine, s.EndLine))
	}
	sort.Strings(got)

	// Golden set: name|runeKind|startLine|endLine, all in original .svelte
	// coordinates. Derived from the fixture; must stay stable across the
	// single-parse refactor.
	want := []string{
		"$bindable:L14|bindable|14|14",
		"$derived:L3|derived|3|3",
		"$derived:L8|derived|8|8",
		"$effect:L10|effect|10|10",
		"$effect:L11|effect|11|11",
		"$effect:L12|effect|12|12",
		"$effect:L4|effect|4|4",
		"$effect:L9|effect|9|9",
		"$host:L17|host|17|17",
		"$inspect:L15|inspect|15|15",
		"$inspect:L16|inspect|16|16",
		"$props:L13|props|13|13",
		"$props|props|5|5",
		"$state:L2|state|2|2",
		"$state:L6|state|6|6",
		"$state:L7|state|7|7",
		"count|state|2|2",
		"doubled|derived|3|3",
		"eager|state|7|7",
		"h|host|17|17",
		"id|props|13|13",
		"name|props|5|5",
		"raw|state|6|6",
		"sum|derived|8|8",
		"val|bindable|14|14",
	}

	if len(got) != len(want) {
		t.Fatalf("rune symbol count = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rune symbol[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
