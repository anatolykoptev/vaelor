package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestBuildTestedSet_FrontendConventions verifies that .test./.spec. file infixes
// in svelte/astro/ts/js symbol paths cause the stem to be marked as tested.
func TestBuildTestedSet_FrontendConventions(t *testing.T) {
	type tc struct {
		name     string
		language string
		file     string
		symName  string
		wantStem string // non-empty → must be in tested set
		wantMiss string // non-empty → must NOT be in tested set
	}
	cases := []tc{
		{
			name:     "typescript test file marks stem",
			language: "typescript",
			file:     "/repo/src/components/Button.test.ts",
			symName:  "describe",
			wantStem: "Button",
		},
		{
			name:     "svelte spec file marks stem",
			language: "svelte",
			file:     "/repo/src/components/Modal.spec.svelte",
			symName:  "it",
			wantStem: "Modal",
		},
		{
			name:     "astro test file marks stem",
			language: "astro",
			file:     "/repo/src/pages/Layout.test.astro",
			symName:  "test",
			wantStem: "Layout",
		},
		{
			name:     "javascript spec file marks stem",
			language: "javascript",
			file:     "/repo/src/utils/script.spec.js",
			symName:  "it",
			wantStem: "script",
		},
		{
			name:     "tsx test file marks stem",
			language: "typescript",
			file:     "/repo/src/components/Button.test.tsx",
			symName:  "describe",
			wantStem: "Button",
		},
		{
			name:     "plain ts file does not mark any stem",
			language: "typescript",
			file:     "/repo/src/components/random.ts",
			symName:  "random",
			wantMiss: "random",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			syms := []*parser.Symbol{
				{Name: c.symName, Language: c.language, File: c.file},
			}
			got := buildTestedSet(syms)
			if c.wantStem != "" && !got[c.wantStem] {
				t.Errorf("stem %q not in tested set; got %v", c.wantStem, got)
			}
			if c.wantMiss != "" && got[c.wantMiss] {
				t.Errorf("stem %q should not be in tested set", c.wantMiss)
			}
		})
	}
}

func TestDeltaReview(t *testing.T) {
	dir := setupGitRepoWithSymbols(t)
	result, err := DeltaReview(context.Background(), DeltaInput{
		Root:  dir,
		Base:  "HEAD~1",
		Depth: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedFiles) == 0 {
		t.Error("expected changed files")
	}
	if result.Risk.RiskLevel == "" {
		t.Error("expected risk level")
	}
}

func setupGitRepoWithSymbols(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	src := "package main\n\nfunc ProcessOrder(id int) error {\n\treturn nil\n}\n\nfunc Helper() string {\n\treturn \"help\"\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	src2 := "package main\n\nimport \"fmt\"\n\nfunc ProcessOrder(id int) error {\n\tif id <= 0 {\n\t\treturn fmt.Errorf(\"invalid id\")\n\t}\n\treturn nil\n}\n\nfunc Helper() string {\n\treturn \"help\"\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644)
	run("add", ".")
	run("commit", "-m", "validate id")

	return dir
}
