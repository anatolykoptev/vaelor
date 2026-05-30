package compare

import (
	"context"
	"testing"
)

func TestParseNumstatLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantAdd  int
		wantDel  int
		wantPath string
		wantOK   bool
	}{
		{
			name:     "normal line",
			line:     "25\t3\tinternal/compare/compare.go",
			wantAdd:  25,
			wantDel:  3,
			wantPath: "internal/compare/compare.go",
			wantOK:   true,
		},
		{
			name:   "binary file",
			line:   "-\t-\timage.png",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:     "rename with arrow",
			line:     "10\t5\t{old => new}/file.go",
			wantAdd:  10,
			wantDel:  5,
			wantPath: "new/file.go",
			wantOK:   true,
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

func TestParseNumstatOutput(t *testing.T) {
	// Simulate git log --pretty=format:%x00 output with two commits.
	// Commit 1: a.go +25/-3, b.go +10/-5
	// Commit 2: a.go +15/-2
	data := []byte("\x00\n25\t3\ta.go\n10\t5\tb.go\n\x00\n15\t2\ta.go\n")

	result := parseNumstatOutput(data)

	if len(result) != 2 {
		t.Fatalf("got %d files, want 2", len(result))
	}

	aStats := result["a.go"]
	if aStats.Commits != 2 {
		t.Errorf("a.go commits = %d, want 2", aStats.Commits)
	}
	if aStats.Additions != 40 { // 25 + 15
		t.Errorf("a.go additions = %d, want 40", aStats.Additions)
	}

	bStats := result["b.go"]
	if bStats.Commits != 1 {
		t.Errorf("b.go commits = %d, want 1", bStats.Commits)
	}
}

func TestCollectChurn_RealRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	churn, err := CollectChurn(context.Background(), root, 0)
	if err != nil {
		t.Fatalf("CollectChurn: %v", err)
	}

	if len(churn) == 0 {
		t.Error("expected churn data for at least one file")
	}

	for path, stats := range churn {
		if stats.Commits <= 0 {
			t.Errorf("file %q has %d commits, want > 0", path, stats.Commits)
		}
	}
}

func TestResolveRenamePath(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"{old => new}/file.go", "new/file.go"},
		{"prefix/{old => new}/suffix.go", "prefix/new/suffix.go"},
		{"no-rename.go", "no-rename.go"},
		{"{a => b}", "b"},
	}

	for _, tt := range tests {
		got := resolveRenamePath(tt.input)
		if got != tt.expect {
			t.Errorf("resolveRenamePath(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestChurnScore(t *testing.T) {
	stats := ChurnStats{Commits: 3, Additions: 150, Deletions: 50}
	// Expected: 3 + (150+50)/100.0 = 3 + 2.0 = 5.0
	got := stats.ChurnScore()
	if got != 5.0 {
		t.Errorf("ChurnScore() = %f, want 5.0", got)
	}
}

// TestParseNumstatOutput_CountsCommitsViaSentinel guards the BUG where
// --format= produced no blank-line separators, collapsing every commit
// into Commits=1. The %x00 sentinel marks each commit boundary.
func TestParseNumstatOutput_CountsCommitsViaSentinel(t *testing.T) {
	data := []byte("\x00\n1\t1\tfoo.go\n\x00\n2\t0\tfoo.go\n\x00\n1\t3\tbar.go\n")
	got := parseNumstatOutput(data)
	if got["foo.go"].Commits != 2 {
		t.Errorf("foo.go Commits: got %d, want 2", got["foo.go"].Commits)
	}
	if got["bar.go"].Commits != 1 {
		t.Errorf("bar.go Commits: got %d, want 1", got["bar.go"].Commits)
	}
	if got["foo.go"].Additions != 3 {
		t.Errorf("foo.go Additions: got %d, want 3", got["foo.go"].Additions)
	}
	if got["foo.go"].Deletions != 1 {
		t.Errorf("foo.go Deletions: got %d, want 1", got["foo.go"].Deletions)
	}
}
