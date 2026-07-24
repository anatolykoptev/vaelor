package embeddings

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectSymbols_IncludesTypeSymbols verifies the bulk path
// (collectSymbols) now emits type-level symbols — class, interface, trait,
// struct, enum/type — in addition to functions/methods. Before #655 the filter
// kept only KindFunction/KindMethod and dropped every type.
//
// Falsification: revert the predicate in collectSymbols to
// `sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod` → the
// type-symbol assertions below go RED (len(syms) drops, each type-kind
// subtest fails).
func TestCollectSymbols_IncludesTypeSymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Rust: trait → interface, struct → struct, enum → type, fn → function.
	writeTestFile(t, dir, "lib.rs", `pub trait Deserialize<'de> {
    fn deserialize() -> Self;
}

pub struct Config {
    pub name: String,
}

pub enum Mode {
    Read,
    Write,
}

pub fn parse() -> Config {
    Config { name: String::new() }
}
`)
	// TypeScript: interface + class + function.
	writeTestFile(t, dir, "model.ts", `export interface User {
  id: number;
}

export class UserService {
  getUser(): User { return { id: 1 }; }
}

export function makeUser(): User { return { id: 0 }; }
`)
	// Java: class + method (a class WITHOUT a same-name constructor — the
	// class+constructor name collision is documented separately in
	// TestCollectSymbols_SameNameCollisionDocumented).
	writeTestFile(t, dir, "Parser.java", `public class Parser {
  public String parse() { return ""; }
}
`)
	// Python: class + function.
	writeTestFile(t, dir, "app.py", `class App:
    def run(self):
        pass

def main():
    pass
`)

	syms, _, err := collectSymbols(context.Background(), dir, false)
	require.NoError(t, err)

	byName := make(map[string]parser.NodeKind, len(syms))
	for _, s := range syms {
		byName[s.Name] = s.Kind
	}

	// Rust kinds.
	assert.Equal(t, parser.KindInterface, byName["Deserialize"],
		"Rust trait must be indexed as interface")
	assert.Equal(t, parser.KindStruct, byName["Config"],
		"Rust struct must be indexed as struct")
	assert.Equal(t, parser.KindType, byName["Mode"],
		"Rust enum must be indexed as type")
	assert.Equal(t, parser.KindFunction, byName["parse"],
		"Rust fn must still be indexed as function")

	// TypeScript kinds.
	assert.Equal(t, parser.KindInterface, byName["User"],
		"TS interface must be indexed as interface")
	assert.Equal(t, parser.KindClass, byName["UserService"],
		"TS class must be indexed as class")

	// Java class.
	assert.Equal(t, parser.KindClass, byName["Parser"],
		"Java class must be indexed as class")
	// `parse` is a method inside the class; tree-sitter's java grammar may
	// label it as function or method depending on capture — the load-bearing
	// assertion is that the CLASS is indexed (the #655 fix), not the method's
	// exact kind label.
	assert.Contains(t, []parser.NodeKind{parser.KindMethod, parser.KindFunction}, byName["parse"],
		"Java parse must still be indexed (as method or function)")

	// Python class.
	assert.Equal(t, parser.KindClass, byName["App"],
		"Python class must be indexed as class")
	assert.Equal(t, parser.KindFunction, byName["main"],
		"Python function must still be indexed as function")
}

// TestBuildSymbolEntriesForFile_IncludesTypeSymbols verifies the cache path
// (buildSymbolEntriesForFile) agrees with the bulk path on the indexed set.
// A divergence between the two paths causes churn: the cache path embeds a
// symbol, the bulk path (or vice versa) orphan-deletes it on the next pass.
//
// Falsification: revert the predicate in buildSymbolEntriesForFile to
// func/method-only → the type-symbol assertions go RED.
func TestBuildSymbolEntriesForFile_IncludesTypeSymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relPath := "types.go"
	content := `package types

type Shape interface {
	Area() float64
}

type Rect struct {
	W, H float64
}

func (r Rect) Area() float64 { return r.W * r.H }

func NewRect(w, h float64) Rect { return Rect{W: w, H: h} }
`
	writeTestFile(t, dir, relPath, content)
	absPath := filepath.Join(dir, relPath)

	// buildSymbolEntriesForFile does not touch p.store or p.client — a bare
	// Pipeline is safe here.
	p := &Pipeline{}
	f := &ingest.File{Path: absPath, RelPath: relPath, Language: "go"}

	entries, err := p.buildSymbolEntriesForFile(f)
	require.NoError(t, err)

	got := make(map[string]parser.NodeKind, len(entries))
	for _, e := range entries {
		got[e.sym.Name] = e.sym.Kind
	}
	assert.Equal(t, parser.KindInterface, got["Shape"], "Go interface must be indexed")
	assert.Equal(t, parser.KindStruct, got["Rect"], "Go struct must be indexed")
	assert.Equal(t, parser.KindMethod, got["Area"], "Go method must still be indexed")
	assert.Equal(t, parser.KindFunction, got["NewRect"], "Go function must still be indexed")

	// buildEmbedText must produce a sensible embed string for a type symbol:
	// it carries Kind/Name/Signature (set by mapType/mapInterface) + body.
	for _, e := range entries {
		if e.sym.Kind == parser.KindInterface || e.sym.Kind == parser.KindStruct {
			assert.Contains(t, e.embedText, relPath, "embed text must include file path")
			assert.Contains(t, e.embedText, string(e.sym.Kind), "embed text must include kind")
			assert.Contains(t, e.embedText, e.sym.Name, "embed text must include name")
			assert.NotEmpty(t, e.embedText, "embed text must be non-empty for type symbol")
		}
	}
}

// TestBuildSymbolEntriesForFile_NoChurnOnReindex verifies the no-churn
// guarantee for type symbols: building entries for an unchanged file twice
// produces byte-identical embedText + hash for every type symbol, so the
// incremental diff (parseAndDiff) classifies them as Skipped, not re-embedded
// or orphan-deleted. This is the hash-skip foundation; the full Skipped
// classification is exercised against a live store by TestIndexFile_ReindexUnchanged.
//
// Falsification: if the predicate or buildEmbedText became non-deterministic
// for type symbols, the two passes would diverge and this test goes RED.
func TestBuildSymbolEntriesForFile_NoChurnOnReindex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relPath := "traits.rs"
	content := `pub trait Deserialize<'de> {
    fn deserialize() -> Self;
}

pub struct Config {
    pub name: String,
}

pub enum Mode {
    Read,
    Write,
}
`
	writeTestFile(t, dir, relPath, content)
	absPath := filepath.Join(dir, relPath)
	p := &Pipeline{}
	f := &ingest.File{Path: absPath, RelPath: relPath, Language: "rust"}

	first, err := p.buildSymbolEntriesForFile(f)
	require.NoError(t, err)
	require.NotEmpty(t, first, "precondition: at least one type symbol indexed")

	second, err := p.buildSymbolEntriesForFile(f)
	require.NoError(t, err)

	// Index by name for stable comparison.
	firstBy := make(map[string]symbolEntry, len(first))
	for _, e := range first {
		firstBy[e.sym.Name] = e
	}
	// Confirm type symbols are present (the whole point of #655).
	typeKinds := map[string]bool{}
	for _, e := range first {
		if e.sym.Kind != parser.KindFunction && e.sym.Kind != parser.KindMethod {
			typeKinds[e.sym.Name] = true
		}
	}
	require.NotEmpty(t, typeKinds, "precondition: type symbols must be indexed (predicate widened)")

	for _, e := range second {
		got, ok := firstBy[e.sym.Name]
		require.True(t, ok, "second pass symbol %q missing from first pass", e.sym.Name)
		assert.Equal(t, got.hash, e.hash,
			"hash for %q must be stable across re-index (no churn)", e.sym.Name)
		assert.Equal(t, got.embedText, e.embedText,
			"embedText for %q must be stable across re-index (no churn)", e.sym.Name)
		assert.Equal(t, got.sym.Kind, e.sym.Kind,
			"kind for %q must be stable across re-index", e.sym.Name)
	}
	// Same symbol set (by name) both passes.
	firstNames := namesOf(first)
	secondNames := namesOf(second)
	sort.Strings(firstNames)
	sort.Strings(secondNames)
	assert.Equal(t, firstNames, secondNames, "symbol set must be identical across re-index")
}

// TestCollectSymbols_ExcludesConstVarModule verifies the excluded kinds do NOT
// enter the embed set — they are high-volume, low-retrieval-value, and indexing
// them would balloon the table. This guards against an over-wide predicate.
//
// Falsification: add KindConst/KindVar/KindModule to IsEmbeddableKind → the
// assertions below go RED.
func TestCollectSymbols_ExcludesConstVarModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "consts.go", `package consts

const Version = "1.0"

var DefaultName = "example"

type Config struct {
	Val int
}

func Init() {}
`)
	syms, _, err := collectSymbols(context.Background(), dir, false)
	require.NoError(t, err)

	for _, s := range syms {
		if s.Name == "Version" {
			t.Errorf("const Version must NOT be indexed (kind=%s)", s.Kind)
		}
		if s.Name == "DefaultName" {
			t.Errorf("var DefaultName must NOT be indexed (kind=%s)", s.Kind)
		}
	}
	// Sanity: the type and function in the same file ARE indexed.
	byName := make(map[string]bool, len(syms))
	for _, s := range syms {
		byName[s.Name] = true
	}
	assert.True(t, byName["Config"], "struct Config must be indexed")
	assert.True(t, byName["Init"], "function Init must be indexed")
}

func namesOf(entries []symbolEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.sym.Name)
	}
	return out
}

// TestBuildEmbedText_TypeSymbolSample prints a sample embed string for a type
// symbol so the report can show buildEmbedText produces a sensible vector
// input for class/interface/struct (it uses Kind/Name/Signature/DocComment/body
// — Signature is set by mapType/mapInterface/mapClass).
func TestBuildEmbedText_TypeSymbolSample(t *testing.T) {
	t.Parallel()
	sym := &parser.Symbol{
		Language:   "rust",
		Kind:       parser.KindInterface,
		Name:       "Deserialize",
		Signature:  "pub trait Deserialize<'de>",
		DocComment: "Deserialize is the core deserialization trait.\nImplemented by every type that can be decoded from input.",
		Body:       "pub trait Deserialize<'de> {\n    fn deserialize() -> Self;\n}",
	}
	text := buildEmbedText(sym, "src/de/mod.rs")
	t.Logf("type-symbol embed text sample:\n%s", text)
	assert.Contains(t, text, "src/de/mod.rs")
	assert.Contains(t, text, "interface")
	assert.Contains(t, text, "Deserialize")
	assert.Contains(t, text, "pub trait Deserialize<'de>")
	assert.Contains(t, text, "deserialization trait")
}

// TestCollectSymbols_SameNameCollisionDocumented documents the pre-existing
// name-only PK limitation that #655's widening makes more likely to surface:
// when a file has two embeddable symbols sharing a name (e.g. a Java class
// `Foo` and its constructor method `Foo()`), collectSymbols emits BOTH into
// its slice, but the downstream filterSymbols / parseAndDiff / DB upsert all
// key by (file_path, symbol_name) and collapse the pair to one row.
//
// This is NOT introduced by #655 — filterSymbols already documents it for
// build-tag variants and C++ overloads (see its doc comment). The fix for it
// is a DB PK migration to include kind/start_line, which is out of scope for
// the type-symbol restoration. This test pins the current behavior so a future
// PK migration can flip the assertion.
//
// Not a falsification target for #655: reverting the predicate does not change
// the collision behavior (it changes which kinds are eligible, not the keying).
func TestCollectSymbols_SameNameCollisionDocumented(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "Foo.java", `public class Foo {
  public Foo() {}
  public String greet() { return ""; }
}
`)
	syms, _, err := collectSymbols(context.Background(), dir, false)
	require.NoError(t, err)

	// Both the class and the constructor are parsed and emitted by collectSymbols.
	var classFoo, methodFoo, methodGreet bool
	for _, s := range syms {
		switch {
		case s.Name == "Foo" && s.Kind == parser.KindClass:
			classFoo = true
		case s.Name == "Foo" && s.Kind == parser.KindMethod:
			methodFoo = true
		case s.Name == "greet" && s.Kind == parser.KindMethod:
			methodGreet = true
		}
	}
	assert.True(t, classFoo, "Java class Foo must be parsed and emitted by collectSymbols")
	assert.True(t, methodFoo, "Java constructor Foo() must be parsed and emitted by collectSymbols")
	assert.True(t, methodGreet, "Java method greet must be parsed and emitted")

	// Document the downstream collapse: a name-keyed map (the shape used by
	// parseAndDiff's `current` and the DB PK) keeps only ONE of the same-name
	// pair — exactly one kind survives for the name "Foo". This is the
	// limitation a future PK migration to (repo_key, file_path, symbol_name,
	// start_line) would resolve.
	byName := make(map[string]parser.NodeKind, len(syms))
	for _, s := range syms {
		byName[s.Name] = s.Kind
	}
	gotKind, hasFoo := byName["Foo"]
	assert.True(t, hasFoo, "Foo must be present in a name-keyed view")
	assert.True(t, gotKind == parser.KindClass || gotKind == parser.KindMethod,
		"name-keyed view collapses same-name pair to one kind (got %s); pre-existing PK limitation",
		gotKind)
}
