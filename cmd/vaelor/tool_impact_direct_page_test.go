package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/impact"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// addTransitiveCallers gives every direct caller of Foo perCaller callers of
// its own, all in one long-named package, so the transitive list alone
// overflows the budget while the direct page stays small.
func addTransitiveCallers(cg *callgraph.CallGraph, root string, perCaller int) {
	direct := cg.Edges
	for _, e := range direct {
		for j := range perCaller {
			up := &parser.Symbol{
				Name:      fmt.Sprintf("%s_upstream_caller_with_a_long_descriptive_name_%02d", e.Caller.Name, j),
				Kind:      parser.KindFunction,
				File:      fmt.Sprintf("%s/internal/a/rather/deeply/nested/upstream/package/%s_%02d.go", root, e.Caller.Name, j),
				StartLine: 1,
				EndLine:   9,
			}
			cg.Symbols = append(cg.Symbols, up)
			cg.Edges = append(cg.Edges, callgraph.CallEdge{Caller: up, Callee: e.Caller, CalleeName: e.Caller.Name, Line: 5})
		}
	}
}

type directPageResp struct {
	DirectCallers          []impactCaller   `json:"direct_callers"`
	TransitiveCallers      *json.RawMessage `json:"transitive_callers"`
	TransitiveCallersCount int              `json:"transitive_callers_count"`
	Notes                  []string         `json:"notes"`
}

func parseDirectPage(t *testing.T, text string) directPageResp {
	t.Helper()
	body := mcpmeta.StripBudgetMarker(text)
	if idx := strings.Index(body, "<!--"); idx >= 0 {
		body = body[:idx]
	}
	var resp directPageResp
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &resp); err != nil {
		t.Fatalf("parse: %v\n%s", err, truncForLog(body, 400))
	}
	return resp
}

// A widely used symbol's transitive list alone overflows the budget (live:
// ParseFile, 151 transitive callers, every page fell to counts). The
// direct-caller page, with the snippets asked for, must still come back
// inline, and the note must say where the transitive callers went.
//
// RED-on-mutation: in handleImpact (cmd/vaelor/tool_impact.go) delete the
// `{Name: "direct-page", …}` rung from the ladder literal and the
// "direct-page-no-snippets" append; the response falls to the counts rung
// and direct_callers is absent.
func TestImpact_DirectPageSurvivesLargeTransitiveSet(t *testing.T) {
	root := t.TempDir()
	cg := buildSnippetCallGraphN(t, root, 10)
	addTransitiveCallers(cg, root, 15)
	defer setupImpactBuildSeam(t, cg)()

	res, err := handleImpact(context.Background(), ImpactInput{
		Repo: root, Symbol: "Foo", IncludeSnippets: true,
	}, impactLadderDeps(), nil, t.TempDir())
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}
	text := impactResultText(t, res)
	resp := parseDirectPage(t, text)

	if len(resp.DirectCallers) != 10 {
		t.Fatalf("the direct-caller page must survive a large transitive set, got %d callers:\n%s",
			len(resp.DirectCallers), truncForLog(text, 600))
	}
	for _, c := range resp.DirectCallers {
		if c.Snippet == "" {
			t.Fatalf("requested snippets must outlive the transitive list, %s has none", c.Name)
		}
	}
	if resp.TransitiveCallers != nil {
		t.Fatalf("this test must exercise the direct-page rung; transitive_callers is still listed")
	}
	if resp.TransitiveCallersCount != 150 {
		t.Fatalf("transitive_callers_count = %d, want 150", resp.TransitiveCallersCount)
	}
	if !strings.Contains(strings.Join(resp.Notes, "\n"), "transitive_callers (150) left out") {
		t.Fatalf("a note must say the transitive list was left out, notes=%q", resp.Notes)
	}
	if !strings.Contains(text, "full-result:") {
		t.Fatalf("the full result must be saved when the ladder condenses:\n%s", truncForLog(text, 600))
	}
}

// A small transitive set must survive snippet shedding: snippets go before
// either caller list, so when the lists fit without snippets both arrive.
//
// RED-on-mutation: in handleImpact delete the "no-snippets" rung append; the
// ladder then jumps to direct-page and drops the transitive list.
func TestImpact_SmallTransitiveSetOutlivesSnippets(t *testing.T) {
	root := t.TempDir()
	cg := buildSnippetCallGraphN(t, root, 5)
	addTransitiveCallers(cg, root, 1)
	defer setupImpactBuildSeam(t, cg)()

	run := func(snippets bool, maxBytes int) string {
		t.Helper()
		res, err := handleImpact(context.Background(), ImpactInput{
			Repo: root, Symbol: "Foo", IncludeSnippets: snippets, MaxBytes: maxBytes,
		}, impactLadderDeps(), nil, "")
		if err != nil {
			t.Fatalf("handleImpact: %v", err)
		}
		return mcpmeta.StripBudgetMarker(impactResultText(t, res))
	}
	// Size the budget from the fixture itself, so path lengths cannot move
	// it: both lists without snippets fit, the full rendering does not.
	withSnippets, without := run(true, mcpmeta.MaxBudget), run(false, mcpmeta.MaxBudget)
	if strings.Contains(withSnippets, "condensed") || strings.Contains(without, "condensed") {
		t.Fatalf("fixture must fit MaxBudget uncondensed (%d / %d bytes)", len(withSnippets), len(without))
	}
	budget := (len(withSnippets) + len(without)) / 2

	text := run(true, budget)
	resp := parseDirectPage(t, text)
	if resp.TransitiveCallers == nil {
		t.Fatalf("a transitive list that fits without snippets must be kept (budget %d):\n%s", budget, truncForLog(text, 600))
	}
	if len(resp.DirectCallers) != 5 {
		t.Fatalf("direct callers = %d, want 5", len(resp.DirectCallers))
	}
	for _, c := range resp.DirectCallers {
		if c.Snippet != "" {
			t.Fatalf("snippets must be shed before the transitive list, %s has one", c.Name)
		}
	}
}

// The hotspot list spans both tiers; the direct-page rung keeps only the names
// it carries.
//
// RED-on-mutation: in formatImpactDirectPage (cmd/vaelor/tool_impact.go) set
// HotspotCallers to output.HotspotCallers; the rendered rung then names T1.
func TestImpact_DirectPageHotspotsStayOnPage(t *testing.T) {
	out := impactOutput{
		Result: &impact.Result{
			Symbol:            "Foo",
			Found:             true,
			DirectCallers:     []impact.AffectedSymbol{{Name: "A"}, {Name: "B"}},
			TransitiveCallers: []impact.AffectedSymbol{{Name: "T1"}, {Name: "T2"}},
		},
		HotspotCallers: []string{"T1", "B", "T2", "A"},
	}
	var resp struct {
		HotspotCallers      []string `json:"hotspot_callers"`
		HotspotCallersCount int      `json:"hotspot_callers_count"`
	}
	if err := json.Unmarshal([]byte(formatImpactDirectPage(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if strings.Join(resp.HotspotCallers, ",") != "B,A" {
		t.Fatalf("hotspot_callers = %v, want [B A]: the rung must not name callers it does not carry", resp.HotspotCallers)
	}
	if resp.HotspotCallersCount != 4 {
		t.Fatalf("hotspot_callers_count = %d, want 4 (both tiers)", resp.HotspotCallersCount)
	}
}

func TestOmittedCallerListsNote(t *testing.T) {
	if got := omittedCallerListsNote(0, 0); got != "" {
		t.Fatalf("nothing left out must give no note, got %q", got)
	}
	got := omittedCallerListsNote(151, 11)
	for _, want := range []string{"transitive_callers (151) and hidden_callers (11)", "full-result pointer", "depth=1 drops the transitive walk"} {
		if !strings.Contains(got, want) {
			t.Fatalf("note %q lacks %q", got, want)
		}
	}
	// depth=1 does not touch hidden callers, so it must not be offered for them.
	if got := omittedCallerListsNote(0, 5); strings.Contains(got, "depth=1") {
		t.Fatalf("hidden-only note must not suggest depth=1: %q", got)
	}
}
