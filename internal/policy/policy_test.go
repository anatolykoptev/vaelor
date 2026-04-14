package policy

import (
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/review"
)

func TestLoadPolicy(t *testing.T) {
	p, err := Load("testdata")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Rules.MaxFuncLines != 100 {
		t.Fatalf("max func lines: got %d", p.Rules.MaxFuncLines)
	}
	if len(p.Rules.ForbiddenImports) != 1 {
		t.Fatal("want 1 forbidden import rule")
	}
}

func TestApplyForbiddenImports(t *testing.T) {
	p, err := Load("testdata")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Fake changed files containing an offending import.
	r := &review.DeltaResult{
		ChangedFiles: []review.FileDiff{{Path: "foo.go", Added: 3, Removed: 0}},
	}
	findings := p.Apply(r, func(path string) string {
		if path == "foo.go" {
			return `package x
import "github.com/pkg/errors"
func f() {}
`
		}
		return ""
	})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Path != "foo.go" {
		t.Fatalf("bad path: %s", findings[0].Path)
	}
	_ = filepath.Separator // keep import used in real impl
}
