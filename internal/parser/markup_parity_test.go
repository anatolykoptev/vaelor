package parser_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// Capability-matrix equivalence harness (frontend-parse-parity Phase 3,
// promoted to a named BLOCKING regression gate in Phase 5 — see
// docs/adr/0001-frontend-parse-parity.md).
//
// THIS IS THE parity regression gate: every assertion in this file is a hard
// t.Errorf/t.Fatalf, never a t.Logf — a per-row Svelte/Astro parity drop, an
// expr-delimiting-accuracy regression (the sibling floor test,
// internal/parser/preproc/svelte_exprs_test.go:TestSvelteExprDelimitingAccuracy),
// or a resurfaced duplicate-edge FAILS `go test ./internal/parser/...`, which
// `make preflight` runs on every PR (.github/workflows/preflight.yml) and
// `preflight` is a required merge check on this public repo (CLAUDE.md ## CI).
// There was no separate "wire it up" step to perform in Phase 5 — the harness
// has asserted hard since Phase 3 landed (commit c4302db, PR #271); Phase 5's
// job is to name this file as the parity contract and hold the line: a drop
// here blocks merge, and no assertion in this file may be weakened to make a
// future regression pass.
//
// A structured, reusable table of {capability-row × framework} with parallel
// Astro/Svelte fixtures and per-cell assertions, proving Svelte's template-expr
// and effective-control-flow rows EQUAL Astro's. go-code models NO
// control-flow-structure edges, so "control-flow parity" is the refs/calls INSIDE
// the construct captured as ordinary call/ref edges — each row asserts the SAME
// surfaced edges from each framework's own syntax (Astro `.map`/`&&`, Svelte
// `{#each}`/`{#if}`).
//
// The harness counts multiplicity (map[string]int), not mere presence, so it can
// catch the duplicate-edge class (a template call emitted by two producers) — see
// TestNoDuplicateMarkupEdges.

// callSitesFor parses src via the language handler (extension-routed) and returns
// the raw call sites.
func callSitesFor(t *testing.T, path, src string) []parser.CallSite {
	t.Helper()
	cs, err := parser.ExtractCalls(path, []byte(src), parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls(%s): %v", path, err)
	}
	return cs
}

// surfaced buckets the call sites into call-name counts and argref-name counts.
func surfaced(t *testing.T, path, src string) (calls, refs map[string]int) {
	t.Helper()
	calls, refs = map[string]int{}, map[string]int{}
	for _, c := range callSitesFor(t, path, src) {
		if c.IsArgRef {
			refs[c.Name]++
		} else {
			calls[c.Name]++
		}
	}
	return calls, refs
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dumpCS(cs []parser.CallSite) string {
	out := "["
	for i, c := range cs {
		if i > 0 {
			out += ", "
		}
		kind := "call"
		if c.IsArgRef {
			kind = "ref"
		}
		out += fmt.Sprintf("%s(recv=%q)@%d:%s", c.Name, c.Receiver, c.Line, kind)
	}
	return out + "]"
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

func rowPasses(calls, refs map[string]int, r capRow) bool {
	for _, n := range r.wantCalls {
		if calls[n] == 0 {
			return false
		}
	}
	for _, n := range r.wantRefs {
		if refs[n] == 0 {
			return false
		}
	}
	return true
}

// TestMarkupCapabilityMatrix asserts each capability row's shared expected edges
// surface for BOTH Astro and Svelte.
func TestMarkupCapabilityMatrix(t *testing.T) {
	t.Parallel()
	for _, row := range capabilityMatrix {
		t.Run(row.name, func(t *testing.T) {
			aCalls, aRefs := surfaced(t, "cap.astro", row.astroSrc)
			sCalls, sRefs := surfaced(t, "cap.svelte", row.svelteSrc)

			for _, n := range row.wantCalls {
				if aCalls[n] == 0 {
					t.Errorf("astro missing call %q; calls=%v refs=%v", n, keysOf(aCalls), keysOf(aRefs))
				}
				if sCalls[n] == 0 {
					t.Errorf("svelte missing call %q; calls=%v refs=%v", n, keysOf(sCalls), keysOf(sRefs))
				}
			}
			for _, n := range row.wantRefs {
				if aRefs[n] == 0 {
					t.Errorf("astro missing ref %q; calls=%v refs=%v", n, keysOf(aCalls), keysOf(aRefs))
				}
				if sRefs[n] == 0 {
					t.Errorf("svelte missing ref %q; calls=%v refs=%v", n, keysOf(sCalls), keysOf(sRefs))
				}
			}
		})
	}
}

// TestSvelteEqualsAstroCapabilities is the literal "rows EQUAL" gate: for every
// capability row, the Svelte cell must pass IFF the Astro cell passes.
func TestSvelteEqualsAstroCapabilities(t *testing.T) {
	t.Parallel()
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

// TestNoDuplicateMarkupEdges is the regression guard for the two-producer
// duplicate-edge class (pr-review-council CRITICAL): a template-region call must
// be emitted EXACTLY ONCE (by the clean markupExprReparse producer), never doubled
// by a raw-file error-recovery parse — and the surviving edge must carry the CLEAN
// receiver (`user`, not the garbled `{user`). Covers BOTH Astro and Svelte, since
// the fix corrects the pre-existing shared-mechanism double-emit in both.
func TestNoDuplicateMarkupEdges(t *testing.T) {
	t.Parallel()
	type ec struct {
		path string
		src  string
		call string   // this call name must appear exactly once
		recv string   // its receiver must be the clean value
		refs []string // each argref must appear exactly once
	}
	cases := []ec{
		{"m.svelte", "<p>{user.greet()}</p>\n", "greet", "user", nil},
		{"m.astro", "---\n---\n<p>{user.greet()}</p>\n", "greet", "user", nil},
		{"c.svelte", "<div>{#if ready}{render(node)}{/if}</div>\n", "render", "", []string{"node"}},
		{"c.astro", "---\n---\n<div>{ready && render(node)}</div>\n", "render", "", []string{"node"}},
	}
	for _, c := range cases {
		cs := callSitesFor(t, c.path, c.src)
		n, recv := 0, ""
		for _, s := range cs {
			if s.Name == c.call && !s.IsArgRef {
				n++
				recv = s.Receiver
			}
		}
		if n != 1 {
			t.Errorf("%s: call %q count=%d, want 1 (duplicate-edge regression); all=%s", c.path, c.call, n, dumpCS(cs))
		}
		if recv != c.recv {
			t.Errorf("%s: call %q receiver=%q, want %q (clean producer must survive)", c.path, c.call, recv, c.recv)
		}
		for _, r := range c.refs {
			rn := 0
			for _, s := range cs {
				if s.Name == r && s.IsArgRef {
					rn++
				}
			}
			if rn != 1 {
				t.Errorf("%s: argref %q count=%d, want 1 (duplicate-edge regression); all=%s", c.path, r, rn, dumpCS(cs))
			}
		}
	}
}

// TestScriptRegionCallsPreserved proves the single-producer split does NOT drop
// <script>/frontmatter calls: those still surface (from ScriptCalls), exactly
// once, while the template call surfaces once from MarkupCalls.
func TestScriptRegionCallsPreserved(t *testing.T) {
	t.Parallel()
	svelte := "<script>\n  import { fetchUser } from './api';\n  function load() { return fetchUser(1); }\n</script>\n<p>{user.greet()}</p>\n"
	calls, _ := surfaced(t, "s.svelte", svelte)
	if calls["fetchUser"] != 1 {
		t.Errorf("svelte script call fetchUser count=%d, want 1", calls["fetchUser"])
	}
	if calls["greet"] != 1 {
		t.Errorf("svelte template call greet count=%d, want 1", calls["greet"])
	}

	astro := "---\nimport { track } from './a';\nconst x = track(1);\n---\n<p>{user.greet()}</p>\n"
	aCalls, _ := surfaced(t, "s.astro", astro)
	if aCalls["track"] != 1 {
		t.Errorf("astro frontmatter call track count=%d, want 1", aCalls["track"])
	}
	if aCalls["greet"] != 1 {
		t.Errorf("astro template call greet count=%d, want 1", aCalls["greet"])
	}
}

// TestSvelteBlockHeaderCalls is the RED->GREEN gate for the sigil-aware
// block-header extraction: every Svelte control-flow / special tag surfaces the
// calls and refs carried by its HEADER EXPR as ordinary edges, exactly like a
// plain mustache. Without svelteHandler.MarkupCalls (the Phase-3 wiring) none of
// these surface.
func TestSvelteBlockHeaderCalls(t *testing.T) {
	t.Parallel()
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
		if calls[n] == 0 {
			t.Errorf("missing block-header call %q; calls=%v", n, keysOf(calls))
		}
	}
	for _, n := range []string{"user", "row", "fallback", "id", "items", "raw", "row2"} {
		if refs[n] == 0 {
			t.Errorf("missing block-header ref %q; refs=%v", n, keysOf(refs))
		}
	}
}
