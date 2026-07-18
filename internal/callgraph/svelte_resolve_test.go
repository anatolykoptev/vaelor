package callgraph

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

// TestResolveTemplateRefs_SvelteScriptImport mirrors the Astro relative-import
// composition test (TestResolveTemplateRefs_RelativeImport) for a .svelte file:
// an import declared in a <script> block resolves a <Card/> tag in the markup to
// its component file, producing exactly one file-level USES edge.
//
// This is the Phase-2 Svelte-composition gate: a .svelte with a component ref
// must produce the same composition edge an equivalent .astro does.
func TestResolveTemplateRefs_SvelteScriptImport(t *testing.T) {
	src := []byte("<script>\n  import Card from './c/Card.svelte';\n</script>\n<Card />")
	refs := []preproc.TemplateRef{ref("Card", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.svelte", testRoot)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	if usages[0].To != "src/c/Card.svelte" {
		t.Errorf("expected To=src/c/Card.svelte, got %q", usages[0].To)
	}
	if usages[0].From != "src/Home.svelte" {
		t.Errorf("expected From=src/Home.svelte, got %q", usages[0].From)
	}
}

// TestResolveTemplateRefs_SvelteLangTsImport verifies a <script lang="ts"> block's
// imports are scanned too (ExtractSvelte handles lang="ts").
func TestResolveTemplateRefs_SvelteLangTsImport(t *testing.T) {
	src := []byte("<script lang=\"ts\">\n  import Panel from './ui/Panel.svelte';\n</script>\n<Panel />")
	refs := []preproc.TemplateRef{ref("Panel", 4)}
	usages := ResolveTemplateRefs(src, refs, "src/Home.svelte", testRoot)
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	if usages[0].To != "src/ui/Panel.svelte" {
		t.Errorf("expected To=src/ui/Panel.svelte, got %q", usages[0].To)
	}
}
