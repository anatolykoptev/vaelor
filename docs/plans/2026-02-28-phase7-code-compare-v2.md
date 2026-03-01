# code_compare v2 — P0+P1 Improvements

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix critical bugs and add structural comparison features (body hash, signature matching, cyclomatic complexity, symbol prioritization) to make code_compare produce meaningful results without relying entirely on LLM.

**Architecture:** Add `BodyHash` field to `parser.Symbol` for content-based change detection. Extend `MatchSymbols` with signature-aware matching and body-hash comparison to distinguish "identical", "modified", and "renamed" symbols. Add cyclomatic complexity to `RepoMetrics`. Prioritize most-different symbols for LLM context. All changes are backward-compatible — no breaking changes to MCP tool schema.

**Tech Stack:** Go 1.24, tree-sitter (smacker/go-tree-sitter), xxhash (already in deps)

---

## Task 1: Add BodyHash to parser.Symbol

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/parser/parser.go:36-65`
- Modify: `/path/to/repos/src/go-code/internal/compare/snapshot.go:99-117`
- Test: `/path/to/repos/src/go-code/internal/compare/snapshot_test.go`

**Step 1: Write the failing test**

Add to `snapshot_test.go`:

```go
func TestBuildSnapshot_BodyHash(t *testing.T) {
	root := findRepoRoot(t)

	snap, err := compare.BuildSnapshot(context.Background(), root, compare.SnapshotOpts{
		Language: "go",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	hashSeen := false
	for _, sym := range snap.Symbols {
		if sym.Body != "" && sym.BodyHash == 0 {
			t.Errorf("symbol %q has body but BodyHash=0", sym.Name)
		}
		if sym.BodyHash != 0 {
			hashSeen = true
		}
	}
	if !hashSeen {
		t.Error("no symbols with BodyHash set — expected at least one")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestBuildSnapshot_BodyHash -v`
Expected: FAIL — `sym.BodyHash undefined (type *parser.Symbol has no field or method BodyHash)`

**Step 3: Add BodyHash field to parser.Symbol**

In `/path/to/repos/src/go-code/internal/parser/parser.go`, add after the `DocComment` field:

```go
	// BodyHash is a content hash of the normalized symbol body.
	// Used for fast equality checks in code comparison (0 means not computed).
	BodyHash uint64
```

**Step 4: Compute BodyHash in snapshot.go**

In `/path/to/repos/src/go-code/internal/compare/snapshot.go`, add import `"github.com/cespare/xxhash/v2"` and a helper function:

```go
// computeBodyHashes sets BodyHash on each symbol that has a non-empty Body.
func computeBodyHashes(symbols []*parser.Symbol) {
	for _, sym := range symbols {
		if sym.Body != "" {
			sym.BodyHash = xxhash.Sum64String(sym.Body)
		}
	}
}
```

Call it in `buildSnapshotResult` right before the return statement:

```go
	computeBodyHashes(allSymbols)

	return &RepoSnapshot{
```

**Step 5: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestBuildSnapshot_BodyHash -v`
Expected: PASS

**Step 6: Run all existing tests to check for regressions**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -v`
Expected: All PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/parser/parser.go internal/compare/snapshot.go internal/compare/snapshot_test.go
git commit -m "feat(compare): add BodyHash to parser.Symbol for content-based matching

Adds xxhash-based body hash to each parsed symbol. Computed during
snapshot building. Enables detecting identical vs modified symbols
without string comparison."
```

---

## Task 2: Use BodyHash in symbol matching — detect identical vs modified

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/compare/compare.go:20-35` (MatchType constants)
- Modify: `/path/to/repos/src/go-code/internal/compare/match.go:66-93` (matchExact)
- Test: `/path/to/repos/src/go-code/internal/compare/match_test.go`

**Step 1: Write the failing test**

Add to `match_test.go`:

```go
func TestMatchExact_DistinguishIdenticalFromModified(t *testing.T) {
	symbolsA := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, Body: "func Foo() { return 1 }", BodyHash: 111},
		{Name: "Bar", Kind: parser.KindFunction, Body: "func Bar() { return 2 }", BodyHash: 222},
	}
	symbolsB := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, Body: "func Foo() { return 1 }", BodyHash: 111}, // identical
		{Name: "Bar", Kind: parser.KindFunction, Body: "func Bar() { return 99 }", BodyHash: 333}, // modified
	}

	matches := MatchSymbols(symbolsA, symbolsB, nil)

	var identicalCount, modifiedCount int
	for _, m := range matches {
		switch m.MatchType {
		case MatchExact:
			identicalCount++
		case MatchModified:
			modifiedCount++
		}
	}

	if identicalCount != 1 {
		t.Errorf("identicalCount = %d, want 1", identicalCount)
	}
	if modifiedCount != 1 {
		t.Errorf("modifiedCount = %d, want 1", modifiedCount)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestMatchExact_DistinguishIdenticalFromModified -v`
Expected: FAIL — `MatchModified` undefined

**Step 3: Add MatchModified constant**

In `compare.go`, add after `MatchExact`:

```go
	// MatchModified means the symbols have the same name and kind but different body.
	MatchModified MatchType = "modified"
```

**Step 4: Update matchExact to use BodyHash**

In `match.go`, update the match creation inside `matchExact` (the block where `idx >= 0`):

```go
		if idx >= 0 {
			usedB[idx] = true
			mt := MatchExact
			if symA.BodyHash != 0 && b[idx].BodyHash != 0 && symA.BodyHash != b[idx].BodyHash {
				mt = MatchModified
			}
			matches = append(matches, SymbolMatch{
				SymbolA:   symA,
				SymbolB:   b[idx],
				MatchType: mt,
				Category:  string(symA.Kind),
				Score:     1.0,
			})
```

**Step 5: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestMatchExact_DistinguishIdenticalFromModified -v`
Expected: PASS

**Step 6: Run all match tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestMatch -v`
Expected: All PASS (existing tests still pass because BodyHash=0 means skip the check)

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/compare/compare.go internal/compare/match.go internal/compare/match_test.go
git commit -m "feat(compare): distinguish identical vs modified symbols via BodyHash

Exact matches now check BodyHash: if both symbols have hashes and they
differ, the match type is 'modified' instead of 'exact'. This lets
consumers see which matched symbols actually changed."
```

---

## Task 3: Add signature-based matching (catch renames)

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/compare/match.go`
- Test: `/path/to/repos/src/go-code/internal/compare/match_test.go`

**Step 1: Write the failing test**

Add to `match_test.go`:

```go
func TestMatchSignature_CatchRename(t *testing.T) {
	// Same signature + same body → should match even with different names
	symbolsA := []*parser.Symbol{
		{Name: "HandleRequest", Kind: parser.KindFunction, Signature: "func(ctx context.Context, req *Request) error", BodyHash: 555},
	}
	symbolsB := []*parser.Symbol{
		{Name: "ProcessRequest", Kind: parser.KindFunction, Signature: "func(ctx context.Context, req *Request) error", BodyHash: 555},
	}

	matches := MatchSymbols(symbolsA, symbolsB, nil)

	found := false
	for _, m := range matches {
		if m.SymbolA != nil && m.SymbolB != nil {
			found = true
			if m.MatchType != MatchRenamed {
				t.Errorf("MatchType = %q, want %q", m.MatchType, MatchRenamed)
			}
			if m.Score < 0.8 {
				t.Errorf("Score = %.2f, want >= 0.8", m.Score)
			}
		}
	}
	if !found {
		t.Error("expected renamed match, got none")
	}
}

func TestMatchSignature_DifferentSignature_NoMatch(t *testing.T) {
	symbolsA := []*parser.Symbol{
		{Name: "HandleRequest", Kind: parser.KindFunction, Signature: "func(ctx context.Context) error"},
	}
	symbolsB := []*parser.Symbol{
		{Name: "ProcessData", Kind: parser.KindFunction, Signature: "func(data []byte) (int, error)"},
	}

	matches := MatchSymbols(symbolsA, symbolsB, nil)

	for _, m := range matches {
		if m.SymbolA != nil && m.SymbolB != nil && m.MatchType == MatchRenamed {
			t.Error("should not match different signatures as renamed")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestMatchSignature -v`
Expected: FAIL — `MatchRenamed` undefined

**Step 3: Add MatchRenamed constant and signature matching pass**

In `compare.go`, add after `MatchModified`:

```go
	// MatchRenamed means the symbols have different names but same signature and/or body hash.
	MatchRenamed MatchType = "renamed"
```

In `match.go`, add after the `matchFuzzy` function:

```go
// signatureMatchThreshold is the minimum similarity for signature-based matching.
const signatureMatchThreshold = 0.85

// matchSignature matches symbols with identical or very similar signatures
// and the same body hash. Catches renames where name changed but code didn't.
func matchSignature(a, b []*parser.Symbol) (unmatchedA, unmatchedB []*parser.Symbol, matches []SymbolMatch) {
	usedB := make([]bool, len(b))

	for _, symA := range a {
		if symA.Signature == "" {
			unmatchedA = append(unmatchedA, symA)
			continue
		}

		idx, score := findSignatureMatch(symA, b, usedB)
		if idx >= 0 {
			usedB[idx] = true
			matches = append(matches, SymbolMatch{
				SymbolA:   symA,
				SymbolB:   b[idx],
				MatchType: MatchRenamed,
				Category:  string(symA.Kind),
				Score:     score,
			})
		} else {
			unmatchedA = append(unmatchedA, symA)
		}
	}

	for i, sym := range b {
		if !usedB[i] {
			unmatchedB = append(unmatchedB, sym)
		}
	}

	return unmatchedA, unmatchedB, matches
}

// findSignatureMatch finds the best candidate with same kind, matching signature,
// and matching body hash (if available). Returns -1 if no match found.
func findSignatureMatch(target *parser.Symbol, candidates []*parser.Symbol, used []bool) (int, float64) {
	bestIdx := -1
	bestScore := 0.0

	for i, c := range candidates {
		if used[i] || c.Kind != target.Kind || c.Signature == "" {
			continue
		}

		sigScore := nameSimilarity(target.Signature, c.Signature)
		if sigScore < signatureMatchThreshold {
			continue
		}

		// Boost score if body hashes also match.
		score := sigScore
		if target.BodyHash != 0 && c.BodyHash != 0 && target.BodyHash == c.BodyHash {
			score = (sigScore + 1.0) / 2 // average with perfect body match
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx, bestScore
}
```

**Step 4: Wire signature matching into MatchSymbols**

In `match.go`, update `MatchSymbols` — insert a new pass between fuzzy and semantic:

Replace the section after `matchFuzzy` (line ~36-44):

```go
	// Pass 2: fuzzy match by name similarity, same kind.
	unmatchedA, unmatchedB, fuzzyMatches := matchFuzzy(unmatchedA, unmatchedB)
	matches = append(matches, fuzzyMatches...)

	// Pass 3: signature match (catches renames where code is identical).
	unmatchedA, unmatchedB, sigMatches := matchSignature(unmatchedA, unmatchedB)
	matches = append(matches, sigMatches...)

	// Pass 4: semantic match via LLM classifier.
	if classifier != nil && (len(unmatchedA) > 0 || len(unmatchedB) > 0) {
```

**Step 5: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestMatchSignature -v`
Expected: PASS

**Step 6: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -v`
Expected: All PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/compare/compare.go internal/compare/match.go internal/compare/match_test.go
git commit -m "feat(compare): add signature-based matching to detect renames

New pass between fuzzy and semantic matching. Catches symbols that were
renamed but kept the same signature and body. Produces MatchRenamed type
with score based on signature similarity + body hash agreement."
```

---

## Task 4: Add cyclomatic complexity to RepoMetrics

**Files:**
- Create: `/path/to/repos/src/go-code/internal/compare/complexity.go`
- Create: `/path/to/repos/src/go-code/internal/compare/complexity_test.go`
- Modify: `/path/to/repos/src/go-code/internal/compare/metrics.go:101-115`

**Step 1: Write the failing test**

Create `/path/to/repos/src/go-code/internal/compare/complexity_test.go`:

```go
package compare

import (
	"testing"
)

func TestCyclomaticComplexity(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		expect int
	}{
		{
			name:   "empty body",
			body:   "",
			expect: 1,
		},
		{
			name:   "simple function",
			body:   "func Foo() { return 1 }",
			expect: 1,
		},
		{
			name:   "single if",
			body:   "func Foo() { if x > 0 { return 1 } return 0 }",
			expect: 2,
		},
		{
			name:   "if-else chain",
			body:   "func Foo() { if x > 0 { return 1 } else if x < 0 { return -1 } else { return 0 } }",
			expect: 3,
		},
		{
			name:   "for loop",
			body:   "func Foo() { for i := 0; i < n; i++ { sum += i } }",
			expect: 2,
		},
		{
			name:   "switch with cases",
			body:   "func Foo() { switch x { case 1: a() case 2: b() case 3: c() default: d() } }",
			expect: 4,
		},
		{
			name:   "logical operators",
			body:   "func Foo() { if a && b || c { return true } }",
			expect: 4, // 1 base + 1 if + 1 && + 1 ||
		},
		{
			name:   "Python patterns",
			body:   "def foo():\n    if x:\n        pass\n    elif y:\n        pass\n    for i in range(n):\n        pass\n    while z:\n        pass",
			expect: 5, // 1 base + if + elif + for + while
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cyclomaticComplexity(tt.body)
			if got != tt.expect {
				t.Errorf("cyclomaticComplexity() = %d, want %d", got, tt.expect)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestCyclomaticComplexity -v`
Expected: FAIL — `cyclomaticComplexity` undefined

**Step 3: Implement complexity.go**

Create `/path/to/repos/src/go-code/internal/compare/complexity.go`:

```go
package compare

import (
	"strings"
)

// cyclomaticComplexity estimates McCabe's cyclomatic complexity from a function body string.
// This is a heuristic based on keyword counting (not full AST), but is fast and
// language-agnostic. Base complexity is 1.
//
// Counted keywords: if, else if, elif, for, while, case, catch, except, &&, ||
func cyclomaticComplexity(body string) int {
	if body == "" {
		return 1
	}

	cc := 1

	// Decision-point keywords. Order matters: "else if" and "elif" must be
	// checked before bare "if" to avoid double-counting.
	type pattern struct {
		text      string
		increment int
	}
	keywords := []pattern{
		{"else if ", 1},
		{"elif ", 1},
		{"} else if ", 1}, // Go style with closing brace
	}
	// Process multi-word patterns first (and remove them so single-word scan doesn't double-count).
	cleaned := body
	for _, kw := range keywords {
		count := strings.Count(cleaned, kw.text)
		cc += count * kw.increment
		cleaned = strings.ReplaceAll(cleaned, kw.text, strings.Repeat(" ", len(kw.text)))
	}

	// Single-keyword patterns on the cleaned body.
	singles := []string{
		"if ", "for ", "while ", "case ", "catch ", "catch(", "except ", "rescue ",
	}
	for _, kw := range singles {
		cc += strings.Count(cleaned, kw)
	}

	// Logical operators (each adds a branch).
	cc += strings.Count(cleaned, "&&")
	cc += strings.Count(cleaned, "||")

	return cc
}
```

**Step 4: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestCyclomaticComplexity -v`
Expected: PASS

**Step 5: Wire complexity into RepoMetrics**

In `metrics.go`, add new fields to `RepoMetrics`:

```go
type RepoMetrics struct {
	Files              int     `json:"files"`
	TotalLines         int     `json:"totalLines"`
	AvgFuncLines       float64 `json:"avgFuncLines"`
	MaxFuncLines       int     `json:"maxFuncLines"`
	AvgComplexity      float64 `json:"avgComplexity"`
	MaxComplexity      int     `json:"maxComplexity"`
	TestRatio          float64 `json:"testRatio"`
	DocRatio           float64 `json:"docRatio"`
	ErrorHandlingRatio float64 `json:"errorHandlingRatio"`
	Interfaces         int     `json:"interfaces"`
	ExternalDeps       int     `json:"externalDeps"`
}
```

In `ComputeMetrics`, add complexity calculation inside the function loop (the `for _, sym := range snap.Symbols` block):

After `maxFuncLines` tracking, add:

```go
	totalComplexity := 0
	// (inside the existing loop over snap.Symbols where kind is function/method)
```

Specifically, modify the existing loop to also track complexity:

```go
	totalFuncLines := 0
	totalComplexity := 0
	funcCount := 0
	maxFuncLines := 0
	maxComplexity := 0

	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		lines := funcLines(sym)
		totalFuncLines += lines
		funcCount++
		if lines > maxFuncLines {
			maxFuncLines = lines
		}

		cc := cyclomaticComplexity(sym.Body)
		totalComplexity += cc
		if cc > maxComplexity {
			maxComplexity = cc
		}
	}

	var avgFuncLines float64
	if funcCount > 0 {
		avgFuncLines = float64(totalFuncLines) / float64(funcCount)
	}
	var avgComplexity float64
	if funcCount > 0 {
		avgComplexity = float64(totalComplexity) / float64(funcCount)
	}
```

And include in the return:

```go
	return RepoMetrics{
		Files:              snap.FileCount,
		TotalLines:         snap.TotalLines,
		AvgFuncLines:       avgFuncLines,
		MaxFuncLines:       maxFuncLines,
		AvgComplexity:      avgComplexity,
		MaxComplexity:      maxComplexity,
		TestRatio:          testFileRatio,
		...
```

**Step 6: Write metric test**

Add to `metrics_test.go`:

```go
func TestComputeMetrics_Complexity(t *testing.T) {
	snap := &RepoSnapshot{
		FileCount:  1,
		TotalLines: 100,
		Symbols: []*parser.Symbol{
			{
				Name: "Simple", Kind: parser.KindFunction,
				Body: "func Simple() { return 1 }", StartLine: 1, EndLine: 3,
			},
			{
				Name: "Complex", Kind: parser.KindFunction,
				Body: "func Complex() { if a { } if b && c { } for i := range x { } }",
				StartLine: 5, EndLine: 15,
			},
		},
	}

	m := ComputeMetrics(snap)

	if m.AvgComplexity == 0 {
		t.Error("AvgComplexity = 0, want > 0")
	}
	if m.MaxComplexity < 2 {
		t.Errorf("MaxComplexity = %d, want >= 2", m.MaxComplexity)
	}
}
```

**Step 7: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -v`
Expected: All PASS

**Step 8: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/compare/complexity.go internal/compare/complexity_test.go internal/compare/metrics.go internal/compare/metrics_test.go
git commit -m "feat(compare): add cyclomatic complexity to RepoMetrics

Keyword-based heuristic (fast, language-agnostic) counts decision points
in function bodies. Adds AvgComplexity and MaxComplexity to RepoMetrics.
Covers Go, Python, JS/TS, Java, Ruby patterns."
```

---

## Task 5: Prioritize most-different symbols for LLM context

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/compare/context.go`
- Test: `/path/to/repos/src/go-code/internal/compare/context_test.go`

**Step 1: Write the failing test**

Add to `context_test.go`:

```go
func TestBuildCompareContext_PrioritizesModified(t *testing.T) {
	// Create matches: 2 identical + 2 modified. Modified should appear first.
	matches := []SymbolMatch{
		{
			SymbolA: &parser.Symbol{Name: "Identical1", Kind: parser.KindFunction, File: "a.go", Body: "same"},
			SymbolB: &parser.Symbol{Name: "Identical1", Kind: parser.KindFunction, File: "b.go", Body: "same"},
			MatchType: MatchExact, Score: 1.0, Category: "function",
		},
		{
			SymbolA: &parser.Symbol{Name: "Changed1", Kind: parser.KindFunction, File: "a.go", Body: "old code"},
			SymbolB: &parser.Symbol{Name: "Changed1", Kind: parser.KindFunction, File: "b.go", Body: "new code"},
			MatchType: MatchModified, Score: 1.0, Category: "function",
		},
		{
			SymbolA: &parser.Symbol{Name: "Identical2", Kind: parser.KindFunction, File: "a.go", Body: "same2"},
			SymbolB: &parser.Symbol{Name: "Identical2", Kind: parser.KindFunction, File: "b.go", Body: "same2"},
			MatchType: MatchExact, Score: 1.0, Category: "function",
		},
		{
			SymbolA: &parser.Symbol{Name: "Renamed1", Kind: parser.KindFunction, File: "a.go", Body: "code"},
			SymbolB: &parser.Symbol{Name: "RenamedOne", Kind: parser.KindFunction, File: "b.go", Body: "code"},
			MatchType: MatchRenamed, Score: 0.9, Category: "function",
		},
	}

	ctx := BuildCompareContext(matches, RepoMetrics{}, RepoMetrics{}, "test")

	// Modified and Renamed should appear before Identical in the output.
	changedIdx := strings.Index(ctx, "Changed1")
	renamedIdx := strings.Index(ctx, "Renamed1")
	identical1Idx := strings.Index(ctx, "Identical1")

	if changedIdx < 0 {
		t.Fatal("Changed1 not found in context")
	}
	if renamedIdx < 0 {
		t.Fatal("Renamed1 not found in context")
	}
	if identical1Idx >= 0 && identical1Idx < changedIdx {
		t.Error("Identical1 appeared before Changed1 — modified symbols should be prioritized")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext_PrioritizesModified -v`
Expected: FAIL — identical symbols appear before modified ones

**Step 3: Add prioritization logic**

In `context.go`, add a sort helper and update `writeMatchedPairs`:

```go
import "sort"
```

Add a priority function:

```go
// matchPriority returns a sort key for symbol matches. Lower = higher priority.
// Modified/renamed/fuzzy matches are more interesting than identical exact matches.
func matchPriority(m *SymbolMatch) int {
	switch m.MatchType {
	case MatchModified:
		return 0 // highest priority — code actually changed
	case MatchRenamed:
		return 1
	case MatchFuzzy:
		return 2
	case MatchSemantic:
		return 3
	case MatchExact:
		return 4 // lowest — identical code, least interesting
	default:
		return 5
	}
}
```

Update `writeMatchedPairs` to sort before writing:

```go
func writeMatchedPairs(sb *strings.Builder, matches []SymbolMatch) {
	sb.WriteString("## Matched Symbols (side-by-side)\n\n")

	// Collect non-gap pairs and sort by priority (most interesting first).
	type indexedMatch struct {
		idx      int
		priority int
	}
	var pairs []indexedMatch
	for i := range matches {
		if !matches[i].IsGap() {
			pairs = append(pairs, indexedMatch{idx: i, priority: matchPriority(&matches[i])})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].priority < pairs[j].priority
	})

	written := 0
	for _, p := range pairs {
		if written >= maxMatchedPairs {
			break
		}
		if sb.Len() >= maxContextChars {
			break
		}
		writePair(sb, &matches[p.idx])
		written++
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext_PrioritizesModified -v`
Expected: PASS

**Step 5: Run all context tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestBuildCompareContext -v`
Expected: All PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/compare/context.go internal/compare/context_test.go
git commit -m "feat(compare): prioritize modified/renamed symbols in LLM context

Sort matched pairs by interest: modified > renamed > fuzzy > semantic > exact.
This ensures the LLM sees the most meaningful differences first within
the 80-pair budget."
```

---

## Task 6: Add match breakdown to CompareResult

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/compare/compare.go:147-158`
- Test: `/path/to/repos/src/go-code/internal/compare/compare_test.go`

**Step 1: Write the failing test**

Add to `compare_test.go`:

```go
func TestCompareRepos_MatchBreakdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	result, err := CompareRepos(context.Background(), CompareInput{
		RootA: root,
		RootB: root,
		Query: "test",
		Opts:  SnapshotOpts{Language: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("CompareRepos: %v", err)
	}

	// Self-compare: all matches should be exact (identical body).
	if result.MatchBreakdown.Exact == 0 {
		t.Error("expected Exact > 0 in self-compare")
	}
	if result.MatchBreakdown.Modified != 0 {
		t.Errorf("expected Modified = 0 in self-compare, got %d", result.MatchBreakdown.Modified)
	}
	if result.MatchBreakdown.Renamed != 0 {
		t.Errorf("expected Renamed = 0 in self-compare, got %d", result.MatchBreakdown.Renamed)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestCompareRepos_MatchBreakdown -v`
Expected: FAIL — `MatchBreakdown` field not found

**Step 3: Add MatchBreakdown struct and field**

In `compare.go`, add:

```go
// MatchBreakdown counts matches by type for structured reporting.
type MatchBreakdown struct {
	Exact    int `json:"exact"`
	Modified int `json:"modified"`
	Fuzzy    int `json:"fuzzy"`
	Renamed  int `json:"renamed"`
	Semantic int `json:"semantic"`
}
```

Add field to `CompareResult`:

```go
type CompareResult struct {
	RepoA          string         `json:"repo_a"`
	RepoB          string         `json:"repo_b"`
	Query          string         `json:"query"`
	MetricsA       RepoMetrics    `json:"metrics_a"`
	MetricsB       RepoMetrics    `json:"metrics_b"`
	Analysis       LLMAnalysis    `json:"analysis"`
	MatchedSymbols int            `json:"matched_symbols"`
	UnmatchedA     int            `json:"unmatched_a"`
	UnmatchedB     int            `json:"unmatched_b"`
	MatchBreakdown MatchBreakdown `json:"match_breakdown"`
}
```

**Step 4: Populate MatchBreakdown in CompareRepos**

In the `CompareRepos` function, update the match counting loop:

```go
	matched, unmatchedA, unmatchedB := 0, 0, 0
	var breakdown MatchBreakdown
	for _, m := range matches {
		switch {
		case m.SymbolB == nil && m.SymbolA != nil:
			unmatchedA++
		case m.SymbolA == nil && m.SymbolB != nil:
			unmatchedB++
		case m.SymbolA != nil && m.SymbolB != nil:
			matched++
			switch m.MatchType {
			case MatchExact:
				breakdown.Exact++
			case MatchModified:
				breakdown.Modified++
			case MatchFuzzy:
				breakdown.Fuzzy++
			case MatchRenamed:
				breakdown.Renamed++
			case MatchSemantic:
				breakdown.Semantic++
			}
		}
	}
```

And include in the result:

```go
	result := &CompareResult{
		...
		MatchBreakdown: breakdown,
	}
```

**Step 5: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -run TestCompareRepos_MatchBreakdown -v`
Expected: PASS

**Step 6: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -v`
Expected: All PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/compare/compare.go internal/compare/compare_test.go
git commit -m "feat(compare): add MatchBreakdown to CompareResult

Consumers can now see how many matches are exact, modified, fuzzy,
renamed, or semantic — without parsing all match entries."
```

---

## Task 7: Lint + full test pass + deploy

**Files:** None new — validation only.

**Step 1: Run linter**

Run: `cd /path/to/repos/src/go-code && make lint`
Expected: PASS with no errors. If errors appear, fix them before proceeding.

**Step 2: Run all tests**

Run: `cd /path/to/repos/src/go-code && make test`
Expected: All PASS

**Step 3: Deploy**

Run: `cd /path/to/repos/src/go-code && make deploy`
Expected: go-code container rebuilt and restarted.

**Step 4: Smoke test**

Run: `curl -s http://127.0.0.1:8897/health | head -1`
Expected: healthy response

**Step 5: Commit any lint fixes if needed**

```bash
cd /path/to/repos/src/go-code
git add -A
git commit -m "chore: lint fixes for code_compare v2"
```

---

## Summary of Changes

| Task | What | Files | New LOC (approx) |
|------|------|-------|-------------------|
| 1 | BodyHash on Symbol | parser.go, snapshot.go | +15 |
| 2 | MatchModified detection | compare.go, match.go | +10 |
| 3 | Signature matching (renames) | match.go | +55 |
| 4 | Cyclomatic complexity | complexity.go (new), metrics.go | +80 |
| 5 | Symbol prioritization | context.go | +30 |
| 6 | MatchBreakdown output | compare.go | +30 |
| 7 | Lint + test + deploy | — | — |
| **Total** | | **8 files touched, 2 new** | **~220 LOC** |

## Not in Scope (deferred to P2)

- AST diff via `smacker/gum` (separate plan, requires `go get github.com/smacker/gum`)
- Import graph diffing
- Hotspot scoring
- Composite quality grade (A-F)
- Git history / churn analysis
