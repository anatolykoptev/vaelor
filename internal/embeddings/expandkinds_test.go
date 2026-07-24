package embeddings

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildEmbedTextExpanded_LOIBody verifies that when expanded=true,
// buildEmbedTextExpanded limits the body to the first few lines (Aider LOI
// approach) instead of the full body. When expanded=false, the output is
// byte-identical to the legacy buildEmbedText (non-regression guard).
//
// Falsification: revert buildEmbedTextExpanded to always delegate to
// buildEmbedText (ignore expanded) → the LOI line-count assertion goes Red
// (full body included instead of LOI subset).
func TestBuildEmbedTextExpanded_LOIBody(t *testing.T) {
	t.Parallel()
	// 20-line body — LOI should include only the first few.
	bodyLines := make([]string, 20)
	for i := range bodyLines {
		bodyLines[i] = "x := " + string(rune('a'+i))
	}
	longBody := strings.Join(bodyLines, "\n")

	sym := &parser.Symbol{
		Language:  "go",
		Kind:      parser.KindFunction,
		Name:      "Big",
		Signature: "func Big()",
		Body:      longBody,
	}

	// Flag OFF — byte-identical to legacy buildEmbedText.
	textOff := buildEmbedTextExpanded(sym, "main.go", false)
	textLegacy := buildEmbedText(sym, "main.go")
	assert.Equal(t, textLegacy, textOff,
		"flag OFF: buildEmbedTextExpanded must be byte-identical to legacy buildEmbedText")

	// Flag ON — LOI body (first loiBodyLines lines only).
	textOn := buildEmbedTextExpanded(sym, "main.go", true)
	assert.Contains(t, textOn, "main.go")
	assert.Contains(t, textOn, "func Big()")
	// First few lines present.
	for i := 0; i < loiBodyLines && i < len(bodyLines); i++ {
		assert.Contains(t, textOn, bodyLines[i], "flag ON: LOI must include early line %q", bodyLines[i])
	}
	// Later lines absent (body was truncated to LOI).
	if len(bodyLines) > loiBodyLines {
		assert.NotContains(t, textOn, bodyLines[loiBodyLines],
			"flag ON: line beyond LOI limit must be absent")
	}
	// LOI text is shorter than full-body text.
	assert.Less(t, len(textOn), len(textOff),
		"flag ON: LOI embed text must be shorter than full-body text")
}

// TestBuildEmbedTextExpanded_IncludesDocComment verifies the @doc docstring is
// included in the expanded embed text (it was already included in the legacy
// path; the expanded path must preserve this).
func TestBuildEmbedTextExpanded_IncludesDocComment(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Name:       "Retry",
		Kind:       parser.KindFunction,
		Signature:  "func Retry(fn func() error) error",
		DocComment: "Retry executes fn with exponential backoff.",
		Body:       "for i := 0; i < 3; i++ { _ = fn() }",
		Language:   "go",
	}
	text := buildEmbedTextExpanded(sym, "foo/bar.go", true)
	assert.Contains(t, text, "exponential backoff",
		"expanded embed text must include @doc docstring")
}

// TestCollectSymbolsExpanded_NewKinds verifies that when expanded=true,
// collectSymbolsExpanded includes macro/module/type-alias symbols. When
// expanded=false, the filtered set is byte-identical to the legacy
// collectSymbols (no new kinds).
//
// Falsification: revert collectSymbolsExpanded to always delegate to
// collectSymbols (ignore expanded) → the expanded=true assertions for
// macro/module go Red.
func TestCollectSymbolsExpanded_NewKinds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "lib.rs", `pub macro_rules! dbg {
    ($($x:expr),*) => {};
}

pub mod net {
    pub fn dial() {}
}

pub type Id = u64;

pub fn init() {}
`)

	// Flag OFF — byte-identical to legacy collectSymbols.
	symsOff, _, err := collectSymbolsExpanded(context.Background(), dir, false)
	require.NoError(t, err)
	symsLegacy, _, err := collectSymbols(context.Background(), dir)
	require.NoError(t, err)
	offNames := sortedSymbolNames(symsOff)
	legacyNames := sortedSymbolNames(symsLegacy)
	assert.Equal(t, legacyNames, offNames,
		"flag OFF: collectSymbolsExpanded must produce the same symbol set as legacy collectSymbols")
	assert.NotContains(t, offNames, "dbg", "flag OFF: macro must NOT be in embed set")
	assert.NotContains(t, offNames, "net", "flag OFF: module must NOT be in embed set")

	// Flag ON — macro/module/type-alias included.
	symsOn, _, err := collectSymbolsExpanded(context.Background(), dir, true)
	require.NoError(t, err)
	onByKind := make(map[string]parser.NodeKind, len(symsOn))
	for _, s := range symsOn {
		onByKind[s.Name] = s.Kind
	}
	assert.Equal(t, parser.KindMacro, onByKind["dbg"],
		"flag ON: macro_rules! dbg must be indexed as macro")
	assert.Equal(t, parser.KindModule, onByKind["net"],
		"flag ON: mod net must be indexed as module")
	assert.Equal(t, parser.KindTypeAlias, onByKind["Id"],
		"flag ON: type Id must be indexed as type_alias")
	assert.Equal(t, parser.KindFunction, onByKind["init"],
		"flag ON: fn init must still be indexed as function")
}

// TestBuildSymbolEntriesForFile_ExpandedKinds verifies the cache path
// (buildSymbolEntriesForFile) agrees with the bulk path when the flag is ON.
// A divergence causes churn (embed on one path, orphan-delete on the next).
//
// Falsification: revert buildSymbolEntriesForFile to use IsEmbeddableKind
// instead of IsEmbeddableKindExpanded → the macro/module entries go Red.
func TestBuildSymbolEntriesForFile_ExpandedKinds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relPath := "macros.rs"
	content := `pub macro_rules! say { () => {} }

pub mod ops {
    pub fn add() {}
}

pub type Num = i32;
`
	writeTestFile(t, dir, relPath, content)
	absPath := filepath.Join(dir, relPath)

	p := &Pipeline{expandSymbolKinds: true}
	f := &ingest.File{Path: absPath, RelPath: relPath, Language: "rust"}

	entries, err := p.buildSymbolEntriesForFile(f)
	require.NoError(t, err)

	got := make(map[string]parser.NodeKind, len(entries))
	for _, e := range entries {
		got[e.sym.Name] = e.sym.Kind
	}
	assert.Equal(t, parser.KindMacro, got["say"], "cache path: macro must be indexed when flag ON")
	assert.Equal(t, parser.KindModule, got["ops"], "cache path: module must be indexed when flag ON")
	assert.Equal(t, parser.KindTypeAlias, got["Num"], "cache path: type alias must be indexed when flag ON")
	assert.Equal(t, parser.KindFunction, got["add"], "cache path: function must still be indexed")
}

// TestBuildSymbolEntriesForFile_NonRegressionFlagOff verifies the cache path
// with flag OFF produces the same symbol set as the pre-change behavior.
//
// Falsification: any change to the flag-OFF path → the equality assertion
// against the legacy collectSymbols goes Red.
func TestBuildSymbolEntriesForFile_NonRegressionFlagOff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relPath := "types.rs"
	content := `pub macro_rules! skip { () => {} }

pub mod defer {
    pub fn inner() {}
}

pub type Handle = u64;

pub fn work() {}
`
	writeTestFile(t, dir, relPath, content)
	absPath := filepath.Join(dir, relPath)

	// Flag OFF pipeline — zero-value expandSymbolKinds=false.
	p := &Pipeline{}
	f := &ingest.File{Path: absPath, RelPath: relPath, Language: "rust"}

	entries, err := p.buildSymbolEntriesForFile(f)
	require.NoError(t, err)

	gotNames := make(map[string]bool, len(entries))
	for _, e := range entries {
		gotNames[e.sym.Name] = true
	}
	// Macro/module NOT in embed set when flag OFF.
	assert.False(t, gotNames["skip"], "flag OFF: macro must NOT be in cache-path embed set")
	assert.False(t, gotNames["defer"], "flag OFF: module must NOT be in cache-path embed set")
	// Existing symbols present.
	assert.True(t, gotNames["work"], "flag OFF: function must be in embed set")
	assert.True(t, gotNames["Handle"], "flag OFF: type (alias as KindType) must be in embed set")
}

// --- helpers ---

func sortedSymbolNames(syms []*parser.Symbol) []string {
	out := make([]string, 0, len(syms))
	for _, s := range syms {
		out = append(out, s.Name)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
