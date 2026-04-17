package callgraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser/preproc"
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
