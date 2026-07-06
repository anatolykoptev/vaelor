package envdetect_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/envdetect"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// findToolchain locates the toolchain rooted at workDir, failing the test if
// absent.
func findToolchain(t *testing.T, env *envdetect.Environment, workDir string) envdetect.Toolchain {
	t.Helper()
	for _, tc := range env.Toolchains {
		if tc.WorkDir == workDir {
			return tc
		}
	}
	t.Fatalf("no toolchain with workDir %q among %d toolchains", workDir, len(env.Toolchains))
	return envdetect.Toolchain{}
}

func commandKinds(cmds []envdetect.Command) []envdetect.CommandKind {
	kinds := make([]envdetect.CommandKind, len(cmds))
	for i, c := range cmds {
		kinds[i] = c.Kind
	}
	return kinds
}

func hasCommand(cmds []envdetect.Command, kind envdetect.CommandKind, source envdetect.CommandSource, argv ...string) bool {
	for _, c := range cmds {
		if c.Kind != kind || c.Source != source {
			continue
		}
		if len(c.Argv) != len(argv) {
			continue
		}
		match := true
		for i := range argv {
			if c.Argv[i] != argv[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestDetect_Go(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/foo\n\ngo 1.22\n")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(env.Toolchains) != 1 {
		t.Fatalf("want 1 toolchain, got %d: %+v", len(env.Toolchains), env.Toolchains)
	}
	tc := env.Toolchains[0]
	if tc.Language != "go" || tc.Manager != "go" || tc.WorkDir != "." {
		t.Fatalf("unexpected toolchain: %+v", tc)
	}
	if !hasCommand(tc.Commands, envdetect.KindBuild, envdetect.SourceConvention, "go", "build", "./...") {
		t.Errorf("missing go build convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceConvention, "go", "test", "./...") {
		t.Errorf("missing go test convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindLint, envdetect.SourceConvention, "go", "vet", "./...") {
		t.Errorf("missing go vet convention command: %+v", tc.Commands)
	}
	if env.Polyglot {
		t.Errorf("single-language repo should not be polyglot")
	}
}

func TestDetect_NPM_FullScripts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{
		"name": "demo",
		"scripts": {"build": "tsc", "test": "jest", "lint": "eslint ."}
	}`)

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")
	if tc.Manager != "npm" {
		t.Errorf("want npm manager (no lockfile), got %q", tc.Manager)
	}
	if !hasCommand(tc.Commands, envdetect.KindInstall, envdetect.SourceConvention, "npm", "install") {
		t.Errorf("missing npm install convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindBuild, envdetect.SourceManifest, "npm", "run", "build") {
		t.Errorf("missing npm run build manifest command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceManifest, "npm", "run", "test") {
		t.Errorf("missing npm run test manifest command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindLint, envdetect.SourceManifest, "npm", "run", "lint") {
		t.Errorf("missing npm run lint manifest command: %+v", tc.Commands)
	}
}

// TestDetect_NPM_PartialScripts is required case (a): only "test" is
// declared — asserts no fabricated build command appears.
func TestDetect_NPM_PartialScripts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "demo", "scripts": {"test": "jest"}}`)

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")

	for _, kind := range commandKinds(tc.Commands) {
		if kind == envdetect.KindBuild {
			t.Fatalf("no build script declared — must not fabricate a build command: %+v", tc.Commands)
		}
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceManifest, "npm", "run", "test") {
		t.Errorf("missing npm run test manifest command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindInstall, envdetect.SourceConvention, "npm", "install") {
		t.Errorf("install must still be emitted as convention even with a partial scripts object: %+v", tc.Commands)
	}
}

func TestDetect_NPM_YarnLockfile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name": "demo", "scripts": {"build": "vite build"}}`)
	writeFile(t, filepath.Join(root, "yarn.lock"), "")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")
	if tc.Manager != "yarn" {
		t.Errorf("want yarn manager, got %q", tc.Manager)
	}
	if !hasCommand(tc.Commands, envdetect.KindInstall, envdetect.SourceConvention, "yarn", "install") {
		t.Errorf("missing yarn install convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindBuild, envdetect.SourceManifest, "yarn", "run", "build") {
		t.Errorf("missing yarn run build manifest command: %+v", tc.Commands)
	}
}

func TestDetect_Cargo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")
	if tc.Language != "rust" || tc.Manager != "cargo" {
		t.Fatalf("unexpected toolchain: %+v", tc)
	}
	if !hasCommand(tc.Commands, envdetect.KindBuild, envdetect.SourceConvention, "cargo", "build") {
		t.Errorf("missing cargo build convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceConvention, "cargo", "test") {
		t.Errorf("missing cargo test convention command: %+v", tc.Commands)
	}
}

func TestDetect_Python_PoetryScripts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), `[tool.poetry]
name = "demo"
version = "0.1.0"

[tool.poetry.scripts]
test = "demo.cli:test"
lint = "demo.cli:lint"
`)

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")
	if tc.Manager != "poetry" {
		t.Errorf("want poetry manager, got %q", tc.Manager)
	}
	if !hasCommand(tc.Commands, envdetect.KindInstall, envdetect.SourceConvention, "poetry", "install") {
		t.Errorf("missing poetry install convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceManifest, "poetry", "run", "test") {
		t.Errorf("missing poetry run test manifest command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindLint, envdetect.SourceManifest, "poetry", "run", "lint") {
		t.Errorf("missing poetry run lint manifest command: %+v", tc.Commands)
	}
	// A ground-truth "test" script exists — pytest must not also be
	// fabricated as a fallback.
	testCount := 0
	for _, c := range tc.Commands {
		if c.Kind == envdetect.KindTest {
			testCount++
		}
	}
	if testCount != 1 {
		t.Errorf("want exactly 1 test command when a manifest script covers it, got %d: %+v", testCount, tc.Commands)
	}
}

func TestDetect_Python_RequirementsOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "requirements.txt"), "flask==3.0.0\n")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")
	if tc.Manager != "pip" {
		t.Errorf("want pip manager, got %q", tc.Manager)
	}
	if !hasCommand(tc.Commands, envdetect.KindInstall, envdetect.SourceConvention, "pip", "install", "-r", "requirements.txt") {
		t.Errorf("missing pip install -r requirements.txt convention command: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceConvention, "pytest") {
		t.Errorf("missing pytest convention fallback command: %+v", tc.Commands)
	}
	for _, kind := range commandKinds(tc.Commands) {
		if kind == envdetect.KindBuild {
			t.Fatalf("pure Python project must not fabricate a build command: %+v", tc.Commands)
		}
	}
}

// TestDetect_MakefileOverridesGoConvention is required case (b): Makefile +
// go.mod in the same directory — Makefile targets must win for overlapping
// kinds.
func TestDetect_MakefileOverridesGoConvention(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/foo\n\ngo 1.22\n")
	writeFile(t, filepath.Join(root, "Makefile"), "build:\n\tgo build -o bin/foo ./...\n\ntest:\n\tgo test ./... -race\n")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(env.Toolchains) != 1 {
		t.Fatalf("want 1 toolchain (Makefile augments the go toolchain in the same dir), got %d: %+v", len(env.Toolchains), env.Toolchains)
	}
	tc := env.Toolchains[0]

	if !hasCommand(tc.Commands, envdetect.KindBuild, envdetect.SourceManifest, "make", "build") {
		t.Errorf("Makefile build target must win over the go convention: %+v", tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceManifest, "make", "test") {
		t.Errorf("Makefile test target must win over the go convention: %+v", tc.Commands)
	}
	// The convention build/test commands must be gone, not merely
	// shadowed — assert only one command per overridden kind.
	buildCount, testCount, lintCount := 0, 0, 0
	for _, c := range tc.Commands {
		switch c.Kind {
		case envdetect.KindBuild:
			buildCount++
		case envdetect.KindTest:
			testCount++
		case envdetect.KindLint:
			lintCount++
		}
	}
	if buildCount != 1 {
		t.Errorf("want exactly 1 build command after override, got %d: %+v", buildCount, tc.Commands)
	}
	if testCount != 1 {
		t.Errorf("want exactly 1 test command after override, got %d: %+v", testCount, tc.Commands)
	}
	// lint has no Makefile target — the go vet convention must survive.
	if lintCount != 1 {
		t.Errorf("want exactly 1 lint command (untouched go convention), got %d: %+v", lintCount, tc.Commands)
	}
	if !hasCommand(tc.Commands, envdetect.KindLint, envdetect.SourceConvention, "go", "vet", "./...") {
		t.Errorf("go vet convention must survive when Makefile has no lint target: %+v", tc.Commands)
	}
}

func TestDetect_StandaloneMakefile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Makefile"), "install:\n\t./setup.sh\n\ncheck:\n\t./run-tests.sh\n")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	tc := findToolchain(t, env, ".")
	if tc.Manager != "make" {
		t.Errorf("want make manager for a standalone Makefile, got %q", tc.Manager)
	}
	if !hasCommand(tc.Commands, envdetect.KindInstall, envdetect.SourceManifest, "make", "install") {
		t.Errorf("missing make install command: %+v", tc.Commands)
	}
	// "check" is an alias for KindTest per this package's documented
	// convention (no Kind named "check" exists).
	if !hasCommand(tc.Commands, envdetect.KindTest, envdetect.SourceManifest, "make", "check") {
		t.Errorf("missing make check->test alias command: %+v", tc.Commands)
	}
}

// TestDetect_Polyglot is required case (c): go.mod at the root, package.json
// in a subdirectory — two Toolchains with correct WorkDir each.
func TestDetect_Polyglot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/foo\n\ngo 1.22\n")
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"name": "web", "scripts": {"build": "vite build", "test": "vitest"}}`)

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(env.Toolchains) != 2 {
		t.Fatalf("want 2 toolchains, got %d: %+v", len(env.Toolchains), env.Toolchains)
	}
	if !env.Polyglot {
		t.Errorf("2 distinct languages must be reported as polyglot")
	}

	goTC := findToolchain(t, env, ".")
	if goTC.Language != "go" {
		t.Errorf("root toolchain should be go, got %+v", goTC)
	}
	webTC := findToolchain(t, env, "web")
	if webTC.Language != "typescript" {
		t.Errorf("web toolchain should be typescript, got %+v", webTC)
	}
	if !hasCommand(webTC.Commands, envdetect.KindBuild, envdetect.SourceManifest, "npm", "run", "build") {
		t.Errorf("web toolchain missing its own build command: %+v", webTC.Commands)
	}
}

// TestDetect_NoManifests is required case (d): a repo with none of the
// recognized manifests — Detect must return an empty (not nil-panic)
// Environment without error.
func TestDetect_NoManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# demo\n")

	env, err := envdetect.Detect(context.Background(), root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if env == nil {
		t.Fatal("Detect returned a nil Environment with no error")
	}
	if len(env.Toolchains) != 0 {
		t.Errorf("want 0 toolchains, got %d: %+v", len(env.Toolchains), env.Toolchains)
	}
	if env.Polyglot {
		t.Errorf("empty repo cannot be polyglot")
	}
}

func TestDetect_ContextCanceled(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := envdetect.Detect(ctx, root); err == nil {
		t.Fatal("want error for an already-canceled context")
	}
}
