package preproc

import (
	"slices"
	"testing"
)

// TestExtractSvelteWithRefs_ComponentTag verifies ExtractSvelteWithRefs captures
// capitalised component tags from the markup while skipping lowercase HTML tags,
// <svelte:*> special elements, and <script>/<style> contents.
func TestExtractSvelteWithRefs_ComponentTag(t *testing.T) {
	t.Parallel()
	src := []byte("<script>\n" +
		"  import Card from './Card.svelte';\n" +
		"  const Local = 1; // not a tag\n" +
		"</script>\n" +
		"<div>\n" +
		"  <Card title=\"x\" />\n" +
		"  <Sidebar>\n" +
		"    <p>text</p>\n" +
		"  </Sidebar>\n" +
		"  <svelte:head><title>t</title></svelte:head>\n" +
		"</div>\n" +
		"<style>\n  .x { color: Red; }\n</style>\n")

	vs, refs := ExtractSvelteWithRefs(src)
	if vs == nil || vs.Lang != "svelte" {
		t.Fatalf("VirtualSource lang = %v, want svelte", vs)
	}

	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	for _, want := range []string{"Card", "Sidebar"} {
		if !slices.Contains(names, want) {
			t.Errorf("expected component ref %q, got %v", want, names)
		}
	}
	// Lowercase HTML tags, <svelte:*> specials, and script/style contents must not appear.
	for _, bad := range names {
		switch bad {
		case "div", "p", "title", "svelte", "svelte:head":
			t.Errorf("unexpected non-component ref %q in %v", bad, names)
		}
	}
}

// TestExtractSvelteWithRefs_NoComponents verifies a component with only HTML tags
// produces no refs.
func TestExtractSvelteWithRefs_NoComponents(t *testing.T) {
	t.Parallel()
	src := []byte("<script>\n  let x = 1;\n</script>\n<div><span>{x}</span></div>\n")
	_, refs := ExtractSvelteWithRefs(src)
	if len(refs) != 0 {
		names := make([]string, len(refs))
		for i, r := range refs {
			names[i] = r.Name
		}
		t.Errorf("expected no refs, got %v", names)
	}
}
