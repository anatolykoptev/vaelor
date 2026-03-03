package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestCollectOutliers(t *testing.T) {
	snap := &RepoSnapshot{
		Root: "/repo",
		Symbols: []*parser.Symbol{
			{
				Name: "Simple", Kind: parser.KindFunction,
				File: "/repo/a.go", StartLine: 1, EndLine: 5,
				Body: "x := 1", Language: "go",
			},
			{
				Name: "Complex", Kind: parser.KindFunction,
				File: "/repo/pkg/b.go", StartLine: 10, EndLine: 80,
				Body: "if a { if b { if c { for i := range x { if d { } } } } }", Language: "go",
			},
		},
	}

	out := CollectOutliers(snap)

	if out.MaxCyclomatic.Name != "Complex" {
		t.Errorf("MaxCyclomatic: got %q, want Complex", out.MaxCyclomatic.Name)
	}
	if out.MaxCyclomatic.File != "pkg/b.go" {
		t.Errorf("MaxCyclomatic.File: got %q, want pkg/b.go", out.MaxCyclomatic.File)
	}
	if out.MaxFuncLines.Name != "Complex" {
		t.Errorf("MaxFuncLines: got %q, want Complex", out.MaxFuncLines.Name)
	}
	if out.MaxFuncLines.Value != 71 {
		t.Errorf("MaxFuncLines.Value: got %d, want 71", out.MaxFuncLines.Value)
	}
	if out.MaxNesting.Name != "Complex" {
		t.Errorf("MaxNesting: got %q, want Complex", out.MaxNesting.Name)
	}
}

func TestCollectOutliers_Empty(t *testing.T) {
	snap := &RepoSnapshot{Root: "/repo"}
	out := CollectOutliers(snap)
	if out.MaxCyclomatic.Name != "" {
		t.Errorf("expected empty outliers for empty snapshot, got %+v", out)
	}
}

func TestCollectOutliers_SkipsNonFunctions(t *testing.T) {
	snap := &RepoSnapshot{
		Root: "/repo",
		Symbols: []*parser.Symbol{
			{Name: "MyStruct", Kind: parser.KindStruct, File: "/repo/a.go"},
			{Name: "MyInterface", Kind: parser.KindInterface, File: "/repo/a.go"},
		},
	}
	out := CollectOutliers(snap)
	if out.MaxCyclomatic.Name != "" {
		t.Errorf("expected empty outliers for non-function symbols")
	}
}
