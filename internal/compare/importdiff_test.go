package compare

import (
	"testing"
)

func TestComputeImportDiff(t *testing.T) {
	importsA := []string{"fmt", "net/http", "github.com/foo/bar", "github.com/shared/lib"}
	importsB := []string{"fmt", "os", "github.com/baz/qux", "github.com/shared/lib"}

	diff := ComputeImportDiff(importsA, importsB)

	if diff.CommonCount != 2 {
		t.Errorf("CommonCount = %d, want 2", diff.CommonCount)
	}
	if diff.OnlyACount != 2 {
		t.Errorf("OnlyACount = %d, want 2", diff.OnlyACount)
	}
	if diff.OnlyBCount != 2 {
		t.Errorf("OnlyBCount = %d, want 2", diff.OnlyBCount)
	}

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
