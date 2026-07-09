package preproc

import "testing"

// sliceExprs returns the EXACT extracted EXPR substrings for a Svelte template
// source — the white-box view of the sigil-aware scanner's delimiting.
func sliceExprs(src string) []string {
	b := []byte(src)
	ranges := scanSvelteExprRanges(b)
	out := make([]string, len(ranges))
	for i, r := range ranges {
		out[i] = string(b[r.start:r.end])
	}
	return out
}

func assertExprs(t *testing.T, got, want []string) {
	t.Helper()
	if !equalStrs(got, want) {
		t.Errorf("exprs = %#v, want %#v", got, want)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSvelteExprRanges_PlainMustache(t *testing.T) {
	t.Parallel()
	assertExprs(t, sliceExprs("<p>{format(count)}</p>\n<span>{count}</span>\n"),
		[]string{"format(count)", "count"})
}

func TestSvelteExprRanges_AttributeMustache(t *testing.T) {
	t.Parallel()
	// Attribute expressions are plain mustaches at the top level of the template.
	assertExprs(t, sliceExprs("<Card x={fmt(i)} class:on={active}/>\n"),
		[]string{"fmt(i)", "active"})
}

func TestSvelteExprRanges_IfKeyHeader(t *testing.T) {
	t.Parallel()
	assertExprs(t, sliceExprs("{#if user.isAdmin && canEdit(user)}<b/>{/if}\n"),
		[]string{"user.isAdmin && canEdit(user)"})
	assertExprs(t, sliceExprs("{#key sel.id}<X/>{/key}\n"), []string{"sel.id"})
}

// TestSvelteExprRanges_EachStripsBinding pins the tricky delimiting case from the
// plan's risk section: EXPR is a.b(c); the `as {x,y}` destructure binding must NOT
// be part of the expression.
func TestSvelteExprRanges_EachStripsBinding(t *testing.T) {
	t.Parallel()
	assertExprs(t, sliceExprs("{#each a.b(c) as {x,y}}<i/>{/each}\n"), []string{"a.b(c)"})
	assertExprs(t, sliceExprs("{#each items as item}<i/>{/each}\n"), []string{"items"})
	assertExprs(t, sliceExprs("{#each getRows() as r, i (r.id)}<i/>{/each}\n"), []string{"getRows()"})
	// 'casts' contains the substring 'as' — must not be mistaken for the binding.
	assertExprs(t, sliceExprs("{#each casts as c}<i/>{/each}\n"), []string{"casts"})
	// Svelte-5 binding-less each.
	assertExprs(t, sliceExprs("{#each rows()}<i/>{/each}\n"), []string{"rows()"})
}

func TestSvelteExprRanges_AwaitThenCatch(t *testing.T) {
	t.Parallel()
	assertExprs(t, sliceExprs("{#await load() then v}<i/>{/await}\n"), []string{"load()"})
	assertExprs(t, sliceExprs("{#await p catch e}<i/>{/await}\n"), []string{"p"})
	assertExprs(t, sliceExprs("{#await fetchThing()}<i/>{/await}\n"), []string{"fetchThing()"})
}

func TestSvelteExprRanges_ElseIf(t *testing.T) {
	t.Parallel()
	assertExprs(t, sliceExprs("{:else if ready(x)}\n"), []string{"ready(x)"})
}

func TestSvelteExprRanges_SpecialTags(t *testing.T) {
	t.Parallel()
	assertExprs(t, sliceExprs("{@const total = sum(a, b)}\n"), []string{"sum(a, b)"})
	assertExprs(t, sliceExprs("{@html render(item)}\n"), []string{"render(item)"})
	assertExprs(t, sliceExprs("{@render row(item)}\n"), []string{"row(item)"})
	// @const with a destructured target and an object-literal is still bounded to
	// the RHS expression.
	assertExprs(t, sliceExprs("{@const {a} = pick(src)}\n"), []string{"pick(src)"})
}

// TestSvelteExprRanges_NoExprTags proves the block-close, plain-else, and binding
// continuations produce NOTHING (never mis-parsed as plain mustaches).
func TestSvelteExprRanges_NoExprTags(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"{/if}\n", "{/each}\n", "{/await}\n", "{/key}\n",
		"{:else}\n", "{:then value}\n", "{:catch err}\n",
		"{#snippet foo(x)}\n{/snippet}\n",
	} {
		if got := sliceExprs(s); len(got) != 0 {
			t.Errorf("%q: expected no exprs, got %#v", s, got)
		}
	}
}

// TestSvelteExprRanges_SkipsScriptStyle proves braces inside <script>/<style> are
// not scanned as template expressions.
func TestSvelteExprRanges_SkipsScriptStyle(t *testing.T) {
	t.Parallel()
	src := "<script>\nconst o = {a: 1};\nfn({b: 2});\n</script>\n" +
		"<style>\n.x { color: red; }\n</style>\n" +
		"<p>{go(x)}</p>\n"
	assertExprs(t, sliceExprs(src), []string{"go(x)"})
}

// svelteDelimCase is one hand-verified header → expected-EXPR extraction, used as
// both a correctness assertion and the corpus for the delimiting-accuracy metric.
type svelteDelimCase struct {
	src  string
	want []string
}

// svelteDelimCorpus is a small, hand-verified corpus of real-shaped Svelte header
// forms. It drives TestSvelteExprDelimitingAccuracy (the documented quality
// metric) and doubles as a per-form correctness assertion.
var svelteDelimCorpus = []svelteDelimCase{
	{"{#if a && b(c)}", []string{"a && b(c)"}},
	{"{#each rows as r}", []string{"rows"}},
	{"{#each list.filter(p => p.on) as {id}}", []string{"list.filter(p => p.on)"}},
	{"{#each casts as c}", []string{"casts"}},
	{"{#each labels as l, i (l.k)}", []string{"labels"}},
	{"{#await load(id) then data}", []string{"load(id)"}},
	{"{#await q catch e}", []string{"q"}},
	{"{#key sel.id}", []string{"sel.id"}},
	{"{:else if canShow(u)}", []string{"canShow(u)"}},
	{"{@const n = total(items)}", []string{"total(items)"}},
	{"{@html sanitize(raw)}", []string{"sanitize(raw)"}},
	{"{@render tpl(row)}", []string{"tpl(row)"}},
	{"{value}", []string{"value"}},
	{"{fn(a, b)}", []string{"fn(a, b)"}},
	{"{/each}", nil},
	{"{:else}", nil},
	{"{:then v}", nil},
}

// TestSvelteExprDelimitingAccuracy is a DOCUMENTED quality metric, not a grammar
// auto-adopt gate: it measures how precisely the sigil-aware scanner delimits the
// header EXPR on a hand-verified corpus and logs the score.
//
// It IS the parity-arc's expr-delimiting BLOCKING regression check named in
// docs/adr/0001-frontend-parse-parity.md (Phase 5): the assertion below was
// already hard (t.Errorf, not t.Logf) when this test was written in Phase 3, so
// "promoting" it in Phase 5 required no code change — only naming it as part of
// the parity contract and holding the line that floorPct never gets lowered to
// paper over a real regression. Any drop below floorPct fails `go test
// ./internal/parser/preproc/...`, which `make preflight` runs on every PR
// (.github/workflows/preflight.yml) and `preflight` is a required merge check on
// this public repo (CLAUDE.md ## CI). The curated corpus is hand-verified, so any
// regression below 100% is a real delimiting bug, not corpus noise.
func TestSvelteExprDelimitingAccuracy(t *testing.T) {
	t.Parallel()
	correct := 0
	for _, c := range svelteDelimCorpus {
		got := sliceExprs(c.src)
		if equalStrs(got, c.want) {
			correct++
		} else {
			t.Logf("delimit miss: %q -> got %#v, want %#v", c.src, got, c.want)
		}
	}
	total := len(svelteDelimCorpus)
	pct := 100.0 * float64(correct) / float64(total)
	t.Logf("Svelte header-EXPR delimiting accuracy: %d/%d = %.1f%%", correct, total, pct)

	const floorPct = 100.0
	if pct < floorPct {
		t.Errorf("delimiting accuracy %.1f%% below documented floor %.1f%%", pct, floorPct)
	}
}
