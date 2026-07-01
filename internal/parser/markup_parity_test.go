package parser_test

import (
	"sort"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// Capability-matrix equivalence harness (frontend-parse-parity Phase 3).
//
// A structured, reusable table of {capability-row × framework} with parallel
// Astro/Svelte fixtures and per-cell assertions, proving Svelte's template-expr
// and effective-control-flow rows EQUAL Astro's. go-code models NO
// control-flow-structure edges, so "control-flow parity" is the refs/calls INSIDE
// the construct captured as ordinary call/ref edges — each row asserts the SAME
// surfaced edges from each framework's own syntax (Astro `.map`/`&&`, Svelte
// `{#each}`/`{#if}`).
//
// Phase 5 promotes this into a blocking regression gate with minimal change: the
// table already carries the per-row expected-capability set, so flipping it to a
// hard gate is a matter of policy, not restructuring.

// surfaced parses src via the language handler (extension-routed) and buckets the
// resulting call sites into call names and argref names.
func surfaced(t *testing.T, path, src string) (calls, refs map[string]bool) {
	t.Helper()
	cs, err := parser.ExtractCalls(path, []byte(src), parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls(%s): %v", path, err)
	}
	calls, refs = map[string]bool{}, map[string]bool{}
	for _, c := range cs {
		if c.IsArgRef {
			refs[c.Name] = true
		} else {
			calls[c.Name] = true
		}
	}
	return calls, refs
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// capRow is a single capability proven EQUAL across Astro and Svelte. astroSrc /
// svelteSrc express the same capability in each framework's own syntax; wantCalls
// / wantRefs are the surfaced edges that MUST appear in BOTH (the shared capability
// — framework-specific extras such as Astro's `.map` receiver or Svelte's each
// source are asserted separately in TestSvelteBlockHeaderCalls).
type capRow struct {
	name      string
	astroSrc  string
	svelteSrc string
	wantCalls []string
	wantRefs  []string
}

var capabilityMatrix = []capRow{
	{
		name:      "plain-call",
		astroSrc:  "---\n---\n<p>{format(count)}</p>\n",
		svelteSrc: "<p>{format(count)}</p>\n",
		wantCalls: []string{"format"},
		wantRefs:  []string{"count"},
	},
	{
		name:      "bare-ref",
		astroSrc:  "---\n---\n<p>{count}</p>\n",
		svelteSrc: "<p>{count}</p>\n",
		wantRefs:  []string{"count"},
	},
	{
		name:      "member-call",
		astroSrc:  "---\n---\n<p>{user.greet()}</p>\n",
		svelteSrc: "<p>{user.greet()}</p>\n",
		wantCalls: []string{"greet"},
	},
	{
		// Effective control flow — a call + its arg ref inside a loop body. Astro
		// expresses it with .map + JSX; Svelte with an {#each} block. Same edges.
		name:      "control-flow-effective",
		astroSrc:  "---\n---\n<div>{items.map(i => <Card x={fmt(i)}/>)}</div>\n",
		svelteSrc: "<div>{#each items as i}<Card x={fmt(i)}/>{/each}</div>\n",
		wantCalls: []string{"fmt"},
		wantRefs:  []string{"i"},
	},
	{
		// Effective conditional — a call + its arg ref inside a conditional body.
		// Astro `cond && expr`; Svelte `{#if cond}…{/if}`. Same body edges.
		name:      "conditional-effective",
		astroSrc:  "---\n---\n<div>{ready && render(node)}</div>\n",
		svelteSrc: "<div>{#if ready}{render(node)}{/if}</div>\n",
		wantCalls: []string{"render"},
		wantRefs:  []string{"node"},
	},
}

func rowPasses(calls, refs map[string]bool, r capRow) bool {
	for _, n := range r.wantCalls {
		if !calls[n] {
			return false
		}
	}
	for _, n := range r.wantRefs {
		if !refs[n] {
			return false
		}
	}
	return true
}

// TestMarkupCapabilityMatrix asserts each capability row's shared expected edges
// surface for BOTH Astro and Svelte.
func TestMarkupCapabilityMatrix(t *testing.T) {
	for _, row := range capabilityMatrix {
		t.Run(row.name, func(t *testing.T) {
			aCalls, aRefs := surfaced(t, "cap.astro", row.astroSrc)
			sCalls, sRefs := surfaced(t, "cap.svelte", row.svelteSrc)

			for _, n := range row.wantCalls {
				if !aCalls[n] {
					t.Errorf("astro missing call %q; calls=%v refs=%v", n, keysOf(aCalls), keysOf(aRefs))
				}
				if !sCalls[n] {
					t.Errorf("svelte missing call %q; calls=%v refs=%v", n, keysOf(sCalls), keysOf(sRefs))
				}
			}
			for _, n := range row.wantRefs {
				if !aRefs[n] {
					t.Errorf("astro missing ref %q; calls=%v refs=%v", n, keysOf(aCalls), keysOf(aRefs))
				}
				if !sRefs[n] {
					t.Errorf("svelte missing ref %q; calls=%v refs=%v", n, keysOf(sCalls), keysOf(sRefs))
				}
			}
		})
	}
}

// TestSvelteEqualsAstroCapabilities is the literal "rows EQUAL" gate: for every
// capability row, the Svelte cell must pass IFF the Astro cell passes.
func TestSvelteEqualsAstroCapabilities(t *testing.T) {
	for _, row := range capabilityMatrix {
		aCalls, aRefs := surfaced(t, "cap.astro", row.astroSrc)
		sCalls, sRefs := surfaced(t, "cap.svelte", row.svelteSrc)
		astroPass := rowPasses(aCalls, aRefs, row)
		sveltePass := rowPasses(sCalls, sRefs, row)
		if astroPass != sveltePass {
			t.Errorf("row %q: capability parity broken — astroPass=%v sveltePass=%v", row.name, astroPass, sveltePass)
		}
		if !astroPass {
			t.Errorf("row %q: astro cell does not satisfy its own capability (fixture/expectation bug)", row.name)
		}
	}
}

// TestSvelteBlockHeaderCalls is the RED->GREEN gate for the sigil-aware
// block-header extraction: every Svelte control-flow / special tag surfaces the
// calls and refs carried by its HEADER EXPR as ordinary edges, exactly like a
// plain mustache. Without svelteHandler.MarkupCalls (the Phase-3 wiring) none of
// these surface.
func TestSvelteBlockHeaderCalls(t *testing.T) {
	src := "" +
		"{#if canView(user)}\n" + // canView call, user argref
		"  {#each fetchRows() as row}\n" + // fetchRows call
		"    <Cell v={render(row)}/>\n" + // render call, row argref
		"  {/each}\n" +
		"{:else if fallback}\n" + // fallback argref
		"{/if}\n" +
		"{#await load(id) then data}\n" + // load call, id argref
		"{/await}\n" +
		"{#key sel.id}<X/>{/key}\n" +
		"{@const n = total(items)}\n" + // total call, items argref
		"{@html sanitize(raw)}\n" + // sanitize call, raw argref
		"{@render tpl(row2)}\n" // tpl call, row2 argref

	calls, refs := surfaced(t, "blocks.svelte", src)

	for _, n := range []string{"canView", "fetchRows", "render", "load", "total", "sanitize", "tpl"} {
		if !calls[n] {
			t.Errorf("missing block-header call %q; calls=%v", n, keysOf(calls))
		}
	}
	for _, n := range []string{"user", "row", "fallback", "id", "items", "raw", "row2"} {
		if !refs[n] {
			t.Errorf("missing block-header ref %q; refs=%v", n, keysOf(refs))
		}
	}
}
