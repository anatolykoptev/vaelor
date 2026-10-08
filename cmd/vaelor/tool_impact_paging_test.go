package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// --- impact_analysis caller paging + opt-in snippets (#892, #896) ---
//
// Each test drives the REAL handleImpact path via the impactBuildFromRepo
// test seam (same as the ladder tests) so mutating the production call site —
// the max_callers application, the offset arithmetic, the include_snippets
// gate — goes RED.
//
// A 104-caller result is ~13 KB — over MaxBudget (9000), so it always
// condenses. The full listing then lives in the OUTPUT_DIR spill file (the
// same mechanism TestImpact_LargeResultWithOutputDir_FileSaved uses), which
// is what these tests read.

// buildPagingCallGraph builds a CallGraph with target "Foo" and n direct
// callers Caller000..Caller{n-1} in deterministic BFS (edge) order. Caller
// files are NOT created on disk — for the snippet test use
// buildSnippetCallGraph instead.
func buildPagingCallGraph(root string, n int) *callgraph.CallGraph {
	target := &parser.Symbol{
		Name: "Foo", Kind: parser.KindFunction,
		File: root + "/foo.go", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{Symbols: []*parser.Symbol{target}, Tier: "basic"}
	for i := 0; i < n; i++ {
		c := &parser.Symbol{
			Name:      fmt.Sprintf("Caller%03d", i),
			Kind:      parser.KindFunction,
			File:      fmt.Sprintf("%s/c%03d.go", root, i),
			StartLine: 1, EndLine: 6,
		}
		cg.Symbols = append(cg.Symbols, c)
		cg.Edges = append(cg.Edges, callgraph.CallEdge{
			Caller: c, Callee: target, CalleeName: "Foo", Line: 4,
		})
	}
	return cg
}

// impactCaller is the decoded shape of one direct_callers entry.
type impactCaller struct {
	Name    string `json:"name"`
	File    string `json:"file"`
	Snippet string `json:"snippet"`
}

// impactDirectCallers extracts the decoded direct_callers array from a
// handleImpact text result (rung-1/rung-2 JSON envelope + budget/condensation
// footers stripped).
func impactDirectCallers(t *testing.T, text string) []impactCaller {
	t.Helper()
	body := mcpmeta.StripBudgetMarker(text)
	if idx := strings.Index(body, "<!--"); idx >= 0 {
		body = body[:idx]
	}
	var resp struct {
		DirectCallers []impactCaller `json:"direct_callers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &resp); err != nil {
		t.Fatalf("direct_callers JSON parse: %v\nbody (first 400):\n%s",
			err, truncForLog(body, 400))
	}
	return resp.DirectCallers
}

// impactResultBody runs handleImpact with outputDir set and returns the
// rung-1 result body: the spilled full-result file when the response
// condensed past the budget (a 104-caller page always does — ~13 KB over the
// 9000-byte MaxBudget), else the inline JSON.
func impactResultBody(t *testing.T, input ImpactInput, outputDir string) string {
	t.Helper()
	res, err := handleImpact(context.Background(), input, impactLadderDeps(), nil, outputDir)
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}
	text := impactResultText(t, res)
	if pathStart := strings.Index(text, "saved to: "); pathStart >= 0 {
		pathStart += len("saved to: ")
		pathEnd := strings.Index(text[pathStart:], " —")
		if pathEnd < 0 {
			t.Fatal("cannot find end of path in spill pointer")
		}
		data, err := os.ReadFile(text[pathStart : pathStart+pathEnd])
		if err != nil {
			t.Fatalf("saved file must exist and be readable: %s", err)
		}
		return string(data)
	}
	body := mcpmeta.StripBudgetMarker(text)
	if idx := strings.Index(body, "<!--"); idx >= 0 {
		body = body[:idx]
	}
	return body
}

func savedDirectCallers(t *testing.T, saved string) []impactCaller {
	t.Helper()
	var resp struct {
		DirectCallers []impactCaller `json:"direct_callers"`
	}
	if err := json.Unmarshal([]byte(saved), &resp); err != nil {
		t.Fatalf("saved file must be parseable JSON: %v\n(first 400):\n%s",
			err, truncForLog(saved, 400))
	}
	return resp.DirectCallers
}

// TestImpact_MaxCallers_ListsBeyondDefaultCap is the #892 acceptance test:
// on a symbol with 104 direct callers, max_callers=200 lists ALL of them and
// the cap note disappears.
//
// RED-on-mutation: change the maxCallers resolution in handleImpact to a hard
// 100 and this goes RED (only 100 callers listed, note reappears).
func TestImpact_MaxCallers_ListsBeyondDefaultCap(t *testing.T) {
	root := t.TempDir()
	cg := buildPagingCallGraph(root, 104)
	defer setupImpactBuildSeam(t, cg)()

	saved := impactResultBody(t, ImpactInput{
		Repo: root, Symbol: "Foo", MaxCallers: 200,
	}, t.TempDir())

	callers := savedDirectCallers(t, saved)
	if len(callers) != 104 {
		t.Fatalf("max_callers=200 must list all 104 direct callers, got %d", len(callers))
	}
	if callers[0].Name != "Caller000" || callers[103].Name != "Caller103" {
		t.Fatalf("caller order drifted: first=%q last=%q", callers[0].Name, callers[103].Name)
	}
	if strings.Contains(saved, "omitted") {
		t.Fatalf("cap note must disappear when max_callers covers every caller:\n%s",
			truncForLog(saved, 400))
	}
}

// TestImpact_Offset_PageTwoIsExactlyTheOmittedNames is the #892 paging test:
// page two must be exactly the names page one omitted — no overlap, no gap.
//
// RED-on-mutation: break the offset arithmetic by one at the call site
// (offset-1 or offset+1) and the page-two set overlaps or loses a caller.
func TestImpact_Offset_PageTwoIsExactlyTheOmittedNames(t *testing.T) {
	root := t.TempDir()
	cg := buildPagingCallGraph(root, 104)
	defer setupImpactBuildSeam(t, cg)()
	outDir := t.TempDir()

	// Page one: defaults (max_callers=100, offset=0).
	saved1 := impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo"}, outDir)
	page1 := savedDirectCallers(t, saved1)
	if len(page1) != 100 {
		t.Fatalf("default page must list 100 of 104 callers, got %d", len(page1))
	}
	if !strings.Contains(saved1, "lists 100 of 104") || !strings.Contains(saved1, "4 omitted") {
		t.Fatalf("page-one cap note missing/incorrect:\n%s", truncForLog(saved1, 400))
	}

	// Page two: offset=100.
	saved2 := impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo", Offset: 100}, outDir)
	page2 := savedDirectCallers(t, saved2)

	// Page two = exactly the 4 omitted names: Caller100..Caller103.
	want := map[string]bool{"Caller100": true, "Caller101": true, "Caller102": true, "Caller103": true}
	if len(page2) != len(want) {
		t.Fatalf("page two must be exactly the %d omitted callers, got %d: %v",
			len(want), len(page2), callerNames(page2))
	}
	page1Set := make(map[string]bool, len(page1))
	for _, c := range page1 {
		page1Set[c.Name] = true
	}
	for _, c := range page2 {
		if !want[c.Name] {
			t.Fatalf("page two contained unexpected caller %q (offset arithmetic off)", c.Name)
		}
		if page1Set[c.Name] {
			t.Fatalf("page two overlaps page one at %q (offset arithmetic off-by-one?)", c.Name)
		}
		delete(want, c.Name)
	}
	if len(want) > 0 {
		t.Fatalf("page two missed omitted callers: %v", want)
	}
	// Nothing left beyond page two → the note says it is the last page and
	// that 100 callers sit before it, instead of going silent.
	if !strings.Contains(saved2, "this is the last page") || !strings.Contains(saved2, "100 omitted before this page") {
		t.Fatalf("last page must say it is the last page and count the skipped head:\n%s", truncForLog(saved2, 600))
	}
}

func callerNames(callers []impactCaller) []string {
	names := make([]string, len(callers))
	for i, c := range callers {
		names[i] = c.Name
	}
	return names
}

// buildSnippetCallGraph builds a CallGraph whose direct callers have REAL
// files on disk (so call-site snippets can be extracted) plus one caller
// whose file is missing (graceful-skip case). Each real file has the call to
// Foo on line 4.
func buildSnippetCallGraph(t *testing.T, root string) *callgraph.CallGraph {
	t.Helper()
	target := &parser.Symbol{
		Name: "Foo", Kind: parser.KindFunction,
		File: root + "/foo.go", StartLine: 1, EndLine: 5,
	}
	cg := &callgraph.CallGraph{Symbols: []*parser.Symbol{target}, Tier: "basic"}

	for i := 0; i < 2; i++ {
		file := fmt.Sprintf("%s/c%03d.go", root, i)
		content := fmt.Sprintf("package p\n\nfunc Caller%03d() {\n\tFoo()\n}\n", i)
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatalf("write caller file: %v", err)
		}
		c := &parser.Symbol{
			Name:      fmt.Sprintf("Caller%03d", i),
			Kind:      parser.KindFunction,
			File:      file,
			StartLine: 3, EndLine: 5,
		}
		cg.Symbols = append(cg.Symbols, c)
		cg.Edges = append(cg.Edges, callgraph.CallEdge{
			Caller: c, Callee: target, CalleeName: "Foo", Line: 4,
		})
	}
	// Caller whose file does not exist on disk — snippet must be skipped.
	gone := &parser.Symbol{
		Name: "CallerGone", Kind: parser.KindFunction,
		File: root + "/gone.go", StartLine: 1, EndLine: 5,
	}
	cg.Symbols = append(cg.Symbols, gone)
	cg.Edges = append(cg.Edges, callgraph.CallEdge{
		Caller: gone, Callee: target, CalleeName: "Foo", Line: 2,
	})
	return cg
}

// TestImpact_IncludeSnippets_OptInAttachesCallSite is the #896 acceptance
// test: include_snippets=true attaches the source lines around each listed
// direct caller's call site; callers whose files cannot be read simply carry
// no snippet.
//
// RED-on-mutation: make the snippet gate default-on in handleImpact and the
// first half goes RED; drop the call-site line propagation in traverseCallers
// and the second half goes RED.
func TestImpact_IncludeSnippets_OptInAttachesCallSite(t *testing.T) {
	root := t.TempDir()
	cg := buildSnippetCallGraph(t, root)
	defer setupImpactBuildSeam(t, cg)()
	deps := impactLadderDeps()

	// Default call: snippets must NOT appear — the small answer is a feature.
	resDef, err := handleImpact(context.Background(), ImpactInput{
		Repo: root, Symbol: "Foo", MaxBytes: 4_000_000,
	}, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact default: %v", err)
	}
	if text := impactResultText(t, resDef); strings.Contains(text, `"snippet"`) {
		t.Fatalf("default output must not carry snippets:\n%s", truncForLog(text, 400))
	}

	// Opt-in call: every listed caller with a readable file gets the call
	// line (±1 context line, numbered).
	res, err := handleImpact(context.Background(), ImpactInput{
		Repo: root, Symbol: "Foo", IncludeSnippets: true, MaxBytes: 4_000_000,
	}, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact include_snippets: %v", err)
	}
	callers := impactDirectCallers(t, impactResultText(t, res))
	if len(callers) != 3 {
		t.Fatalf("expected 3 direct callers, got %d", len(callers))
	}
	byName := make(map[string]impactCaller, len(callers))
	for _, c := range callers {
		byName[c.Name] = c
	}
	for _, name := range []string{"Caller000", "Caller001"} {
		snippet := byName[name].Snippet
		if snippet == "" {
			t.Fatalf("%s: expected call-site snippet, got empty", name)
		}
		if !strings.Contains(snippet, "Foo()") {
			t.Fatalf("%s: snippet must contain the call line, got:\n%s", name, snippet)
		}
		if !strings.Contains(snippet, "4│") {
			t.Fatalf("%s: snippet must number the call line, got:\n%s", name, snippet)
		}
	}
	if s := byName["CallerGone"].Snippet; s != "" {
		t.Fatalf("unreadable file must skip the snippet, got:\n%s", s)
	}
}
