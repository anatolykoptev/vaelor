package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/impact"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestDirectCallersTruncationNote_NamesOmittedAndSaysTheyAreCounted(t *testing.T) {
	t.Parallel()
	var omitted []impact.AffectedSymbol
	for i := range 13 {
		omitted = append(omitted, impact.AffectedSymbol{Name: fmt.Sprintf("Caller%02d", i)})
	}
	note := directCallersTruncationNote(100, 113, 0, 100, omitted, "owner/repo", "ParseFile", "enhanced")

	for _, want := range []string{
		"lists 100 of 113", "13 omitted", "Caller00", "Caller09", "+3 more",
		"total_affected includes the omitted",
		// #892: the note names the paging params, not call_trace.
		`impact_analysis repo="owner/repo" symbol="ParseFile"`, "offset=100", "max_callers=100",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "Caller10") {
		t.Errorf("note must cap spelled-out names at %d:\n%s", maxOmittedNamesInNote, note)
	}
}

// The cap is applied inside handleImpact; this drives the real handler so
// removing the call-site wiring (not just the formatter) goes RED.
func TestImpact_DirectCapNote_EmittedByHandler(t *testing.T) {
	root := t.TempDir()
	target := &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: root + "/foo.go", StartLine: 1, EndLine: 5}
	cg := &callgraph.CallGraph{Symbols: []*parser.Symbol{target}, Tier: "basic"}
	for i := range 105 {
		c := &parser.Symbol{Name: fmt.Sprintf("Caller%03d", i), Kind: parser.KindFunction, File: fmt.Sprintf("%s/c%d.go", root, i), StartLine: 1, EndLine: 3}
		cg.Symbols = append(cg.Symbols, c)
		cg.Edges = append(cg.Edges, callgraph.CallEdge{Caller: c, Callee: target, CalleeName: "Foo", Line: 2})
	}
	defer setupImpactBuildSeam(t, cg)()

	res, err := handleImpact(context.Background(), ImpactInput{Repo: root, Symbol: "Foo", MaxBytes: 4_000_000}, impactLadderDeps(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	text := impactResultText(t, res)
	for _, want := range []string{"lists 100 of 105", "5 omitted", "total_affected includes the omitted", "impact_analysis repo=", "offset="} {
		if !strings.Contains(text, want) {
			t.Errorf("handler output missing %q", want)
		}
	}
}
