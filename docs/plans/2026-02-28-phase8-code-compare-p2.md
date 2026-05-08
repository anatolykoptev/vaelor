# code_compare P2 — Quality Grade, Import Diff, Hotspots

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add composite quality grade (A-F), import graph diffing, git churn analysis, and hotspot scoring (churn × complexity) to `code_compare`, so the result includes actionable per-file risk data alongside the existing symbol-level comparison.

**Architecture:** Quality grade is a weighted scoring of existing `RepoMetrics` fields. Import diff compares unique imports already collected in `RepoSnapshot.Imports`. Git churn runs `git log --numstat` and parses output. Hotspot scoring combines per-file churn percentiles × per-file complexity percentiles (Omen formula). All new data is added to `CompareResult` and fed into the LLM context. No new external dependencies.

**Tech Stack:** Go 1.24+, `os/exec` for git, existing tree-sitter parser for complexity

---

### Task 1: Composite Quality Grade (A-F)

**Files:**
- Create: `$REPO_ROOT/internal/compare/grade.go`
- Create: `$REPO_ROOT/internal/compare/grade_test.go`
- Modify: `$REPO_ROOT/internal/compare/compare.go:108-120` (RepoMetrics → add Grade field)

**Step 1: Write the failing test**

Create `$REPO_ROOT/internal/compare/grade_test.go`:

```go
package compare

import (
	"testing"
)

func TestComputeGrade(t *testing.T) {
	tests := []struct {
		name    string
		metrics RepoMetrics
		expect  string
	}{
		{
			name: "excellent repo",
			metrics: RepoMetrics{
				Files: 50, TotalLines: 5000,
				AvgFuncLines: 12, MaxFuncLines: 40,
				AvgComplexity: 3.0, MaxComplexity: 8,
				TestRatio: 0.4, DocRatio: 0.8,
				ErrorHandlingRatio: 0.7, Interfaces: 5, ExternalDeps: 10,
			},
			expect: "A",
		},
		{
			name: "good repo",
			metrics: RepoMetrics{
				Files: 30, TotalLines: 4000,
				AvgFuncLines: 20, MaxFuncLines: 60,
				AvgComplexity: 5.0, MaxComplexity: 12,
				TestRatio: 0.25, DocRatio: 0.5,
				ErrorHandlingRatio: 0.5, Interfaces: 3, ExternalDeps: 15,
			},
			expect: "B",
		},
		{
			name: "mediocre repo",
			metrics: RepoMetrics{
				Files: 20, TotalLines: 3000,
				AvgFuncLines: 35, MaxFuncLines: 90,
				AvgComplexity: 8.0, MaxComplexity: 18,
				TestRatio: 0.1, DocRatio: 0.2,
				ErrorHandlingRatio: 0.3, Interfaces: 1, ExternalDeps: 25,
			},
			expect: "C",
		},
		{
			name: "poor repo — high complexity, no tests, no docs",
			metrics: RepoMetrics{
				Files: 10, TotalLines: 2000,
				AvgFuncLines: 50, MaxFuncLines: 150,
				AvgComplexity: 12.0, MaxComplexity: 25,
				TestRatio: 0.0, DocRatio: 0.0,
				ErrorHandlingRatio: 0.1, Interfaces: 0, ExternalDeps: 30,
			},
			expect: "D",
		},
		{
			name:    "empty repo",
			metrics: RepoMetrics{},
			expect:  "F",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeGrade(tt.metrics)
			if got != tt.expect {
				t.Errorf("ComputeGrade() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestGradeScore_Range(t *testing.T) {
	// Score should always be in [0, 100].
	metrics := RepoMetrics{
		Files: 100, TotalLines: 10000,
		AvgFuncLines: 15, MaxFuncLines: 50,
		AvgComplexity: 4.0, MaxComplexity: 10,
		TestRatio: 0.3, DocRatio: 0.6,
		ErrorHandlingRatio: 0.6, Interfaces: 8, ExternalDeps: 12,
	}
	score := gradeScore(metrics)
	if score < 0 || score > 100 {
		t.Errorf("gradeScore() = %.1f, want [0, 100]", score)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestComputeGrade -v`
Expected: FAIL — `ComputeGrade` undefined

**Step 3: Implement grade.go**

Create `$REPO_ROOT/internal/compare/grade.go`:

```go
package compare

import "math"

// Grade thresholds — score out of 100.
const (
	gradeAThreshold = 80
	gradeBThreshold = 60
	gradeCThreshold = 40
	gradeDThreshold = 20
)

// Scoring weights — must sum to 1.0.
const (
	weightComplexity     = 0.25
	weightTestCoverage   = 0.20
	weightDocCoverage    = 0.15
	weightFuncSize       = 0.15
	weightErrorHandling  = 0.15
	weightMaxComplexity  = 0.10
)

// gradeScore computes a quality score in [0, 100] from RepoMetrics.
// Higher is better.
func gradeScore(m RepoMetrics) float64 {
	if m.Files == 0 {
		return 0
	}

	// Each sub-score is in [0, 1], where 1 = best.

	// Complexity: avgComplexity <=3 → 1.0, >=15 → 0.0 (linear)
	complexityScore := clamp01(1.0 - (m.AvgComplexity-3.0)/12.0)

	// Max complexity: <=8 → 1.0, >=25 → 0.0
	maxComplexityScore := clamp01(1.0 - (float64(m.MaxComplexity)-8.0)/17.0)

	// Test ratio: >=0.3 → 1.0, 0 → 0.0
	testScore := clamp01(m.TestRatio / 0.3)

	// Doc ratio: >=0.6 → 1.0, 0 → 0.0
	docScore := clamp01(m.DocRatio / 0.6)

	// Avg func lines: <=15 → 1.0, >=60 → 0.0
	funcSizeScore := clamp01(1.0 - (m.AvgFuncLines-15.0)/45.0)

	// Error handling: >=0.6 → 1.0, 0 → 0.0
	errorScore := clamp01(m.ErrorHandlingRatio / 0.6)

	total := complexityScore*weightComplexity +
		maxComplexityScore*weightMaxComplexity +
		testScore*weightTestCoverage +
		docScore*weightDocCoverage +
		funcSizeScore*weightFuncSize +
		errorScore*weightErrorHandling

	return math.Round(total * 100)
}

// ComputeGrade returns a letter grade (A-F) for the given metrics.
func ComputeGrade(m RepoMetrics) string {
	score := gradeScore(m)
	switch {
	case score >= gradeAThreshold:
		return "A"
	case score >= gradeBThreshold:
		return "B"
	case score >= gradeCThreshold:
		return "C"
	case score >= gradeDThreshold:
		return "D"
	default:
		return "F"
	}
}

// clamp01 clamps v to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
```

**Step 4: Run test to verify it passes**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestComputeGrade -v`
Expected: PASS

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestGradeScore_Range -v`
Expected: PASS

**Step 5: Add Grade field to RepoMetrics**

In `$REPO_ROOT/internal/compare/compare.go`, add to `RepoMetrics`:

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
	Grade              string  `json:"grade"`
}
```

In `$REPO_ROOT/internal/compare/metrics.go`, add at end of `ComputeMetrics` before `return`:

```go
	result := RepoMetrics{
		// ... existing fields ...
	}
	result.Grade = ComputeGrade(result)
	return result
```

**Step 6: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -v`
Expected: All PASS

**Step 7: Commit**

```bash
cd $REPO_ROOT
git add internal/compare/grade.go internal/compare/grade_test.go internal/compare/compare.go internal/compare/metrics.go
git commit -m "feat(compare): add composite quality grade (A-F) to RepoMetrics

Weighted scoring of avgComplexity (25%), testRatio (20%), docRatio (15%),
avgFuncLines (15%), errorHandling (15%), maxComplexity (10%). Score 0-100
mapped to A/B/C/D/F letter grades."
```

---

### Task 2: Import Diff

**Files:**
- Create: `$REPO_ROOT/internal/compare/importdiff.go`
- Create: `$REPO_ROOT/internal/compare/importdiff_test.go`
- Modify: `$REPO_ROOT/internal/compare/compare.go:164-176` (CompareResult → add ImportDiff)

**Step 1: Write the failing test**

Create `$REPO_ROOT/internal/compare/importdiff_test.go`:

```go
package compare

import (
	"testing"
)

func TestComputeImportDiff(t *testing.T) {
	importsA := []string{"fmt", "net/http", "github.com/foo/bar", "github.com/shared/lib"}
	importsB := []string{"fmt", "os", "github.com/baz/qux", "github.com/shared/lib"}

	diff := ComputeImportDiff(importsA, importsB)

	if diff.CommonCount != 2 { // fmt, github.com/shared/lib
		t.Errorf("CommonCount = %d, want 2", diff.CommonCount)
	}
	if diff.OnlyACount != 2 { // net/http, github.com/foo/bar
		t.Errorf("OnlyACount = %d, want 2", diff.OnlyACount)
	}
	if diff.OnlyBCount != 2 { // os, github.com/baz/qux
		t.Errorf("OnlyBCount = %d, want 2", diff.OnlyBCount)
	}

	// Check lists
	if !containsStr(diff.OnlyA, "net/http") {
		t.Error("OnlyA should contain net/http")
	}
	if !containsStr(diff.OnlyB, "os") {
		t.Error("OnlyB should contain os")
	}
}

func TestComputeImportDiff_Empty(t *testing.T) {
	diff := ComputeImportDiff(nil, nil)
	if diff.CommonCount != 0 || diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Errorf("expected all zeros for empty imports, got %+v", diff)
	}
}

func TestComputeImportDiff_Identical(t *testing.T) {
	imports := []string{"fmt", "os", "net/http"}
	diff := ComputeImportDiff(imports, imports)
	if diff.CommonCount != 3 {
		t.Errorf("CommonCount = %d, want 3", diff.CommonCount)
	}
	if diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Error("identical imports should have 0 only-A and only-B")
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestComputeImportDiff -v`
Expected: FAIL — `ComputeImportDiff` undefined

**Step 3: Implement importdiff.go**

Create `$REPO_ROOT/internal/compare/importdiff.go`:

```go
package compare

import "sort"

// ImportDiff captures the difference between two import sets.
type ImportDiff struct {
	CommonCount int      `json:"commonCount"`
	OnlyACount  int      `json:"onlyACount"`
	OnlyBCount  int      `json:"onlyBCount"`
	OnlyA       []string `json:"onlyA,omitempty"`
	OnlyB       []string `json:"onlyB,omitempty"`
}

// maxImportDiffItems limits how many items are listed in OnlyA/OnlyB
// to keep the JSON output manageable.
const maxImportDiffItems = 30

// ComputeImportDiff computes the set difference between two import lists.
func ComputeImportDiff(importsA, importsB []string) ImportDiff {
	setA := make(map[string]struct{}, len(importsA))
	for _, imp := range importsA {
		setA[imp] = struct{}{}
	}

	setB := make(map[string]struct{}, len(importsB))
	for _, imp := range importsB {
		setB[imp] = struct{}{}
	}

	var common, onlyA, onlyB int
	var onlyAList, onlyBList []string

	for imp := range setA {
		if _, ok := setB[imp]; ok {
			common++
		} else {
			onlyA++
			if len(onlyAList) < maxImportDiffItems {
				onlyAList = append(onlyAList, imp)
			}
		}
	}

	for imp := range setB {
		if _, ok := setA[imp]; !ok {
			onlyB++
			if len(onlyBList) < maxImportDiffItems {
				onlyBList = append(onlyBList, imp)
			}
		}
	}

	sort.Strings(onlyAList)
	sort.Strings(onlyBList)

	return ImportDiff{
		CommonCount: common,
		OnlyACount:  onlyA,
		OnlyBCount:  onlyB,
		OnlyA:       onlyAList,
		OnlyB:       onlyBList,
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestComputeImportDiff -v`
Expected: All PASS

**Step 5: Wire into CompareResult**

In `$REPO_ROOT/internal/compare/compare.go`, add `ImportDiff` field to `CompareResult`:

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
	ImportDiff     ImportDiff     `json:"import_diff"`
}
```

In `CompareRepos`, after `metricsB := ComputeMetrics(snapB)`, add:

```go
	importDiff := ComputeImportDiff(snapA.Imports, snapB.Imports)
```

And include in the result:

```go
	result := &CompareResult{
		// ... existing fields ...
		MatchBreakdown: breakdown,
		ImportDiff:     importDiff,
	}
```

**Step 6: Add integration test**

Add to `$REPO_ROOT/internal/compare/compare_test.go`:

```go
func TestCompareRepos_ImportDiff(t *testing.T) {
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

	// Self-compare: all imports should be common.
	if result.ImportDiff.CommonCount == 0 {
		t.Error("expected CommonCount > 0 in self-compare")
	}
	if result.ImportDiff.OnlyACount != 0 {
		t.Errorf("expected OnlyACount = 0 in self-compare, got %d", result.ImportDiff.OnlyACount)
	}
	if result.ImportDiff.OnlyBCount != 0 {
		t.Errorf("expected OnlyBCount = 0 in self-compare, got %d", result.ImportDiff.OnlyBCount)
	}
}
```

**Step 7: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -v`
Expected: All PASS

**Step 8: Commit**

```bash
cd $REPO_ROOT
git add internal/compare/importdiff.go internal/compare/importdiff_test.go internal/compare/compare.go internal/compare/compare_test.go
git commit -m "feat(compare): add import diff to CompareResult

Shows common, only-A, only-B imports with counts and lists.
Helps identify dependency strategy differences between repos."
```

---

### Task 3: Git Churn Analysis

**Files:**
- Create: `$REPO_ROOT/internal/compare/churn.go`
- Create: `$REPO_ROOT/internal/compare/churn_test.go`

**Step 1: Write the failing test**

Create `$REPO_ROOT/internal/compare/churn_test.go`:

```go
package compare

import (
	"context"
	"testing"
)

func TestParseNumstatLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantAdd int
		wantDel int
		wantPath string
		wantOK  bool
	}{
		{
			name: "normal line",
			line: "25\t3\tinternal/compare/compare.go",
			wantAdd: 25, wantDel: 3, wantPath: "internal/compare/compare.go", wantOK: true,
		},
		{
			name: "binary file",
			line: "-\t-\timage.png",
			wantOK: false,
		},
		{
			name: "empty line",
			line: "",
			wantOK: false,
		},
		{
			name: "rename with arrow",
			line: "10\t5\t{old => new}/file.go",
			wantAdd: 10, wantDel: 5, wantPath: "new/file.go", wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			add, del, path, ok := parseNumstatLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if add != tt.wantAdd || del != tt.wantDel || path != tt.wantPath {
				t.Errorf("got (%d, %d, %q), want (%d, %d, %q)",
					add, del, path, tt.wantAdd, tt.wantDel, tt.wantPath)
			}
		})
	}
}

func TestCollectChurn_RealRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	churn, err := CollectChurn(context.Background(), root)
	if err != nil {
		t.Fatalf("CollectChurn: %v", err)
	}

	if len(churn) == 0 {
		t.Error("expected churn data for at least one file")
	}

	// Check that known files have commits > 0.
	for path, stats := range churn {
		if stats.Commits <= 0 {
			t.Errorf("file %q has %d commits, want > 0", path, stats.Commits)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestParseNumstatLine -v`
Expected: FAIL — `parseNumstatLine` undefined

**Step 3: Implement churn.go**

Create `$REPO_ROOT/internal/compare/churn.go`:

```go
package compare

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ChurnStats holds change frequency data for a single file.
type ChurnStats struct {
	// Commits is the number of commits that touched this file.
	Commits int `json:"commits"`

	// Additions is the total lines added across all commits.
	Additions int `json:"additions"`

	// Deletions is the total lines deleted across all commits.
	Deletions int `json:"deletions"`
}

// ChurnScore returns a weighted churn score as defined by Omen:
// sum(1.0 + (additions + deletions) / 100.0) per commit.
// Larger changes are weighted more than small ones.
func (c ChurnStats) ChurnScore() float64 {
	// Approximate: commits contribute base 1.0 each + size bonus.
	return float64(c.Commits) + float64(c.Additions+c.Deletions)/100.0
}

// gitLogTimeout is the maximum time to wait for git log.
const gitLogTimeout = 30

// CollectChurn runs git log --numstat on the given repo root and returns
// churn statistics per file (relative paths).
// Returns nil map and nil error for non-git directories.
func CollectChurn(ctx context.Context, root string) (map[string]ChurnStats, error) {
	ctx, cancel := context.WithTimeout(ctx, gitLogTimeout*1_000_000_000) // 30s in ns
	defer cancel()

	//nolint:gosec // root is a trusted local path from resolveRoot
	cmd := exec.CommandContext(ctx, "git", "-C", root, "log",
		"--numstat", "--format=", "--no-merges",
		"--diff-filter=AMRC")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Not a git repo or git not available — non-fatal.
		if strings.Contains(stderr.String(), "not a git repository") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log: %w: %s", err, stderr.String())
	}

	return parseNumstatOutput(stdout.Bytes()), nil
}

// parseNumstatOutput parses the full output of git log --numstat --format=.
func parseNumstatOutput(data []byte) map[string]ChurnStats {
	result := make(map[string]ChurnStats)
	// Track which files appear in which "commit block" (separated by blank lines).
	// Since --format= produces empty lines between commits, we use blank lines
	// to count commit boundaries per file.
	var currentFiles map[string]struct{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Commit boundary — flush current file set.
			if currentFiles != nil {
				for path := range currentFiles {
					stats := result[path]
					stats.Commits++
					result[path] = stats
				}
			}
			currentFiles = make(map[string]struct{})
			continue
		}

		add, del, path, ok := parseNumstatLine(line)
		if !ok {
			continue
		}

		if currentFiles == nil {
			currentFiles = make(map[string]struct{})
		}
		currentFiles[path] = struct{}{}

		stats := result[path]
		stats.Additions += add
		stats.Deletions += del
		result[path] = stats
	}

	// Flush last commit block.
	if currentFiles != nil {
		for path := range currentFiles {
			stats := result[path]
			stats.Commits++
			result[path] = stats
		}
	}

	return result
}

// parseNumstatLine parses a single numstat line: "ADD\tDEL\tPATH".
// Returns false for binary files (shown as "-\t-\t...") or malformed lines.
func parseNumstatLine(line string) (add, del int, path string, ok bool) {
	if line == "" {
		return 0, 0, "", false
	}

	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return 0, 0, "", false
	}

	// Binary files show "-" for add/del.
	if parts[0] == "-" || parts[1] == "-" {
		return 0, 0, "", false
	}

	add, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, "", false
	}

	del, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, "", false
	}

	path = parts[2]

	// Handle renames: {old => new}/file.go → new/file.go
	if idx := strings.Index(path, "{"); idx >= 0 {
		path = resolveRenamePath(path)
	}

	return add, del, path, true
}

// resolveRenamePath resolves git rename notation: "prefix/{old => new}/suffix" → "prefix/new/suffix".
func resolveRenamePath(path string) string {
	open := strings.Index(path, "{")
	close := strings.Index(path, "}")
	if open < 0 || close < 0 || close <= open {
		return path
	}

	prefix := path[:open]
	suffix := path[close+1:]
	inner := path[open+1 : close]

	arrow := strings.Index(inner, " => ")
	if arrow < 0 {
		return path
	}

	newPart := inner[arrow+4:]
	return prefix + newPart + suffix
}
```

**Step 4: Run tests to verify they pass**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestParseNumstatLine -v`
Expected: PASS

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestCollectChurn_RealRepo -v`
Expected: PASS

**Step 5: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
cd $REPO_ROOT
git add internal/compare/churn.go internal/compare/churn_test.go
git commit -m "feat(compare): add git churn analysis

Runs git log --numstat to collect per-file change frequency.
ChurnScore uses Omen formula: commits + (additions+deletions)/100.
Handles renames, binary files, non-git repos gracefully."
```

---

### Task 4: Hotspot Scoring

**Files:**
- Create: `$REPO_ROOT/internal/compare/hotspot.go`
- Create: `$REPO_ROOT/internal/compare/hotspot_test.go`

**Step 1: Write the failing test**

Create `$REPO_ROOT/internal/compare/hotspot_test.go`:

```go
package compare

import (
	"testing"
)

func TestComputeHotspots(t *testing.T) {
	churn := map[string]ChurnStats{
		"a.go": {Commits: 20, Additions: 500, Deletions: 200},
		"b.go": {Commits: 2, Additions: 50, Deletions: 10},
		"c.go": {Commits: 10, Additions: 200, Deletions: 100},
		"d.go": {Commits: 1, Additions: 10, Deletions: 5},
	}

	fileComplexity := map[string]float64{
		"a.go": 12.0,
		"b.go": 2.0,
		"c.go": 8.0,
		"d.go": 15.0,
	}

	hotspots := ComputeHotspots(churn, fileComplexity)

	if len(hotspots) == 0 {
		t.Fatal("expected hotspots, got none")
	}

	// a.go has highest churn AND high complexity → should be top hotspot.
	if hotspots[0].File != "a.go" {
		t.Errorf("top hotspot = %q, want a.go", hotspots[0].File)
	}

	// Scores should be in [0, 1].
	for _, h := range hotspots {
		if h.Score < 0 || h.Score > 1 {
			t.Errorf("hotspot %q score = %.2f, want [0, 1]", h.File, h.Score)
		}
	}

	// Should be sorted by score descending.
	for i := 1; i < len(hotspots); i++ {
		if hotspots[i].Score > hotspots[i-1].Score {
			t.Errorf("hotspots not sorted: [%d].Score=%.2f > [%d].Score=%.2f",
				i, hotspots[i].Score, i-1, hotspots[i-1].Score)
		}
	}
}

func TestComputeHotspots_Empty(t *testing.T) {
	hotspots := ComputeHotspots(nil, nil)
	if len(hotspots) != 0 {
		t.Errorf("expected empty hotspots, got %d", len(hotspots))
	}
}

func TestPercentileRank(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	// Value 5 should be at 100th percentile.
	if p := percentileRank(5, values); p != 1.0 {
		t.Errorf("percentileRank(5) = %.2f, want 1.0", p)
	}
	// Value 1 should be at 20th percentile (1 out of 5).
	if p := percentileRank(1, values); p < 0.1 || p > 0.3 {
		t.Errorf("percentileRank(1) = %.2f, want ~0.2", p)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestComputeHotspots -v`
Expected: FAIL — `ComputeHotspots` undefined

**Step 3: Implement hotspot.go**

Create `$REPO_ROOT/internal/compare/hotspot.go`:

```go
package compare

import (
	"sort"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// HotspotFile is a file identified as a maintenance risk (high churn × high complexity).
type HotspotFile struct {
	File       string  `json:"file"`
	Score      float64 `json:"score"`
	Churn      int     `json:"churn"`
	Complexity float64 `json:"complexity"`
	Risk       string  `json:"risk"` // "critical", "high", "moderate"
}

// Hotspot thresholds from Omen (percentile products).
const (
	hotspotCritical = 0.81
	hotspotHigh     = 0.64
	hotspotModerate = 0.36
)

// maxHotspots limits the number of hotspots returned.
const maxHotspots = 20

// ComputeHotspots combines churn data and per-file complexity to find maintenance hotspots.
// Uses the Omen formula: score = percentile(churn) × percentile(complexity).
func ComputeHotspots(churn map[string]ChurnStats, fileComplexity map[string]float64) []HotspotFile {
	if len(churn) == 0 || len(fileComplexity) == 0 {
		return nil
	}

	// Collect files that have both churn and complexity data.
	type entry struct {
		file       string
		churnScore float64
		complexity float64
	}
	var entries []entry
	for file, stats := range churn {
		cc, ok := fileComplexity[file]
		if !ok || cc <= 0 {
			continue
		}
		entries = append(entries, entry{file: file, churnScore: stats.ChurnScore(), complexity: cc})
	}

	if len(entries) == 0 {
		return nil
	}

	// Compute percentile ranks.
	churnValues := make([]float64, len(entries))
	complexityValues := make([]float64, len(entries))
	for i, e := range entries {
		churnValues[i] = e.churnScore
		complexityValues[i] = e.complexity
	}
	sort.Float64s(churnValues)
	sort.Float64s(complexityValues)

	var hotspots []HotspotFile
	for _, e := range entries {
		churnPct := percentileRank(e.churnScore, churnValues)
		complexityPct := percentileRank(e.complexity, complexityValues)
		score := churnPct * complexityPct

		risk := classifyRisk(score)
		if risk == "" {
			continue // below moderate threshold
		}

		hotspots = append(hotspots, HotspotFile{
			File:       e.file,
			Score:      score,
			Churn:      int(e.churnScore),
			Complexity: e.complexity,
			Risk:       risk,
		})
	}

	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].Score > hotspots[j].Score
	})

	if len(hotspots) > maxHotspots {
		hotspots = hotspots[:maxHotspots]
	}

	return hotspots
}

// FileComplexityFromSnapshot computes average cyclomatic complexity per file
// from the snapshot's symbols.
func FileComplexityFromSnapshot(snap *RepoSnapshot) map[string]float64 {
	type acc struct {
		total int
		count int
	}

	byFile := make(map[string]*acc)
	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if sym.File == "" {
			continue
		}
		a, ok := byFile[sym.File]
		if !ok {
			a = &acc{}
			byFile[sym.File] = a
		}
		a.total += cyclomaticComplexity(sym.Body)
		a.count++
	}

	result := make(map[string]float64, len(byFile))
	for file, a := range byFile {
		if a.count > 0 {
			result[file] = float64(a.total) / float64(a.count)
		}
	}
	return result
}

// percentileRank returns the fraction of values in the sorted slice that are <= val.
// Range: [1/N, 1.0] for values present in the slice.
func percentileRank(val float64, sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	// Count values <= val.
	count := 0
	for _, v := range sorted {
		if v <= val {
			count++
		}
	}
	return float64(count) / float64(n)
}

// classifyRisk returns the risk classification based on hotspot score thresholds.
func classifyRisk(score float64) string {
	switch {
	case score >= hotspotCritical:
		return "critical"
	case score >= hotspotHigh:
		return "high"
	case score >= hotspotModerate:
		return "moderate"
	default:
		return ""
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run "TestComputeHotspots|TestPercentileRank" -v`
Expected: All PASS

**Step 5: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
cd $REPO_ROOT
git add internal/compare/hotspot.go internal/compare/hotspot_test.go
git commit -m "feat(compare): add hotspot scoring (churn × complexity)

Omen formula: score = percentile(churn) × percentile(complexity).
Risk levels: critical (≥0.81), high (≥0.64), moderate (≥0.36).
FileComplexityFromSnapshot extracts per-file avg complexity."
```

---

### Task 5: Wire Hotspots into CompareRepos and LLM Context

**Files:**
- Modify: `$REPO_ROOT/internal/compare/compare.go:164-274` (add hotspot fields + collection)
- Modify: `$REPO_ROOT/internal/compare/context.go` (add hotspot section)
- Modify: `$REPO_ROOT/internal/compare/context_test.go`
- Modify: `$REPO_ROOT/internal/compare/compare_test.go`

**Step 1: Write the failing test for context**

Add to `$REPO_ROOT/internal/compare/context_test.go`:

```go
func TestBuildCompareContext_IncludesHotspots(t *testing.T) {
	matches := []SymbolMatch{
		{
			SymbolA:   &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "a.go", Body: "code"},
			SymbolB:   &parser.Symbol{Name: "Foo", Kind: parser.KindFunction, File: "b.go", Body: "code"},
			MatchType: MatchExact, Score: 1.0, Category: "function",
		},
	}

	hotspotsA := []HotspotFile{
		{File: "hot.go", Score: 0.95, Churn: 50, Complexity: 12.0, Risk: "critical"},
	}
	hotspotsB := []HotspotFile{
		{File: "warm.go", Score: 0.70, Churn: 30, Complexity: 8.0, Risk: "high"},
	}

	ctx := BuildCompareContextV2(matches, RepoMetrics{}, RepoMetrics{}, "test", hotspotsA, hotspotsB)

	if !strings.Contains(ctx, "Hotspots") {
		t.Error("context should contain Hotspots section")
	}
	if !strings.Contains(ctx, "hot.go") {
		t.Error("context should contain hotspot file hot.go")
	}
	if !strings.Contains(ctx, "critical") {
		t.Error("context should contain risk level critical")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestBuildCompareContext_IncludesHotspots -v`
Expected: FAIL — `BuildCompareContextV2` undefined

**Step 3: Update BuildCompareContext to accept hotspots**

In `$REPO_ROOT/internal/compare/context.go`:

1. Rename `BuildCompareContext` → keep it as a backward-compat wrapper, add `BuildCompareContextV2`:

```go
// BuildCompareContext assembles structured text context for the LLM (no hotspots).
func BuildCompareContext(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string) string {
	return BuildCompareContextV2(matches, metricsA, metricsB, query, nil, nil)
}

// BuildCompareContextV2 assembles structured text context for the LLM, including hotspot data.
func BuildCompareContextV2(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string, hotspotsA, hotspotsB []HotspotFile) string {
	var sb strings.Builder

	writeQuery(&sb, query)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	writeMetrics(&sb, metricsA, metricsB)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	if len(hotspotsA) > 0 || len(hotspotsB) > 0 {
		writeHotspots(&sb, hotspotsA, hotspotsB)
		if sb.Len() >= maxContextChars {
			return sb.String()
		}
	}

	writeMatchedPairs(&sb, matches)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	writeGaps(&sb, matches)

	return sb.String()
}
```

Add the `writeHotspots` function:

```go
// maxHotspotsInContext limits hotspots shown to LLM.
const maxHotspotsInContext = 10

func writeHotspots(sb *strings.Builder, hotspotsA, hotspotsB []HotspotFile) {
	sb.WriteString("## Maintenance Hotspots\n\n")

	if len(hotspotsA) > 0 {
		sb.WriteString("**Repo A hotspots** (high churn × high complexity):\n")
		limit := len(hotspotsA)
		if limit > maxHotspotsInContext {
			limit = maxHotspotsInContext
		}
		for _, h := range hotspotsA[:limit] {
			fmt.Fprintf(sb, "- `%s` — risk: %s, score: %.2f, churn: %d, complexity: %.1f\n",
				h.File, h.Risk, h.Score, h.Churn, h.Complexity)
		}
		sb.WriteString("\n")
	}

	if len(hotspotsB) > 0 {
		sb.WriteString("**Repo B hotspots** (high churn × high complexity):\n")
		limit := len(hotspotsB)
		if limit > maxHotspotsInContext {
			limit = maxHotspotsInContext
		}
		for _, h := range hotspotsB[:limit] {
			fmt.Fprintf(sb, "- `%s` — risk: %s, score: %.2f, churn: %d, complexity: %.1f\n",
				h.File, h.Risk, h.Score, h.Churn, h.Complexity)
		}
		sb.WriteString("\n")
	}
}
```

**Step 4: Wire hotspots into CompareRepos**

In `$REPO_ROOT/internal/compare/compare.go`:

Add fields to `CompareResult`:

```go
type CompareResult struct {
	// ... existing fields ...
	ImportDiff     ImportDiff     `json:"import_diff"`
	HotspotsA     []HotspotFile  `json:"hotspots_a,omitempty"`
	HotspotsB     []HotspotFile  `json:"hotspots_b,omitempty"`
}
```

In `CompareRepos`, after computing import diff, add:

```go
	// Hotspot analysis (non-fatal — skip if git unavailable).
	var hotspotsA, hotspotsB []HotspotFile
	churnA, _ := CollectChurn(ctx, input.RootA)
	churnB, _ := CollectChurn(ctx, input.RootB)
	if churnA != nil {
		hotspotsA = ComputeHotspots(churnA, FileComplexityFromSnapshot(snapA))
	}
	if churnB != nil {
		hotspotsB = ComputeHotspots(churnB, FileComplexityFromSnapshot(snapB))
	}
```

Add to result:

```go
	result := &CompareResult{
		// ... existing fields ...
		ImportDiff:     importDiff,
		HotspotsA:     hotspotsA,
		HotspotsB:     hotspotsB,
	}
```

Update the LLM context call to use V2:

```go
	if llmClient != nil {
		compareCtx := BuildCompareContextV2(matches, metricsA, metricsB, input.Query, hotspotsA, hotspotsB)
		answer, err := llmClient.Complete(ctx, llm.SystemPromptCodeCompare, compareCtx)
```

**Step 5: Run context test to verify it passes**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -run TestBuildCompareContext_IncludesHotspots -v`
Expected: PASS

**Step 6: Add integration test**

Add to `$REPO_ROOT/internal/compare/compare_test.go`:

```go
func TestCompareRepos_Hotspots(t *testing.T) {
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

	// Self-compare on a real repo with git history should produce some hotspots.
	// (May be empty on brand-new repos, so just check no error.)
	t.Logf("HotspotsA: %d, HotspotsB: %d", len(result.HotspotsA), len(result.HotspotsB))
}
```

**Step 7: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -v`
Expected: All PASS

**Step 8: Commit**

```bash
cd $REPO_ROOT
git add internal/compare/compare.go internal/compare/context.go internal/compare/context_test.go internal/compare/compare_test.go
git commit -m "feat(compare): wire hotspots into CompareResult and LLM context

CollectChurn + FileComplexityFromSnapshot → ComputeHotspots for each repo.
BuildCompareContextV2 adds hotspot section before matched symbols.
Non-fatal: skipped gracefully for non-git repos."
```

---

### Task 6: Lint + Full Test Pass + Deploy

**Files:** None new — validation only.

**Step 1: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/compare/ -v -count=1`
Expected: All PASS

Run: `cd $REPO_ROOT && go test ./... -count=1`
Expected: All PASS

**Step 2: Deploy**

```bash
cd ~/deploy/my-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Smoke test**

Run: `curl -s http://127.0.0.1:8897/health | head -1`
Expected: healthy response

**Step 4: Commit any lint fixes if needed**

```bash
cd $REPO_ROOT
git add -A
git commit -m "chore: lint fixes for code_compare P2"
```

---

## Summary of Changes

| Task | What | Files | New LOC (approx) |
|------|------|-------|-------------------|
| 1 | Quality grade (A-F) | grade.go (new) | +80 |
| 2 | Import diff | importdiff.go (new) | +60 |
| 3 | Git churn analysis | churn.go (new) | +130 |
| 4 | Hotspot scoring | hotspot.go (new) | +120 |
| 5 | Wire into CompareResult + LLM context | compare.go, context.go | +60 |
| 6 | Lint + test + deploy | — | — |
| **Total** | | **4 new files, 2 modified** | **~450 LOC** |

## Not in Scope (deferred to P3)

- AST diff via `smacker/gum` (needs new dependency, separate plan)
- Identifier-level reference graph + personalized PageRank
- INHERITS/IMPLEMENTS edges
- Semantic search via embeddings
- Cross-language HTTP boundaries
