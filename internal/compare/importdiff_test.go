package compare

import (
	"testing"
)

func TestComputeImportDiff(t *testing.T) {
	importsA := []string{"fmt", "net/http", "github.com/foo/bar", "github.com/shared/lib"}
	importsB := []string{"fmt", "os", "github.com/baz/qux", "github.com/shared/lib"}

	diff := ComputeImportDiff(importsA, importsB, "go")

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
	diff := ComputeImportDiff(nil, nil, "go")
	if diff.CommonCount != 0 || diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Errorf("expected all zeros for empty imports, got %+v", diff)
	}
}

func TestComputeImportDiff_Identical(t *testing.T) {
	imports := []string{"fmt", "os", "net/http"}
	diff := ComputeImportDiff(imports, imports, "go")
	if diff.CommonCount != 3 {
		t.Errorf("CommonCount = %d, want 3", diff.CommonCount)
	}
	if diff.OnlyACount != 0 || diff.OnlyBCount != 0 {
		t.Error("identical imports should have 0 only-A and only-B")
	}
}

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
	if diff.StdlibB != 2 {
		t.Errorf("StdlibB = %d, want 2", diff.StdlibB)
	}
	if diff.ExternalB != 1 {
		t.Errorf("ExternalB = %d, want 1", diff.ExternalB)
	}
	if len(diff.FrameworksA) == 0 || diff.FrameworksA[0] != "gin" {
		t.Errorf("FrameworksA = %v, want [gin]", diff.FrameworksA)
	}
	if len(diff.FrameworksB) == 0 || diff.FrameworksB[0] != "echo" {
		t.Errorf("FrameworksB = %v, want [echo]", diff.FrameworksB)
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
