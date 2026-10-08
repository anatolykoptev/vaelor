package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/impact"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// Edge cases of the #892 paging window, found by review of the first cut.

// A huge max_callers with a large offset must not overflow offset+max_callers
// into a negative slice bound (the first cut panicked the handler goroutine).
//
// RED-on-mutation: in pageWindow drop the min(…, maxImpactMaxCallers) clamp and
// in pageDirectCallers compute `end := min(offset+size, len(callers))`; this
// test then panics with "slice bounds out of range".
func TestImpact_Paging_HugeMaxCallersDoesNotOverflow(t *testing.T) {
	root := t.TempDir()
	defer setupImpactBuildSeam(t, buildPagingCallGraph(root, 1100))()

	saved := impactResultBody(t, ImpactInput{
		Repo: root, Symbol: "Foo", MaxCallers: math.MaxInt - 1, Offset: 1050,
	}, t.TempDir())

	got := savedDirectCallers(t, saved)
	// The window is cut from the (file, name) order, so the expected tail is
	// the last 50 of the sorted fixture files (not the numerically last).
	files := make([]string, 1100)
	for i := range files {
		files[i] = fmt.Sprintf("%s/c%03d.go", root, i)
	}
	slices.Sort(files)
	if len(got) != 50 {
		t.Fatalf("offset=1050 of 1100 must list exactly 50 callers, got %d", len(got))
	}
	for i, c := range got {
		if c.File != files[1050+i] {
			t.Fatalf("page entry %d = %s, want %s", i, c.File, files[1050+i])
		}
	}
}

// max_callers is clamped, so a request for more than the ceiling still returns
// a page and a note that says where the next one starts.
func TestImpact_Paging_MaxCallersIsClamped(t *testing.T) {
	root := t.TempDir()
	defer setupImpactBuildSeam(t, buildPagingCallGraph(root, maxImpactMaxCallers+20))()

	saved := impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo", MaxCallers: math.MaxInt}, t.TempDir())
	if n := len(savedDirectCallers(t, saved)); n != maxImpactMaxCallers {
		t.Fatalf("max_callers must clamp to %d, listed %d", maxImpactMaxCallers, n)
	}
	if want := fmt.Sprintf("offset=%d (next page)", maxImpactMaxCallers); !strings.Contains(saved, want) {
		t.Fatalf("note must name the next offset %q:\n%s", want, truncForLog(saved, 600))
	}
}

// A middle page counts both what sits before it and what follows, and names
// the next offset.
//
// RED-on-mutation: in directCallersTruncationNote change `head+shown` to
// `shown` (the next offset) or drop `head` from the omitted count.
func TestImpact_Paging_MiddlePageNoteCountsHeadAndTail(t *testing.T) {
	root := t.TempDir()
	defer setupImpactBuildSeam(t, buildPagingCallGraph(root, 250))()

	saved := impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo", Offset: 100}, t.TempDir())
	for _, want := range []string{
		"lists 100 of 250",
		"150 omitted (100 before this page, 50 after",
		"offset=200 (next page)",
	} {
		if !strings.Contains(saved, want) {
			t.Fatalf("middle-page note missing %q:\n%s", want, truncForLog(saved, 700))
		}
	}
}

// An offset past the end returns an explicit explanation, not a silent empty list.
func TestImpact_Paging_OffsetPastEndIsExplained(t *testing.T) {
	root := t.TempDir()
	defer setupImpactBuildSeam(t, buildPagingCallGraph(root, 104))()

	saved := impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo", Offset: 500}, t.TempDir())
	if n := len(savedDirectCallers(t, saved)); n != 0 {
		t.Fatalf("offset past the end must list nothing, got %d", n)
	}
	if !strings.Contains(saved, "offset skips all 104 direct callers") {
		t.Fatalf("empty page must be explained:\n%s", truncForLog(saved, 600))
	}
}

// Pages are cut from one stable order even when the call graph hands the
// edges over in a different order on the next call (typed vs basic tier).
//
// RED-on-mutation: delete the orderForWindow call in handleImpact; page one
// (graph order A) and page two (graph order B) then overlap.
func TestImpact_Paging_StableAcrossEdgeOrder(t *testing.T) {
	root := t.TempDir()
	outDir := t.TempDir()

	cgA := buildPagingCallGraph(root, 150)
	restoreA := setupImpactBuildSeam(t, cgA)
	page1 := savedDirectCallers(t, impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo"}, outDir))
	restoreA()

	cgB := buildPagingCallGraph(root, 150)
	slices.Reverse(cgB.Edges)
	defer setupImpactBuildSeam(t, cgB)()
	page2 := savedDirectCallers(t, impactResultBody(t, ImpactInput{Repo: root, Symbol: "Foo", Offset: 100}, outDir))

	seen := map[string]bool{}
	for _, c := range append(page1, page2...) {
		if seen[c.Name] {
			t.Fatalf("caller %q appears on both pages", c.Name)
		}
		seen[c.Name] = true
	}
	if len(seen) != 150 {
		t.Fatalf("two pages must cover all 150 callers, got %d", len(seen))
	}
}

// With snippets on and a response over the inline budget, the ladder sheds the
// snippets (keeping the caller list) before it falls back to counts.
//
// RED-on-mutation: remove the "no-snippets" rung in handleImpact; the response
// then drops to the counts rung and carries no direct_callers list.
func TestImpact_IncludeSnippets_ShedBeforeCallersDrop(t *testing.T) {
	root := t.TempDir()
	cg := buildSnippetCallGraphN(t, root, 20)
	defer setupImpactBuildSeam(t, cg)()

	res, err := handleImpact(context.Background(), ImpactInput{
		Repo: root, Symbol: "Foo", IncludeSnippets: true,
	}, impactLadderDeps(), nil, "")
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}
	text := impactResultText(t, res)
	callers := impactDirectCallers(t, text)
	if len(callers) != 20 {
		t.Fatalf("callers must survive snippet shedding, got %d:\n%s", len(callers), truncForLog(text, 500))
	}
	for _, c := range callers {
		if c.Snippet != "" {
			t.Fatalf("a shed response must carry no snippets, %s has one", c.Name)
		}
	}
}

// buildSnippetCallGraphN is buildSnippetCallGraph with n readable callers.
func buildSnippetCallGraphN(t *testing.T, root string, n int) *callgraph.CallGraph {
	t.Helper()
	target := &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: root + "/foo.go", StartLine: 1, EndLine: 5}
	cg := &callgraph.CallGraph{Symbols: []*parser.Symbol{target}, Tier: "basic"}
	for i := 0; i < n; i++ {
		file := fmt.Sprintf("%s/internal/service/handlers/caller_%03d.go", root, i)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		content := fmt.Sprintf("package p\n\nfunc Caller%03d() {\n\tFoo(arg%d, someOtherArgument, yetAnotherArgument)\n}\n", i, i)
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		c := &parser.Symbol{Name: fmt.Sprintf("Caller%03d", i), Kind: parser.KindFunction, File: file, StartLine: 3, EndLine: 5}
		cg.Symbols = append(cg.Symbols, c)
		cg.Edges = append(cg.Edges, callgraph.CallEdge{Caller: c, Callee: target, CalleeName: "Foo", Line: 4})
	}
	return cg
}

// The hotspot set is recomputed on every call (churn/snapshot share a 15 s
// deadline) and can differ between page one and page two. It must reorder a
// page, never move the boundary.
//
// RED-on-mutation: in windowDirectCallers partition before pageDirectCallers
// (partitionByHotspot(callers, …) then cut); pages then overlap and leave gaps.
func TestImpact_Paging_HotspotSetCannotMovePageBoundary(t *testing.T) {
	root := "/repo"
	mk := func() []impact.AffectedSymbol {
		out := make([]impact.AffectedSymbol, 150)
		for i := range out {
			out[i] = impact.AffectedSymbol{Name: fmt.Sprintf("C%03d", i), File: fmt.Sprintf("%s/f%03d.go", root, i)}
		}
		return out
	}
	// Page one sees three hotspot files that sort late; page two sees none.
	hot := map[string]bool{"f140.go": true, "f141.go": true, "f142.go": true}
	page1, _ := windowDirectCallers(mk(), ImpactInput{}, root, hot, "enhanced")
	page2, _ := windowDirectCallers(mk(), ImpactInput{Offset: 100}, root, nil, "enhanced")

	seen := map[string]bool{}
	for _, c := range append(page1, page2...) {
		if seen[c.Name] {
			t.Fatalf("caller %s appears on both pages", c.Name)
		}
		seen[c.Name] = true
	}
	if len(seen) != 150 {
		t.Fatalf("pages must cover all 150 callers, got %d", len(seen))
	}
}

// The tier caveat appears off the enhanced tier only, and a page after the
// first says how many snippets are missing in the handler note.
func TestDirectCallersTruncationNote_TierCaveat(t *testing.T) {
	tail := []impact.AffectedSymbol{{Name: "X"}}
	if n := directCallersTruncationNote(100, 101, 0, 100, tail, "r", "S", "basic"); !strings.Contains(n, "stable within one tier only") {
		t.Fatalf("basic tier must carry the caveat:\n%s", n)
	}
	if n := directCallersTruncationNote(100, 101, 0, 100, tail, "r", "S", "enhanced"); strings.Contains(n, "stable within one tier") {
		t.Fatalf("enhanced tier must not carry the caveat:\n%s", n)
	}
}

func TestImpact_IncludeSnippets_MissNoteCountsCallersWithoutSnippet(t *testing.T) {
	root := t.TempDir()
	defer setupImpactBuildSeam(t, buildSnippetCallGraph(t, root))() // 2 readable + CallerGone
	res, err := handleImpact(context.Background(), ImpactInput{
		Repo: root, Symbol: "Foo", IncludeSnippets: true, MaxBytes: 4_000_000,
	}, impactLadderDeps(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if text := impactResultText(t, res); !strings.Contains(text, "1 of 3 listed direct callers have no snippet") {
		t.Fatalf("miss note missing:\n%s", truncForLog(text, 600))
	}
}
