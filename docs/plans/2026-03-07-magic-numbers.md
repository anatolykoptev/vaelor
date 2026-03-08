# Magic Number Detection in code_health

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add magic number / hardcoded constant detection as an 11th quality metric in `code_health`.

**Architecture:** Scan function bodies (already available as `sym.Body`) for numeric literals not in an allowed set (0, 1, -1, 2). Strip strings, comments, and const declarations before counting. Add `MagicNumberRatio` to `RepoMetrics`, a new sub-score with weight, outlier tracking, and recommendation message. Rebalance existing 10 weights to accommodate the 11th.

**Tech Stack:** Go, string scanning (no regex — matches existing pattern in `complexity.go`), tree-sitter AST bodies.

**Tests:** Already written in `internal/compare/metrics_magic_test.go` (TDD RED phase complete, all tests fail to compile).

---

### Task 1: Core Detection — `metrics_magic.go`

**Files:**
- Create: `internal/compare/metrics_magic.go`

**What to implement:**

Two functions:

1. `countMagicNumbers(body, language string) int` — counts magic number occurrences in a function body:
   - Strip comments using `clean.StripComments(body, language)`
   - Strip string literals (content between quotes) to avoid false positives on `"port 8080"`
   - Skip `const` lines entirely (`const maxSize = 1024` is fine)
   - Find numeric literals: integers (decimal, hex `0x`, octal `0o`, binary `0b`), floats
   - Allowed set (not magic): `0`, `1`, `-1`, `2` — these are universally accepted
   - Count everything else as magic
   - Return the count

2. `computeMagicNumberRatio(symbols []*parser.Symbol) float64` — ratio of functions containing magic numbers:
   - Only count `KindFunction` and `KindMethod`
   - Skip test files (use existing `isTestFile`)
   - A function "has magic" if `countMagicNumbers(body, lang) > 0`
   - Return `functionsWithMagic / totalEligibleFunctions`

**Numeric literal detection approach** (no regex, string scanning):
- Walk the body character by character
- When a digit is found (or `-` followed by digit), extract the full numeric token
- Check if preceded by `[` for array index context (small indices 0-2 are fine)
- Compare against allowed set

**Step 1:** Implement `countMagicNumbers` and `computeMagicNumberRatio` in `metrics_magic.go`.

**Step 2:** Run tests:
```bash
cd ~/src/go-code && go test ./internal/compare/ -run "TestCountMagicNumbers|TestComputeMagicNumberRatio" -v
```
Expected: all `TestCountMagicNumbers` and `TestComputeMagicNumberRatio*` tests PASS.

**Step 3:** Commit:
```bash
git add internal/compare/metrics_magic.go
git commit -m "feat(compare): add magic number detection"
```

---

### Task 2: Wire into `RepoMetrics` and `ComputeMetrics`

**Files:**
- Modify: `internal/compare/compare.go:159-182` — add `MagicNumberRatio float64` field to `RepoMetrics`
- Modify: `internal/compare/metrics.go:90-121` — call `computeMagicNumberRatio` and set the field

**Step 1:** Add field to `RepoMetrics` struct in `compare.go`:
```go
// After DuplicationRatio line:
MagicNumberRatio       float64 `json:"magicNumberRatio"`
```

**Step 2:** In `metrics.go`, after `duplicationRatio := computeDuplicationRatio(...)` add:
```go
magicNumberRatio := computeMagicNumberRatio(snap.Symbols)
```
And set `MagicNumberRatio: magicNumberRatio` in the result struct.

**Step 3:** Run test:
```bash
go test ./internal/compare/ -run "TestMagicNumbersInMetrics" -v
```
Expected: PASS.

**Step 4:** Commit:
```bash
git add internal/compare/compare.go internal/compare/metrics.go
git commit -m "feat(compare): wire MagicNumberRatio into RepoMetrics"
```

---

### Task 3: Add Sub-Score and Rebalance Weights

**Files:**
- Modify: `internal/compare/grade.go` — add 11th sub-score, rebalance weights
- Modify: `internal/compare/recommend.go` — add to `computeSubScores` and `buildMessage`

**New weight distribution** (must sum to 1.0):

| Metric | Old | New | Rationale |
|--------|-----|-----|-----------|
| cognitive_complexity | 0.15 | 0.13 | -0.02 |
| cyclomatic_avg | 0.08 | 0.07 | -0.01 |
| cyclomatic_max | 0.05 | 0.05 | same |
| test_coverage | 0.18 | 0.16 | -0.02 |
| doc_coverage | 0.10 | 0.09 | -0.01 |
| func_size | 0.10 | 0.09 | -0.01 |
| error_handling | 0.10 | 0.09 | -0.01 |
| nesting_depth | 0.08 | 0.08 | same |
| file_size | 0.08 | 0.08 | same |
| duplication | 0.08 | 0.08 | same |
| **magic_numbers** | — | **0.08** | new |
| **Total** | 1.00 | 1.00 | |

**Step 1:** In `grade.go`, update weight constants and add:
```go
weightMagicNumbers = 0.08
```
Add magic number sub-score formula:
```go
magicScore := clamp01(1.0 - m.MagicNumberRatio * 3.0)
```
Add to the `total` sum: `magicScore*weightMagicNumbers`

**Step 2:** In `recommend.go`, add to `computeSubScores` slice:
```go
{"magic_numbers", clamp01(1.0 - m.MagicNumberRatio*3.0), weightMagicNumbers, 0},
```
Add to `buildMessage` switch:
```go
case "magic_numbers":
    msg := fmt.Sprintf("Extract magic numbers into named constants (%.0f%% of functions affected)", m.MagicNumberRatio*100)
    return appendOutlier(msg, out.MaxMagicNumbers)
```

**Step 3:** Update existing grade tests — weight changes may shift scores. Run:
```bash
go test ./internal/compare/ -run "TestComputeGrade|TestGrade|TestNewMetrics|TestMagicNumbers" -v
```
Fix any test expectations that shifted due to rebalanced weights.

**Step 4:** Run drift guard test:
```bash
go test ./internal/compare/ -run "TestSubScoreSum" -v
```
Must still pass (SubScoreSum == GradeScore).

**Step 5:** Commit:
```bash
git add internal/compare/grade.go internal/compare/recommend.go
git commit -m "feat(compare): add magic_numbers sub-score (11th metric)"
```

---

### Task 4: Add Outlier Tracking

**Files:**
- Modify: `internal/compare/outliers.go` — add `MaxMagicNumbers OutlierFunc` to `Outliers` struct and collection

**Step 1:** Add field to `Outliers`:
```go
MaxMagicNumbers OutlierFunc
```

**Step 2:** In `CollectOutliers`, add inside the loop (after nesting depth):
```go
mn := countMagicNumbers(sym.Body, sym.Language)
if mn > out.MaxMagicNumbers.Value {
    out.MaxMagicNumbers = OutlierFunc{Name: sym.Name, File: rel, Line: line, Value: mn}
}
```

**Step 3:** Run test:
```bash
go test ./internal/compare/ -run "TestMagicNumbersInOutliers" -v
```
Expected: PASS.

**Step 4:** Commit:
```bash
git add internal/compare/outliers.go
git commit -m "feat(compare): track worst magic number offender in outliers"
```

---

### Task 5: Update XML Output

**Files:**
- Modify: `cmd/go-code/tool_code_compare.go:38-61` — add `MagicNumberRatio` to `xmlCompMetrics`
- Modify: `cmd/go-code/tool_code_compare.go:267-286` — add to `convertMetrics`

**Step 1:** Add to `xmlCompMetrics`:
```go
MagicNumberRatio       float64 `xml:"magicNumberRatio,attr"`
```

**Step 2:** Add to `convertMetrics`:
```go
MagicNumberRatio:       m.MagicNumberRatio,
```

**Step 3:** Run full test suite:
```bash
go test ./internal/compare/ ./cmd/go-code/ -v -count=1
```
Expected: all PASS.

**Step 4:** Commit:
```bash
git add cmd/go-code/tool_code_compare.go
git commit -m "feat(code_health): expose magic_numbers in XML output"
```

---

### Task 6: Update `explore/health.go` (lightweight health)

**Files:**
- Modify: `internal/explore/health.go` — add magic number sub-score to lightweight health

**Step 1:** Check if `explore/health.go` needs the magic number metric. The lightweight `computeHealth` uses a simpler 5-metric formula. Adding magic numbers here is optional but keeps parity.

If adding: compute magic number ratio from symbols, add weight and sub-score in the same pattern.

**Step 2:** Run:
```bash
go test ./internal/explore/ -v -count=1
```

**Step 3:** Commit.

---

### Task 7: Final Verification

**Step 1:** Run full test suite:
```bash
cd ~/src/go-code && make test
```

**Step 2:** Run linter:
```bash
make lint
```

**Step 3:** Test manually against a real repo:
```bash
# Use go-code MCP tool code_health on a repo with known magic numbers
```

**Step 4:** Final commit and deploy:
```bash
make deploy
```

---

## Subagent Assignment

| Task | Subagent | Dependencies |
|------|----------|-------------|
| Task 1 (core detection) | Subagent A | None |
| Task 2 (wire metrics) | Subagent A | Task 1 |
| Task 3 (scoring + weights) | Subagent A | Task 2 |
| Task 4 (outliers) | Subagent A | Task 1 |
| Task 5 (XML output) | Subagent A | Task 2 |
| Task 6 (explore health) | Subagent B | Task 1 |
| Task 7 (verification) | Main | All |

Tasks 1→2→3 are sequential (each builds on prior). Tasks 4 and 5 can run after their deps. Task 6 is independent after Task 1.

**Recommended flow:** Single subagent for Tasks 1-5 (all in `internal/compare` + `cmd/go-code`), separate subagent for Task 6, main agent for Task 7.
