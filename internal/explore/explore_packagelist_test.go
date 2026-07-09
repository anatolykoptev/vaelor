package explore

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func TestBuildPackageList_FiltersNonCodeDirs(t *testing.T) {
	t.Parallel()
	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/cmd/svc/main.go", Language: "go"},
		{Path: "/repo/internal/auth/auth.go", Language: "go"},
		{Path: "/repo/.github/workflows/ci.yml", Language: ""},
		{Path: "/repo/docs/plan.md", Language: ""},
		{Path: "/repo/docs/plans/oct.md", Language: ""},
		{Path: "/repo/policies/audit.rego", Language: ""},
	}

	pkgs := buildPackageList(files, root)
	got := strings.Join(pkgs, ",")

	for _, want := range []string{"cmd/svc", "internal/auth"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in packages, got: %s", want, got)
		}
	}
	for _, unwanted := range []string{".github", "docs", "policies"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("non-code dir %q must be filtered, got: %s", unwanted, got)
		}
	}
}
