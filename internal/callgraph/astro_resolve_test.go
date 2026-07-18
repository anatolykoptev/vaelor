package callgraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// root used across all tests.
const testRoot = "/repo"

// ref constructs a TemplateRef for convenience.
func ref(name string, line uint32) preproc.TemplateRef {
	return preproc.TemplateRef{Name: name, Line: line}
}

// TestResolveTemplateRefs_RelativeImport checks that a simple default import
// resolves a <X /> tag to the correct relative path.
func TestResolveTemplateRefs_RelativeImport(t *testing.T) {
	src := []byte("---\nimport X from './c/X.astro'\n---\n<X />")
	refs := []preproc.TemplateRef{ref("X", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", testRoot)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	if usages[0].To != "src/c/X.astro" {
		t.Errorf("expected To=src/c/X.astro, got %q", usages[0].To)
	}
	if usages[0].From != "src/Home.astro" {
		t.Errorf("expected From=src/Home.astro, got %q", usages[0].From)
	}
}

// TestResolveTemplateRefs_PathTraversal checks that ../ in the import path is
// resolved correctly.
func TestResolveTemplateRefs_PathTraversal(t *testing.T) {
	src := []byte("---\nimport X from '../shared/X.astro'\n---\n<X />")
	refs := []preproc.TemplateRef{ref("X", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/pages/Home.astro", testRoot)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	if usages[0].To != "src/shared/X.astro" {
		t.Errorf("expected To=src/shared/X.astro, got %q", usages[0].To)
	}
}

// TestResolveTemplateRefs_TagNotInImports verifies that a tag with no matching
// import binding produces no edge.
func TestResolveTemplateRefs_TagNotInImports(t *testing.T) {
	src := []byte("---\nimport Known from './Known.astro'\n---\n<NotImported />")
	refs := []preproc.TemplateRef{ref("NotImported", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", testRoot)
	if len(usages) != 0 {
		t.Errorf("expected 0 usages, got %d", len(usages))
	}
}

// TestResolveTemplateRefs_NamedImports checks that named imports bind both
// names individually. We use two separate import paths so deduplication does
// not collapse the two file-level edges.
func TestResolveTemplateRefs_NamedImports(t *testing.T) {
	src := []byte("---\nimport { A } from './a.astro'\nimport { B } from './b.astro'\n---\n<A /><B />")
	refs := []preproc.TemplateRef{ref("A", 5), ref("B", 5)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", testRoot)
	if len(usages) != 2 {
		t.Fatalf("expected 2 usages, got %d", len(usages))
	}
	targets := map[string]bool{}
	for _, u := range usages {
		targets[u.To] = true
	}
	if !targets["src/a.astro"] || !targets["src/b.astro"] {
		t.Errorf("expected both src/a.astro and src/b.astro in usages, got %v", targets)
	}
}

// TestResolveTemplateRefs_MultiLineImport is a regression test for Issue 1:
// multi-line import statements were silently dropped. Both A and B come from
// the same path; we expect exactly 1 resolved edge (dedup is correct) but
// confirm the edge actually exists — proving the multi-line statement was parsed.
func TestResolveTemplateRefs_MultiLineImport(t *testing.T) {
	src := []byte("---\nimport {\n  A,\n  B,\n} from './lib.astro'\n---\n<A /><B />")
	refs := []preproc.TemplateRef{ref("A", 7), ref("B", 7)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", testRoot)
	// After dedup, A and B resolve to the same file so we get 1 edge.
	// The key assertion is that it's > 0: previously it was 0 (silent drop).
	if len(usages) == 0 {
		t.Fatal("expected at least 1 usage for multi-line import, got 0 (Issue 1 regression)")
	}
	if usages[0].To != "src/lib.astro" {
		t.Errorf("expected To=src/lib.astro, got %q", usages[0].To)
	}
}

// TestResolveTemplateRefs_DefaultPlusNamed is a regression test for Issue 2:
// default + named combo lost the named bindings. C and D come from the same
// path, so dedup yields 1 edge; the key assertion is that both C and D are
// bound (i.e. at least 1 edge exists — previously D was silently dropped and
// only C was bound, but C's dedup with a hypothetical D was a separate problem).
// To verify D specifically, we also test with a ref for D only.
func TestResolveTemplateRefs_DefaultPlusNamed(t *testing.T) {
	src := []byte("---\nimport C, { D } from './x.astro'\n---\n<D />")
	// Only ref D — C is not referenced. If D's binding exists, 1 usage is produced.
	refs := []preproc.TemplateRef{ref("D", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", testRoot)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage for named binding D in default+named import, got %d (Issue 2 regression)", len(usages))
	}
	if usages[0].To != "src/x.astro" {
		t.Errorf("expected To=src/x.astro, got %q", usages[0].To)
	}
}

// TestResolveTemplateRefs_BareSpecifier verifies that bare (non-relative)
// specifiers like 'svelte' are silently ignored.
func TestResolveTemplateRefs_BareSpecifier(t *testing.T) {
	src := []byte("---\nimport S from 'svelte'\n---\n<S />")
	refs := []preproc.TemplateRef{ref("S", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", testRoot)
	if len(usages) != 0 {
		t.Errorf("expected 0 usages for bare specifier, got %d", len(usages))
	}
}

// TestResolveTemplateRefs_ScopedWorkspacePkg_NoCounter verifies that a scoped
// npm/turborepo workspace package (e.g. @guide/core) that is NOT declared in
// tsconfig paths does not increment the unresolved-alias counter.
// Regression: old gate used strings.Contains(importPath, "/") which fired on
// any scoped package, making the counter useless on monorepos with @scope/* deps.
func TestResolveTemplateRefs_ScopedWorkspacePkg_NoCounter(t *testing.T) {
	before := testutil.ToFloat64(parserUnresolvedAliasTotal)

	src := []byte("---\nimport Core from '@guide/core'\n---\n<Core />")
	refs := []preproc.TemplateRef{ref("Core", 4)}
	// Use a real temp root with no tsconfig → alias map is empty.
	root := t.TempDir()
	aliasCache.Delete(root) // ensure fresh load
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", root)

	after := testutil.ToFloat64(parserUnresolvedAliasTotal)
	if len(usages) != 0 {
		t.Errorf("expected 0 usages for scoped npm package, got %d", len(usages))
	}
	if after != before {
		t.Errorf("counter must NOT increment for scoped npm package (@guide/core not in paths), delta=%.0f", after-before)
	}
}

// TestResolveTemplateRefs_BrokenAlias_CounterIncrements verifies that when a
// tsconfig paths alias is declared (e.g. "@/*" → "src/*") but the resolved file
// does not exist on disk, the unresolved-alias counter increments by 1.
func TestResolveTemplateRefs_BrokenAlias_CounterIncrements(t *testing.T) {
	root := t.TempDir()
	tsconfigJSON := `{"compilerOptions":{"paths":{"@/*":["src/*"]}}}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	aliasCache.Delete(root) // force re-read of new tsconfig

	before := testutil.ToFloat64(parserUnresolvedAliasTotal)
	src := []byte("---\nimport M from '@/components/Missing.astro'\n---\n<M />")
	refs := []preproc.TemplateRef{ref("M", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", root)
	after := testutil.ToFloat64(parserUnresolvedAliasTotal)

	if len(usages) != 0 {
		t.Errorf("expected 0 usages for broken alias (file absent), got %d", len(usages))
	}
	if after-before != 1 {
		t.Errorf("counter must increment by 1 for broken declared alias, delta=%.0f", after-before)
	}
}

// TestResolveTemplateRefs_ValidAlias_NoCounter verifies that when a tsconfig
// paths alias resolves to a file that EXISTS on disk, no counter is incremented
// and the usage edge is produced. This is the regression/happy-path guard.
func TestResolveTemplateRefs_ValidAlias_NoCounter(t *testing.T) {
	root := t.TempDir()
	tsconfigJSON := `{"compilerOptions":{"paths":{"@/*":["src/*"]}}}`
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create the actual component file so disk-existence check passes.
	if err := os.MkdirAll(filepath.Join(root, "src/components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/components/Foo.astro"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	aliasCache.Delete(root) // force re-read of new tsconfig

	before := testutil.ToFloat64(parserUnresolvedAliasTotal)
	src := []byte("---\nimport Foo from '@/components/Foo.astro'\n---\n<Foo />")
	refs := []preproc.TemplateRef{ref("Foo", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.astro", root)
	after := testutil.ToFloat64(parserUnresolvedAliasTotal)

	if len(usages) != 1 {
		t.Fatalf("expected 1 usage for valid alias, got %d", len(usages))
	}
	if usages[0].To != "src/components/Foo.astro" {
		t.Errorf("expected To=src/components/Foo.astro, got %q", usages[0].To)
	}
	if after != before {
		t.Errorf("counter must NOT increment for valid alias, delta=%.0f", after-before)
	}
}
