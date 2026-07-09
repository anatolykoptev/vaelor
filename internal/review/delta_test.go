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
	t.Parallel()
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

// TestBuildTestedSet_Swift_TestPrefix verifies that a Swift function whose name
// starts with "test" (XCTest convention) is detected as a test function and its
// name is marked in the tested set.
// Relies on the Swift case in buildTestedSet (internal/review/delta.go).
func TestBuildTestedSet_Swift_TestPrefix(t *testing.T) {
	t.Parallel()
	syms := []*parser.Symbol{
		// production symbol
		{Name: "fetchOrder", Language: "swift", File: "OrderService.swift", Kind: parser.KindFunction},
		// XCTest function: func testFetchOrder() — name starts with "test"
		{Name: "testFetchOrder", Language: "swift", File: "OrderServiceTests.swift", Kind: parser.KindMethod},
	}
	tested := buildTestedSet(syms)
	// XCTest names the test function directly; the tested set records the test function name.
	if !tested["testFetchOrder"] {
		t.Error("expected testFetchOrder to be in tested set (Swift XCTest name-prefix convention)")
	}
}

// TestBuildTestedSet_Swift_NoTestPrefix verifies that a Swift function whose name
// does NOT start with "test" is not detected as an XCTest function.
// Relies on the Swift case in buildTestedSet (internal/review/delta.go).
func TestBuildTestedSet_Swift_NoTestPrefix(t *testing.T) {
	t.Parallel()
	syms := []*parser.Symbol{
		// helper function in a test file — not a test itself
		{Name: "makeOrder", Language: "swift", File: "OrderServiceTests.swift", Kind: parser.KindFunction},
	}
	tested := buildTestedSet(syms)
	if tested["makeOrder"] {
		t.Error("makeOrder should not be in tested set — no 'test' prefix (Swift XCTest convention)")
	}
}

// TestBuildTestedSet_Kotlin_AtTest verifies that a Kotlin function whose
// Attributes slice contains "@Test" (JUnit 4/5 annotation) is detected as a
// test function and its name marked in the tested set.
// Relies on the "@Test" branch in buildTestedSet (internal/review/delta.go).
func TestBuildTestedSet_Kotlin_AtTest(t *testing.T) {
	t.Parallel()
	syms := []*parser.Symbol{
		// production symbol
		{Name: "processPayment", Language: "kotlin", File: "Payment.kt", Kind: parser.KindFunction},
		// test function with @Test annotation
		{Name: "processPayment", Language: "kotlin", File: "PaymentTest.kt", Kind: parser.KindFunction,
			Attributes: []string{"@Test"}},
	}
	tested := buildTestedSet(syms)
	if !tested["processPayment"] {
		t.Error("expected processPayment to be in tested set when @Test attribute present")
	}
}

// TestBuildTestedSet_Kotlin_NoAnnotation verifies that a Kotlin function WITHOUT
// a @Test annotation is NOT automatically treated as a test by name prefix alone
// (prefix-based detection is Go/Python convention, not Kotlin/JUnit).
func TestBuildTestedSet_Kotlin_NoAnnotation(t *testing.T) {
	t.Parallel()
	syms := []*parser.Symbol{
		// name starts with "test" — valid in Go, not in Kotlin without annotation
		{Name: "testSomething", Language: "kotlin", File: "Foo.kt", Kind: parser.KindFunction},
	}
	tested := buildTestedSet(syms)
	// Kotlin without @Test annotation should NOT be in tested set via name-prefix
	if tested["Something"] {
		t.Error("Kotlin name-prefix 'test' should not mark 'Something' as tested (use @Test annotation)")
	}
}

func TestDeltaReview(t *testing.T) {
	t.Parallel()
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
