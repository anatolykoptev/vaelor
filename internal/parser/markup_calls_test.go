package parser_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestAstroMarkupExprCalls is the Phase-1 Astro capability gate: template-body
// {expr} ranges must surface their calls-in-markup as CallSites. Two of the
// assertions are RED→GREEN discriminators that the pre-Phase-1 raw-.astro-as-TS
// path cannot satisfy:
//   - "map" ({items.map(i => <Card/>)}): needs the TSX grammar to parse the JSX
//     embedded in the expression; a plain-TS reparse errors on <Card/>.
//   - bare "count" ({count}): typescript_calls.scm has no bare-identifier
//     capture, so a lone {count} is never a CallSite without markup_refs.scm.
func TestAstroMarkupExprCalls(t *testing.T) {
	t.Parallel()
	src := readMarkupFixture(t)

	calls, err := parser.ExtractCalls("markup_exprs.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	// {format(count)} on line 8 — a plain call.
	if !hasCall(calls, "format", 8, false) {
		t.Errorf("missing markup call format@8; got %s", formatCalls(calls))
	}
	// {items.map(i => <Card id={i}/>)} on line 9 — method call, needs TSX to
	// parse the embedded <Card/> JSX.
	if !hasCall(calls, "map", 9, false) {
		t.Errorf("missing markup method map@9 (TSX-reparse discriminator); got %s", formatCalls(calls))
	}
	// {count} on line 10 — bare top-level identifier, needs markup_refs.scm.
	if !hasArgRef(calls, "count", 10) {
		t.Errorf("missing bare markup ref count@10 (markup_refs discriminator); got %s", formatCalls(calls))
	}
}

// TestAstroMarkupExprRefs asserts the JSX-in-expression component reference
// (<Card/> inside {items.map(...)}) is captured as a TemplateRef. This already
// works via scanTemplateRefs (which walks into {expr} ranges); the test pins the
// Phase-1 gate that JSX-in-expr refs light up.
func TestAstroMarkupExprRefs(t *testing.T) {
	t.Parallel()
	src := readMarkupFixture(t)

	result, err := parser.ParseFile("markup_exprs.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	found := false
	for _, r := range result.TemplateRefs {
		if r.Name == "Card" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(result.TemplateRefs))
		for i, r := range result.TemplateRefs {
			names[i] = r.Name
		}
		t.Errorf("TemplateRefs missing JSX-in-expr ref Card; got %v", names)
	}
}

func readMarkupFixture(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "astro", "markup_exprs.astro"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return src
}

func hasCall(calls []parser.CallSite, name string, line uint32, argref bool) bool {
	for _, c := range calls {
		if c.Name == name && c.Line == line && c.IsArgRef == argref {
			return true
		}
	}
	return false
}

func hasArgRef(calls []parser.CallSite, name string, line uint32) bool {
	for _, c := range calls {
		if c.Name == name && c.Line == line && c.IsArgRef {
			return true
		}
	}
	return false
}

func formatCalls(calls []parser.CallSite) string {
	out := "["
	for i, c := range calls {
		if i > 0 {
			out += ", "
		}
		kind := "call"
		if c.IsArgRef {
			kind = "argref"
		}
		out += c.Name
		if c.Receiver != "" {
			out += "(recv=" + c.Receiver + ")"
		}
		out += "@" + strconv.FormatUint(uint64(c.Line), 10) + ":" + kind
	}
	return out + "]"
}
