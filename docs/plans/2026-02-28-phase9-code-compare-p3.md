# code_compare P3 — AST Diff + Import Categorization

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add structural AST diff for modified symbol pairs (using smacker/gum GumTree algorithm) and import categorization (stdlib/internal/external) to `code_compare`, so comparisons show *what specifically changed* in modified symbols and how dependency strategies differ.

**Architecture:** AST diff re-parses symbol bodies with tree-sitter, converts to `gum.Tree`, runs GumTree Match+Patch to produce an edit script (Insert/Delete/Update/Move operations), then summarizes into a human-readable `DiffSummary`. Import categorization classifies imports by origin (stdlib/internal/external) using language-specific heuristics. Both new data types feed into `CompareResult` and the LLM context.

**Tech Stack:** Go 1.26+, `github.com/smacker/gum` (GumTree AST diff), `github.com/smacker/go-tree-sitter` (already used)

---

### Task 1: Add smacker/gum dependency + multi-language ToTree converter

**Files:**
- Modify: `/home/krolik/src/go-code/go.mod` (add gum dependency)
- Create: `/home/krolik/src/go-code/internal/compare/astconv.go`
- Create: `/home/krolik/src/go-code/internal/compare/astconv_test.go`

**Step 1: Add the gum dependency**

Run: `cd /home/krolik/src/go-code && go get github.com/smacker/gum`

If this fails due to old go.mod in gum (it was last updated 2019), the fallback is to vendor the core algorithm. The core `gum` package is ~500 lines with no external dependencies. If vendoring is needed:
1. Copy `gum.go` and `tree.go` from github.com/smacker/gum into `internal/compare/gum/`
2. Adjust package name to `gum`
3. Remove any test-only dependencies

**Step 2: Write the failing test**

Create `/home/krolik/src/go-code/internal/compare/astconv_test.go`:

```go
package compare

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/gum"
)

func TestToGumTree_Go(t *testing.T) {
	src := []byte(`package p
func Foo(x int) error { return nil }
`)
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	tree, err := parser.ParseCtx(nil, nil, src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	gt := ToGumTree(tree.RootNode(), src)
	if gt == nil {
		t.Fatal("ToGumTree returned nil")
	}
	if gt.Type != "source_file" {
		t.Errorf("root type = %q, want source_file", gt.Type)
	}
	if len(gt.Children) == 0 {
		t.Error("expected children in gum tree")
	}

	// Verify leaf nodes have values.
	leaves := collectLeaves(gt)
	hasIdentifier := false
	for _, l := range leaves {
		if l.Type == "identifier" && l.Value == "Foo" {
			hasIdentifier = true
		}
	}
	if !hasIdentifier {
		t.Error("expected leaf with identifier 'Foo'")
	}
}

func TestToGumTree_RoundTrip(t *testing.T) {
	// Two slightly different Go functions should produce matchable trees.
	srcA := []byte(`package p
func Foo(x int) error { return nil }
`)
	srcB := []byte(`package p
func Foo(x int, y string) error { return nil }
`)
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	treeA, _ := parser.ParseCtx(nil, nil, srcA)
	defer treeA.Close()
	treeB, _ := parser.ParseCtx(nil, nil, srcB)
	defer treeB.Close()

	gtA := ToGumTree(treeA.RootNode(), srcA)
	gtB := ToGumTree(treeB.RootNode(), srcB)

	mappings := gum.Match(gtA, gtB)
	if len(mappings) == 0 {
		t.Error("expected non-empty mappings between similar trees")
	}

	actions := gum.Patch(gtA, gtB, mappings)
	if len(actions) == 0 {
		t.Error("expected edit actions for modified function")
	}
}

func collectLeaves(t *gum.Tree) []*gum.Tree {
	if len(t.Children) == 0 {
		return []*gum.Tree{t}
	}
	var out []*gum.Tree
	for _, c := range t.Children {
		out = append(out, collectLeaves(c)...)
	}
	return out
}
```

**Step 3: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestToGumTree -v`
Expected: FAIL — `ToGumTree` undefined

**Step 4: Implement astconv.go**

Create `/home/krolik/src/go-code/internal/compare/astconv.go`:

```go
package compare

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/gum"
)

// ToGumTree converts a tree-sitter node tree into a gum.Tree suitable for
// GumTree AST diffing. It processes only named children (skipping anonymous
// syntax like punctuation) and sets Value on leaf nodes.
//
// The resulting tree has Refresh() called, making it ready for gum.Match().
func ToGumTree(node *sitter.Node, source []byte) *gum.Tree {
	t := toGumTreeRec(node, source)
	t.Refresh()
	return t
}

func toGumTreeRec(node *sitter.Node, source []byte) *gum.Tree {
	var children []*gum.Tree
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil {
			children = append(children, toGumTreeRec(child, source))
		}
	}

	var value string
	// Leaf named nodes (no named children) carry their source text as value.
	// This includes identifiers, literals, comments, keywords, etc.
	if node.NamedChildCount() == 0 {
		value = string(source[node.StartByte():node.EndByte()])
	}

	return &gum.Tree{
		Type:     node.Type(),
		Value:    value,
		Children: children,
	}
}
```

**Design note:** Instead of maintaining per-language token type maps (like the original tsitter adapter does for Go only), we set `Value` on ALL leaf named nodes. This is language-agnostic and works because tree-sitter's named nodes at the leaf level are inherently value-bearing (identifiers, literals, comments). Anonymous nodes (punctuation, keywords like `func`/`if`) are already excluded by using `NamedChildCount`/`NamedChild`.

**Step 5: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestToGumTree -v`
Expected: PASS

**Step 6: Run all tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

**Step 7: Commit**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git add go.mod go.sum internal/compare/astconv.go internal/compare/astconv_test.go
sudo -u krolik git commit -m "feat(compare): add gum dependency and ToGumTree converter

Multi-language tree-sitter to gum.Tree conversion for AST diffing.
Sets Value on all leaf named nodes (language-agnostic).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: DiffSummary type + ComputeASTDiff function

**Files:**
- Create: `/home/krolik/src/go-code/internal/compare/astdiff.go`
- Create: `/home/krolik/src/go-code/internal/compare/astdiff_test.go`

**Step 1: Write the failing test**

Create `/home/krolik/src/go-code/internal/compare/astdiff_test.go`:

```go
package compare

import (
	"testing"
)

func TestComputeASTDiff_Modified(t *testing.T) {
	bodyA := `func Foo(x int) error {
	return nil
}`
	bodyB := `func Foo(x int, y string) (int, error) {
	if y == "" {
		return 0, nil
	}
	return len(y), nil
}`
	diff := ComputeASTDiff(bodyA, bodyB, "go")
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if diff.TotalChanges == 0 {
		t.Error("expected TotalChanges > 0")
	}
	if len(diff.Changes) == 0 {
		t.Error("expected at least one change description")
	}
	t.Logf("diff: %+v", diff)
}

func TestComputeASTDiff_Identical(t *testing.T) {
	body := `func Foo(x int) error {
	return nil
}`
	diff := ComputeASTDiff(body, body, "go")
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if diff.TotalChanges != 0 {
		t.Errorf("expected TotalChanges = 0 for identical bodies, got %d", diff.TotalChanges)
	}
}

func TestComputeASTDiff_UnsupportedLanguage(t *testing.T) {
	diff := ComputeASTDiff("code", "code2", "brainfuck")
	if diff != nil {
		t.Error("expected nil for unsupported language")
	}
}

func TestComputeASTDiff_EmptyBody(t *testing.T) {
	diff := ComputeASTDiff("", "func Foo() {}", "go")
	if diff != nil {
		t.Error("expected nil for empty body")
	}
}

func TestComputeASTDiff_Python(t *testing.T) {
	bodyA := `def foo(x):
    return x + 1`
	bodyB := `def foo(x, y=0):
    return x + y + 1`
	diff := ComputeASTDiff(bodyA, bodyB, "python")
	if diff == nil {
		t.Fatal("expected non-nil diff for python")
	}
	if diff.TotalChanges == 0 {
		t.Error("expected changes for modified python function")
	}
	t.Logf("python diff: %+v", diff)
}

func TestSummarizeActions_Empty(t *testing.T) {
	changes := summarizeActions(nil, "go")
	if len(changes) != 0 {
		t.Errorf("expected empty changes for nil actions, got %d", len(changes))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestComputeASTDiff -v`
Expected: FAIL — `ComputeASTDiff` undefined

**Step 3: Implement astdiff.go**

Create `/home/krolik/src/go-code/internal/compare/astdiff.go`:

```go
package compare

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/gum"
)

// DiffSummary describes structural changes between two symbol bodies.
type DiffSummary struct {
	// TotalChanges is the total number of edit operations.
	TotalChanges int `json:"totalChanges"`

	// Inserts is the count of Insert/InsertTree operations.
	Inserts int `json:"inserts"`

	// Deletes is the count of Delete/DeleteTree operations.
	Deletes int `json:"deletes"`

	// Updates is the count of Update operations (value changes).
	Updates int `json:"updates"`

	// Moves is the count of Move operations.
	Moves int `json:"moves"`

	// Changes is a list of human-readable change descriptions (max 5).
	Changes []string `json:"changes,omitempty"`
}

// maxDiffChanges limits the number of human-readable change descriptions.
const maxDiffChanges = 5

// maxDiffBodyLen limits the body length for AST diffing (skip huge functions).
const maxDiffBodyLen = 10_000

// ComputeASTDiff computes a structural diff between two symbol bodies.
// Returns nil if the language is unsupported, bodies are empty, or parsing fails.
func ComputeASTDiff(bodyA, bodyB, language string) *DiffSummary {
	if bodyA == "" || bodyB == "" {
		return nil
	}
	if len(bodyA) > maxDiffBodyLen || len(bodyB) > maxDiffBodyLen {
		return nil
	}

	lang := lookupLanguage(language)
	if lang == nil {
		return nil
	}

	srcA := []byte(bodyA)
	srcB := []byte(bodyB)

	p := sitter.NewParser()
	p.SetLanguage(lang)

	treeA, err := p.ParseCtx(nil, nil, srcA)
	if err != nil || treeA == nil {
		return nil
	}
	defer treeA.Close()

	treeB, err := p.ParseCtx(nil, nil, srcB)
	if err != nil || treeB == nil {
		return nil
	}
	defer treeB.Close()

	gtA := ToGumTree(treeA.RootNode(), srcA)
	gtB := ToGumTree(treeB.RootNode(), srcB)

	mappings := gum.Match(gtA, gtB)
	actions := gum.Patch(gtA, gtB, mappings)

	summary := &DiffSummary{
		TotalChanges: len(actions),
	}

	for _, a := range actions {
		switch a.Type {
		case gum.Insert, gum.InsertTree:
			summary.Inserts++
		case gum.Delete, gum.DeleteTree:
			summary.Deletes++
		case gum.Update:
			summary.Updates++
		case gum.Move:
			summary.Moves++
		}
	}

	summary.Changes = summarizeActions(actions, language)

	return summary
}

// lookupLanguage returns the tree-sitter language for a given language name.
func lookupLanguage(language string) *sitter.Language {
	switch strings.ToLower(language) {
	case "go":
		return golang.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "javascript", "typescript":
		return javascript.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "c":
		return c.GetLanguage()
	case "cpp", "c++":
		return cpp.GetLanguage()
	case "ruby":
		return ruby.GetLanguage()
	case "csharp", "c#":
		return csharp.GetLanguage()
	default:
		return nil
	}
}

// summarizeActions produces human-readable descriptions of the most significant changes.
func summarizeActions(actions []gum.Action, _ string) []string {
	if len(actions) == 0 {
		return nil
	}

	var changes []string

	for _, a := range actions {
		if len(changes) >= maxDiffChanges {
			break
		}
		desc := describeAction(a)
		if desc != "" {
			changes = append(changes, desc)
		}
	}

	return changes
}

// describeAction creates a human-readable description of a single edit action.
func describeAction(a gum.Action) string {
	nodeDesc := nodeDescription(a.Node)
	switch a.Type {
	case gum.InsertTree:
		if a.Parent != nil {
			return fmt.Sprintf("added %s in %s", nodeDesc, a.Parent.Type)
		}
		return fmt.Sprintf("added %s", nodeDesc)
	case gum.Insert:
		if a.Parent != nil {
			return fmt.Sprintf("added %s in %s", nodeDesc, a.Parent.Type)
		}
		return fmt.Sprintf("added %s", nodeDesc)
	case gum.DeleteTree:
		return fmt.Sprintf("removed %s", nodeDesc)
	case gum.Delete:
		return fmt.Sprintf("removed %s", nodeDesc)
	case gum.Update:
		return fmt.Sprintf("changed %s: %q -> %q", a.Node.Type, truncateValue(a.Node.Value), truncateValue(a.Value))
	case gum.Move:
		if a.Parent != nil {
			return fmt.Sprintf("moved %s to %s", nodeDesc, a.Parent.Type)
		}
		return fmt.Sprintf("moved %s", nodeDesc)
	default:
		return ""
	}
}

// nodeDescription returns a human-readable description of a tree node.
func nodeDescription(t *gum.Tree) string {
	if t == nil {
		return "unknown"
	}
	if t.Value != "" {
		return fmt.Sprintf("%s %q", t.Type, truncateValue(t.Value))
	}
	return t.Type
}

// truncateValue shortens a value for display (max 40 chars).
func truncateValue(s string) string {
	const maxValLen = 40
	if len(s) <= maxValLen {
		return s
	}
	return s[:maxValLen-3] + "..."
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestComputeASTDiff -v`
Expected: All PASS

**Step 5: Run all tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git add internal/compare/astdiff.go internal/compare/astdiff_test.go
sudo -u krolik git commit -m "feat(compare): add AST diff for modified symbol pairs

GumTree-based structural diff: re-parses symbol bodies with tree-sitter,
computes edit script (Insert/Delete/Update/Move), generates human-readable
change summaries. Supports all 9 languages.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Wire AST diff into SymbolMatch + CompareRepos

**Files:**
- Modify: `/home/krolik/src/go-code/internal/compare/compare.go:44-52` (add DiffSummary to SymbolMatch)
- Modify: `/home/krolik/src/go-code/internal/compare/compare.go:217-264` (compute AST diff for modified matches)
- Modify: `/home/krolik/src/go-code/internal/compare/compare_test.go`

**Step 1: Write the failing test**

Add to `/home/krolik/src/go-code/internal/compare/compare_test.go`:

```go
func TestAnnotateASTDiffs(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Foo(x int) error {\n\treturn nil\n}",
			},
			SymbolB: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Foo(x int, y string) (int, error) {\n\treturn 0, nil\n}",
			},
			MatchType: MatchModified,
			Score:     1.0,
		},
		{
			SymbolA: &parser.Symbol{
				Name: "Bar", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Bar() {}",
			},
			SymbolB: &parser.Symbol{
				Name: "Bar", Kind: parser.KindFunction,
				Language: "go",
				Body:     "func Bar() {}",
			},
			MatchType: MatchExact,
			Score:     1.0,
		},
	}

	annotateASTDiffs(matches)

	// Modified match should have a diff.
	if matches[0].Diff == nil {
		t.Error("expected Diff on modified match")
	}
	if matches[0].Diff.TotalChanges == 0 {
		t.Error("expected TotalChanges > 0 on modified match")
	}

	// Exact match should NOT have a diff.
	if matches[1].Diff != nil {
		t.Error("expected nil Diff on exact match")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestAnnotateASTDiffs -v`
Expected: FAIL — `annotateASTDiffs` undefined, `Diff` field doesn't exist

**Step 3: Add Diff field to SymbolMatch**

In `/home/krolik/src/go-code/internal/compare/compare.go`, add to `SymbolMatch`:

```go
type SymbolMatch struct {
	SymbolA   *parser.Symbol `json:"symbolA,omitempty"`
	SymbolB   *parser.Symbol `json:"symbolB,omitempty"`
	MatchType MatchType      `json:"matchType"`
	Category  string         `json:"category"`
	Score     float64        `json:"score"`
	Diff      *DiffSummary   `json:"diff,omitempty"`
}
```

**Step 4: Add annotateASTDiffs and DiffStats**

In `/home/krolik/src/go-code/internal/compare/compare.go`:

```go
// annotateASTDiffs computes AST diffs for modified symbol matches.
func annotateASTDiffs(matches []SymbolMatch) {
	for i := range matches {
		m := &matches[i]
		if m.MatchType != MatchModified || m.SymbolA == nil || m.SymbolB == nil {
			continue
		}
		lang := m.SymbolA.Language
		if lang == "" {
			lang = m.SymbolB.Language
		}
		m.Diff = ComputeASTDiff(m.SymbolA.Body, m.SymbolB.Body, lang)
	}
}

// DiffStats aggregates AST diff statistics across all modified matches.
type DiffStats struct {
	ModifiedWithDiff int `json:"modifiedWithDiff"`
	TotalInserts     int `json:"totalInserts"`
	TotalDeletes     int `json:"totalDeletes"`
	TotalUpdates     int `json:"totalUpdates"`
	TotalMoves       int `json:"totalMoves"`
}

// computeDiffStats aggregates diff statistics from annotated matches.
func computeDiffStats(matches []SymbolMatch) *DiffStats {
	stats := &DiffStats{}
	for _, m := range matches {
		if m.Diff == nil {
			continue
		}
		stats.ModifiedWithDiff++
		stats.TotalInserts += m.Diff.Inserts
		stats.TotalDeletes += m.Diff.Deletes
		stats.TotalUpdates += m.Diff.Updates
		stats.TotalMoves += m.Diff.Moves
	}
	if stats.ModifiedWithDiff == 0 {
		return nil
	}
	return stats
}
```

Add `DiffStats` field to `CompareResult`:

```go
type CompareResult struct {
	// ... existing fields ...
	HotspotsA     []HotspotFile  `json:"hotspots_a,omitempty"`
	HotspotsB     []HotspotFile  `json:"hotspots_b,omitempty"`
	DiffStats     *DiffStats     `json:"diff_stats,omitempty"`
}
```

In `CompareRepos`, after `matches := MatchSymbols(...)`, add:

```go
	// Annotate modified matches with AST diffs.
	annotateASTDiffs(matches)
```

After the match counting loop, add:

```go
	diffStats := computeDiffStats(matches)
```

Include `DiffStats: diffStats` in the result struct.

**Step 5: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestAnnotateASTDiffs -v`
Expected: PASS

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git add internal/compare/compare.go internal/compare/compare_test.go
sudo -u krolik git commit -m "feat(compare): wire AST diff into SymbolMatch and CompareResult

Modified matches now carry DiffSummary with edit operation counts and
human-readable change descriptions. DiffStats aggregates across all
modified symbols.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Wire AST diff into LLM context

**Files:**
- Modify: `/home/krolik/src/go-code/internal/compare/context.go:151-166` (writePair — include diff summary)
- Modify: `/home/krolik/src/go-code/internal/compare/context_test.go`

**Step 1: Write the failing test**

Add to `/home/krolik/src/go-code/internal/compare/context_test.go`:

```go
func TestBuildCompareContext_IncludesDiffSummary(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction, File: "a.go",
				Body: "func Foo(x int) {}",
			},
			SymbolB: &parser.Symbol{
				Name: "Foo", Kind: parser.KindFunction, File: "b.go",
				Body: "func Foo(x int, y string) {}",
			},
			MatchType: MatchModified,
			Score:     1.0,
			Category:  "function",
			Diff: &DiffSummary{
				TotalChanges: 3,
				Inserts:      2,
				Updates:      1,
				Changes: []string{
					"added parameter_declaration in parameter_list",
					"changed identifier: \"int\" -> \"string\"",
				},
			},
		},
	}

	ctx := BuildCompareContextV2(matches, RepoMetrics{}, RepoMetrics{}, "test", nil, nil)

	if !strings.Contains(ctx, "Structural changes") {
		t.Error("context should contain 'Structural changes' section for modified pairs with diff")
	}
	if !strings.Contains(ctx, "added parameter_declaration") {
		t.Error("context should contain diff change description")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext_IncludesDiffSummary -v`
Expected: FAIL — no "Structural changes" in output

**Step 3: Update writePair to include diff summary**

In `/home/krolik/src/go-code/internal/compare/context.go`, modify the `writePair` function to add a diff section at the end:

```go
func writePair(sb *strings.Builder, m *SymbolMatch) {
	fmt.Fprintf(sb, "### %s `%s` (match: %s, score: %.2f, category: %s)\n\n",
		m.SymbolA.Kind, m.SymbolA.Name, m.MatchType, m.Score, m.Category)

	sb.WriteString("**Repo A** (`")
	sb.WriteString(m.SymbolA.File)
	sb.WriteString("`):\n```\n")
	sb.WriteString(truncate(m.SymbolA.Body, maxSnippetChars))
	sb.WriteString("\n```\n\n")

	sb.WriteString("**Repo B** (`")
	sb.WriteString(m.SymbolB.File)
	sb.WriteString("`):\n```\n")
	sb.WriteString(truncate(m.SymbolB.Body, maxSnippetChars))
	sb.WriteString("\n```\n\n")

	// Include AST diff summary for modified symbols.
	if m.Diff != nil && m.Diff.TotalChanges > 0 {
		writeDiffSummary(sb, m.Diff)
	}
}

func writeDiffSummary(sb *strings.Builder, diff *DiffSummary) {
	fmt.Fprintf(sb, "**Structural changes** (%d total: +%d -%d ~%d move:%d):\n",
		diff.TotalChanges, diff.Inserts, diff.Deletes, diff.Updates, diff.Moves)
	for _, c := range diff.Changes {
		sb.WriteString("- ")
		sb.WriteString(c)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext_IncludesDiffSummary -v`
Expected: PASS

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git add internal/compare/context.go internal/compare/context_test.go
sudo -u krolik git commit -m "feat(compare): show AST diff in LLM context for modified pairs

writePair now includes a Structural changes section when a modified
match has a DiffSummary, showing edit operation counts and change
descriptions.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Import categorization

**Files:**
- Create: `/home/krolik/src/go-code/internal/compare/importcat.go`
- Create: `/home/krolik/src/go-code/internal/compare/importcat_test.go`
- Modify: `/home/krolik/src/go-code/internal/compare/importdiff.go` (add categories to ImportDiff)
- Modify: `/home/krolik/src/go-code/internal/compare/importdiff_test.go`
- Modify: `/home/krolik/src/go-code/internal/compare/compare.go` (pass language to ComputeImportDiff)

**Step 1: Write the failing test**

Create `/home/krolik/src/go-code/internal/compare/importcat_test.go`:

```go
package compare

import (
	"testing"
)

func TestCategorizeImport_Go(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		{"fmt", ImportStdlib},
		{"net/http", ImportStdlib},
		{"os/exec", ImportStdlib},
		{"github.com/gin-gonic/gin", ImportExternal},
		{"github.com/myorg/mylib", ImportExternal},
		{"golang.org/x/tools", ImportExternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "go")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, go) = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestCategorizeImport_Python(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		{"os", ImportStdlib},
		{"sys", ImportStdlib},
		{"json", ImportStdlib},
		{"pathlib", ImportStdlib},
		{"requests", ImportExternal},
		{"flask", ImportExternal},
		{"numpy", ImportExternal},
		{".utils", ImportInternal},
		{"..models", ImportInternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "python")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, python) = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestCategorizeImport_JavaScript(t *testing.T) {
	tests := []struct {
		imp  string
		want ImportCategory
	}{
		{"fs", ImportStdlib},
		{"path", ImportStdlib},
		{"http", ImportStdlib},
		{"react", ImportExternal},
		{"express", ImportExternal},
		{"./utils", ImportInternal},
		{"../models", ImportInternal},
		{"@org/package", ImportExternal},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := CategorizeImport(tt.imp, "javascript")
			if got != tt.want {
				t.Errorf("CategorizeImport(%q, javascript) = %q, want %q", tt.imp, got, tt.want)
			}
		})
	}
}

func TestDetectFrameworks(t *testing.T) {
	imports := []string{
		"github.com/gin-gonic/gin",
		"github.com/go-redis/redis/v9",
		"gorm.io/gorm",
		"fmt",
		"net/http",
	}

	frameworks := DetectFrameworks(imports, "go")
	if len(frameworks) == 0 {
		t.Error("expected detected frameworks")
	}

	found := false
	for _, f := range frameworks {
		if f == "gin" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'gin' in detected frameworks")
	}
	t.Logf("frameworks: %v", frameworks)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run "TestCategorizeImport|TestDetectFrameworks" -v`
Expected: FAIL — `CategorizeImport` undefined

**Step 3: Implement importcat.go**

Create `/home/krolik/src/go-code/internal/compare/importcat.go`:

```go
package compare

import (
	"sort"
	"strings"
)

// ImportCategory classifies an import's origin.
type ImportCategory string

const (
	ImportStdlib   ImportCategory = "stdlib"
	ImportInternal ImportCategory = "internal"
	ImportExternal ImportCategory = "external"
)

// CategorizeImport determines whether an import is stdlib, internal, or external.
func CategorizeImport(imp, language string) ImportCategory {
	switch strings.ToLower(language) {
	case "go":
		return categorizeGoImport(imp)
	case "python":
		return categorizePythonImport(imp)
	case "javascript", "typescript":
		return categorizeJSImport(imp)
	default:
		return ImportExternal
	}
}

func categorizeGoImport(imp string) ImportCategory {
	// Go stdlib imports have no dots in the path.
	if !strings.Contains(imp, ".") {
		return ImportStdlib
	}
	return ImportExternal
}

func categorizePythonImport(imp string) ImportCategory {
	// Relative imports start with dots.
	if strings.HasPrefix(imp, ".") {
		return ImportInternal
	}
	if pythonStdlib[imp] {
		return ImportStdlib
	}
	// Check top-level module (e.g. "os.path" -> "os").
	top := strings.SplitN(imp, ".", 2)[0]
	if pythonStdlib[top] {
		return ImportStdlib
	}
	return ImportExternal
}

func categorizeJSImport(imp string) ImportCategory {
	// Relative imports.
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return ImportInternal
	}
	if nodeBuiltins[imp] {
		return ImportStdlib
	}
	return ImportExternal
}

// pythonStdlib is a set of common Python stdlib top-level modules.
var pythonStdlib = map[string]bool{
	"abc": true, "argparse": true, "ast": true, "asyncio": true,
	"base64": true, "bisect": true, "builtins": true,
	"calendar": true, "codecs": true, "collections": true, "contextlib": true,
	"copy": true, "csv": true, "ctypes": true,
	"dataclasses": true, "datetime": true, "decimal": true, "difflib": true,
	"email": true, "enum": true, "errno": true,
	"fnmatch": true, "fractions": true, "functools": true,
	"getpass": true, "glob": true, "gzip": true,
	"hashlib": true, "heapq": true, "hmac": true, "html": true, "http": true,
	"importlib": true, "inspect": true, "io": true, "ipaddress": true,
	"itertools": true, "json": true, "keyword": true,
	"logging": true, "lzma": true,
	"math": true, "mimetypes": true, "multiprocessing": true,
	"operator": true, "os": true,
	"pathlib": true, "pickle": true, "platform": true, "pprint": true,
	"queue": true, "random": true, "re": true,
	"secrets": true, "select": true, "shelve": true, "shlex": true,
	"shutil": true, "signal": true, "site": true, "socket": true,
	"sqlite3": true, "ssl": true, "stat": true, "statistics": true,
	"string": true, "struct": true, "subprocess": true, "sys": true,
	"tarfile": true, "tempfile": true, "textwrap": true, "threading": true,
	"time": true, "timeit": true, "token": true, "tokenize": true,
	"traceback": true, "types": true, "typing": true,
	"unicodedata": true, "unittest": true, "urllib": true, "uuid": true,
	"venv": true, "warnings": true, "weakref": true,
	"xml": true, "xmlrpc": true, "zipfile": true, "zipimport": true, "zlib": true,
}

// nodeBuiltins is a set of Node.js built-in modules.
var nodeBuiltins = map[string]bool{
	"assert": true, "buffer": true, "child_process": true, "cluster": true,
	"console": true, "constants": true, "crypto": true,
	"dgram": true, "dns": true, "domain": true, "events": true,
	"fs": true, "http": true, "https": true, "module": true,
	"net": true, "os": true, "path": true, "perf_hooks": true,
	"process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"timers": true, "tls": true, "tty": true, "url": true,
	"util": true, "v8": true, "vm": true, "worker_threads": true, "zlib": true,
}

// frameworkPatterns maps known framework import patterns to their common names.
// Format: "import_prefix:framework_name"
var frameworkPatterns = map[string][]string{
	"go": {
		"github.com/gin-gonic/gin:gin",
		"github.com/labstack/echo:echo",
		"github.com/gofiber/fiber:fiber",
		"github.com/gorilla/mux:gorilla",
		"google.golang.org/grpc:grpc",
		"gorm.io/gorm:gorm",
		"github.com/jmoiron/sqlx:sqlx",
		"github.com/go-redis/redis:redis",
		"go.uber.org/zap:zap",
		"github.com/sirupsen/logrus:logrus",
		"github.com/stretchr/testify:testify",
		"github.com/spf13/cobra:cobra",
		"github.com/spf13/viper:viper",
	},
	"python": {
		"flask:flask",
		"django:django",
		"fastapi:fastapi",
		"requests:requests",
		"sqlalchemy:sqlalchemy",
		"celery:celery",
		"pytest:pytest",
		"numpy:numpy",
		"pandas:pandas",
		"torch:pytorch",
		"tensorflow:tensorflow",
	},
	"javascript": {
		"react:react",
		"express:express",
		"next:next.js",
		"vue:vue",
		"angular:angular",
		"axios:axios",
		"lodash:lodash",
		"jest:jest",
		"mocha:mocha",
	},
}

// DetectFrameworks identifies known frameworks from the import list.
func DetectFrameworks(imports []string, language string) []string {
	patterns, ok := frameworkPatterns[strings.ToLower(language)]
	if !ok {
		return nil
	}

	seen := make(map[string]bool)
	for _, imp := range imports {
		for _, pattern := range patterns {
			parts := strings.SplitN(pattern, ":", 2)
			prefix, name := parts[0], parts[1]
			if strings.HasPrefix(imp, prefix) && !seen[name] {
				seen[name] = true
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -run "TestCategorizeImport|TestDetectFrameworks" -v`
Expected: All PASS

**Step 5: Add categories to ImportDiff**

In `/home/krolik/src/go-code/internal/compare/importdiff.go`, add fields to `ImportDiff`:

```go
type ImportDiff struct {
	CommonCount int      `json:"commonCount"`
	OnlyACount  int      `json:"onlyACount"`
	OnlyBCount  int      `json:"onlyBCount"`
	OnlyA       []string `json:"onlyA,omitempty"`
	OnlyB       []string `json:"onlyB,omitempty"`
	FrameworksA []string `json:"frameworksA,omitempty"`
	FrameworksB []string `json:"frameworksB,omitempty"`
	StdlibA     int      `json:"stdlibA"`
	StdlibB     int      `json:"stdlibB"`
	ExternalA   int      `json:"externalA"`
	ExternalB   int      `json:"externalB"`
}
```

Change `ComputeImportDiff` signature to accept language:

```go
func ComputeImportDiff(importsA, importsB []string, language string) ImportDiff {
```

Add category counting and framework detection before the return statement:

```go
	var stdlibA, externalA, stdlibB, externalB int
	for imp := range setA {
		switch CategorizeImport(imp, language) {
		case ImportStdlib:
			stdlibA++
		case ImportExternal:
			externalA++
		}
	}
	for imp := range setB {
		switch CategorizeImport(imp, language) {
		case ImportStdlib:
			stdlibB++
		case ImportExternal:
			externalB++
		}
	}

	return ImportDiff{
		CommonCount: common,
		OnlyACount:  onlyA,
		OnlyBCount:  onlyB,
		OnlyA:       onlyAList,
		OnlyB:       onlyBList,
		FrameworksA: DetectFrameworks(importsA, language),
		FrameworksB: DetectFrameworks(importsB, language),
		StdlibA:     stdlibA,
		StdlibB:     stdlibB,
		ExternalA:   externalA,
		ExternalB:   externalB,
	}
```

**Step 6: Update callers**

In `/home/krolik/src/go-code/internal/compare/compare.go`, update:

```go
	importDiff := ComputeImportDiff(snapA.Imports, snapB.Imports, snapA.Language)
```

**Step 7: Update existing tests**

In `/home/krolik/src/go-code/internal/compare/importdiff_test.go`, update all `ComputeImportDiff` calls to pass `"go"` as the third argument:

```go
ComputeImportDiff(importsA, importsB, "go")
ComputeImportDiff(nil, nil, "go")
ComputeImportDiff(imports, imports, "go")
```

Add a new test:

```go
func TestComputeImportDiff_WithCategories(t *testing.T) {
	importsA := []string{"fmt", "net/http", "github.com/gin-gonic/gin"}
	importsB := []string{"fmt", "os", "github.com/labstack/echo"}

	diff := ComputeImportDiff(importsA, importsB, "go")

	if diff.StdlibA != 2 {
		t.Errorf("StdlibA = %d, want 2", diff.StdlibA)
	}
	if diff.ExternalA != 1 {
		t.Errorf("ExternalA = %d, want 1", diff.ExternalA)
	}
	if len(diff.FrameworksA) == 0 || diff.FrameworksA[0] != "gin" {
		t.Errorf("FrameworksA = %v, want [gin]", diff.FrameworksA)
	}
	if len(diff.FrameworksB) == 0 || diff.FrameworksB[0] != "echo" {
		t.Errorf("FrameworksB = %v, want [echo]", diff.FrameworksB)
	}
}
```

**Step 8: Run all tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

Run: `cd /home/krolik/src/go-code && go test ./... -count=1`
Expected: All PASS

**Step 9: Commit**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git add internal/compare/importcat.go internal/compare/importcat_test.go internal/compare/importdiff.go internal/compare/importdiff_test.go internal/compare/compare.go
sudo -u krolik git commit -m "feat(compare): add import categorization and framework detection

Categorize imports as stdlib/internal/external for Go, Python, JS/TS.
Detect common frameworks (gin, flask, react, etc.) from import lists.
ImportDiff now includes per-repo stdlib/external counts and framework lists.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Lint + Full Test Pass + Deploy

**Files:** None new — validation only.

**Step 1: Run all tests**

Run: `cd /home/krolik/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

Run: `cd /home/krolik/src/go-code && go test ./... -count=1`
Expected: All PASS

**Step 2: Deploy**

```bash
cd ~/deploy/krolik-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Health check**

Run: `curl -s http://127.0.0.1:8897/health | head -1`
Expected: healthy response with version containing latest commit hash

**Step 4: Smoke test**

Use the MCP tool to compare two repos and verify new fields appear:
- `diff_stats` in the result (if any modified symbols exist)
- `frameworksA`/`frameworksB` in `import_diff`
- `stdlibA`/`stdlibB`/`externalA`/`externalB` in `import_diff`

**Step 5: Push to origin**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git push origin main
```

**Step 6: Commit any lint/build fixes if needed**

```bash
cd /home/krolik/src/go-code
sudo -u krolik git add -A
sudo -u krolik git commit -m "chore: lint fixes for code_compare P3

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Summary of Changes

| Task | What | Files | New LOC (approx) |
|------|------|-------|-------------------|
| 1 | gum dependency + ToGumTree converter | astconv.go (new) | +50 |
| 2 | AST diff computation | astdiff.go (new) | +160 |
| 3 | Wire into SymbolMatch + CompareResult | compare.go (mod) | +50 |
| 4 | Wire into LLM context | context.go (mod) | +25 |
| 5 | Import categorization | importcat.go (new), importdiff.go (mod) | +180 |
| 6 | Lint + test + deploy | - | - |
| **Total** | | **3 new files, 3 modified** | **~465 LOC** |

## Not in Scope (deferred to P4+)

- Identifier-level reference graph + personalized PageRank (separate feature track, not code_compare)
- INHERITS/IMPLEMENTS edges (needs parser changes)
- Semantic search via embeddings (needs vector DB integration)
- Cross-language HTTP boundaries (needs polyglot call graph)
- Impact/blast radius analysis (separate tool, not code_compare)
