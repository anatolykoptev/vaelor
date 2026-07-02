package callgraph

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func TestTraceRepo_Integration(t *testing.T) {
	dir := t.TempDir()
	mainGo := `package main

func main() {
	result := compute(42)
	println(result)
}

func compute(x int) int {
	return transform(x) + 1
}

func transform(x int) int {
	return x * 2
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "main",
		Opts:   TraceOpts{Direction: "callees", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("TraceRepo: %v", err)
	}

	if result.Root == nil || result.Root.Name != "main" {
		t.Fatalf("root = %v, want main", result.Root)
	}
	if result.TotalNodes < 3 {
		t.Errorf("totalNodes = %d, want >= 3 (main, compute, transform)", result.TotalNodes)
	}
	if result.Unresolved < 1 {
		t.Errorf("unresolved = %d, want >= 1 (println)", result.Unresolved)
	}
}

func TestTraceRepo_Callers(t *testing.T) {
	dir := t.TempDir()
	mainGo := `package main

func main() {
	serve()
}

func serve() {
	handle()
}

func handle() {
	println("done")
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "handle",
		Opts:   TraceOpts{Direction: "callers", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("TraceRepo: %v", err)
	}

	if result.Root == nil || result.Root.Name != "handle" {
		t.Fatalf("root = %v, want handle", result.Root)
	}
	if result.TotalNodes < 2 {
		t.Errorf("totalNodes = %d, want >= 2", result.TotalNodes)
	}
}

func TestTraceRepo_SymbolNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "nonexistent",
		Opts:   TraceOpts{Direction: "callees", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Root != nil {
		t.Errorf("root should be nil for nonexistent symbol")
	}
}

func TestBuildFromRepo_GoTypesEnhanced(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/enhanced\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Interface + implementation — go/types resolves the dispatch.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func useGreeter(g Greeter) string {
	return g.Greet()
}

func main() {
	h := Hello{}
	useGreeter(h)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	if cg.Tier != "enhanced" {
		t.Errorf("expected tier 'enhanced', got %q", cg.Tier)
	}
	if len(cg.Edges) == 0 {
		t.Fatal("expected edges from go/types resolution")
	}

	// Verify main -> useGreeter edge exists.
	hasMainToUse := slices.ContainsFunc(cg.Edges, func(e CallEdge) bool {
		return e.Caller != nil && e.Caller.Name == "main" && e.CalleeName == "useGreeter"
	})
	if !hasMainToUse {
		t.Error("expected main->useGreeter edge")
	}
}

// TestBuildFromRepo_CloneRepoCalleesFiltered is a regression test for the
// callees noise issue: `understand`/`call_trace` on a CloneRepo-shaped
// function previously reported member access (`opts.Slug`, `opts.Ref`,
// `opts.DestDir`, `opts.GithubToken`), local variables (`ctx`, `localPath`,
// `slug`, `err`), and builtins (`append`, `string`) as callees. The default
// build (IncludeFieldAccess=false) must drop those while keeping real calls
// (`NormalizeSlug`, `refreshClone`, `os.Stat`, `os.MkdirAll`,
// `os.RemoveAll`, `filepath.Base`, `filepath.Join`, `strings.ReplaceAll`,
// `fmt.Sprintf`, `fmt.Errorf`, `exec.CommandContext`).
func TestBuildFromRepo_CloneRepoCalleesFiltered(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/cloneshape\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Mirror of internal/ingest/clone.go's CloneRepo — same call shape, same
	// field-access patterns. Standalone (no exec.Command etc.) so go/types
	// can resolve everything in-package.
	src := `package main

import (
	"fmt"
	"os"
)

type CloneOpts struct {
	Slug        string
	Ref         string
	DestDir     string
	GithubToken string
}

type CloneResult struct {
	LocalPath string
	Ref       string
}

func NormalizeSlug(s string) (string, error) { return s, nil }

func refreshClone(localPath, ref string) error { return nil }

func CloneRepo(ctx int, opts CloneOpts) (*CloneResult, error) {
	slug, err := NormalizeSlug(opts.Slug)
	if err != nil {
		return nil, err
	}

	localPath := opts.DestDir + "/" + slug

	if _, statErr := os.Stat(localPath); statErr == nil {
		if err := refreshClone(localPath, opts.Ref); err == nil {
			return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
		}
		_ = os.RemoveAll(localPath)
	}

	if err := os.MkdirAll(opts.DestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s.git", slug)
	if opts.GithubToken != "" {
		cloneURL = fmt.Sprintf("https://%s@github.com/%s.git", opts.GithubToken, slug)
	}
	_ = cloneURL
	_ = ctx

	return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
}

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	// Collect callees of CloneRepo from the tree-sitter tier (go/types tier
	// uses different selector resolution and doesn't emit argref entries
	// either, so this is independent of which tier wins the merge).
	calleeNames := map[string]bool{}
	for _, e := range cg.Edges {
		if e.Caller != nil && e.Caller.Name == "CloneRepo" {
			calleeNames[e.CalleeName] = true
		}
	}

	// Real calls — must be present.
	wantPresent := []string{"NormalizeSlug", "refreshClone", "Stat", "MkdirAll", "RemoveAll", "Sprintf", "Errorf"}
	for _, name := range wantPresent {
		if !calleeNames[name] {
			t.Errorf("expected callee %q from CloneRepo, got: %v", name, calleeNames)
		}
	}

	// Noise that must be dropped — member access of opts and locals.
	wantAbsent := []string{"Slug", "Ref", "DestDir", "GithubToken", "ctx", "localPath", "slug", "err", "dirPerm", "statErr", "cloneURL"}
	for _, name := range wantAbsent {
		if calleeNames[name] {
			t.Errorf("noise leaked into CloneRepo callees: %q (full set: %v)", name, calleeNames)
		}
	}
}

// TestBuildFromRepo_CloneRepoFieldAccessOptIn confirms that with
// IncludeFieldAccess=true, the legacy permissive behaviour is restored:
// unresolved argrefs (`opts.Slug` etc.) reappear as callees.
func TestBuildFromRepo_CloneRepoFieldAccessOptIn(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/cloneshape2\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `package main

type Opts struct{ Slug string }

func helper(s string) {}

func F(opts Opts) {
	helper(opts.Slug)
}

func main() { F(Opts{}) }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	// Bust cache so opt-in path isn't masked by default-path cache hit.
	InvalidateBuildCache()
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{
		Root:               dir,
		IncludeFieldAccess: true,
	})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	hasSlug := false
	for _, e := range cg.Edges {
		if e.Caller != nil && e.Caller.Name == "F" && e.CalleeName == "Slug" {
			hasSlug = true
		}
	}
	if !hasSlug {
		t.Errorf("with IncludeFieldAccess=true, expected `Slug` argref edge from F; edges=%+v", cg.Edges)
	}
}

// TestTryGoTypesResolution_WarnOnFailure verifies that TryGoTypesResolution
// emits a slog.Warn when packages.Load fails (e.g. no go.mod present).
// This gives operators a "why is my repo stuck at basic tier" signal.
func TestTryGoTypesResolution_WarnOnFailure(t *testing.T) {
	// A bare directory with no go.mod forces packages.Load to fail.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	prev := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(prev)

	// tempdir has no go.mod — LoadPackages errors immediately on missing module
	// file, before any context check is reached.
	result := TryGoTypesResolution(context.Background(), dir, nil)
	if result != nil {
		t.Error("expected nil result for failing packages.Load")
	}

	got := buf.String()
	if !strings.Contains(got, "go/packages load failed") {
		t.Errorf("expected warn log containing 'go/packages load failed'; got: %q", got)
	}
}

// TestBuildPrewarmEnv_ContainsCGODisabled verifies that buildPrewarmEnv includes
// CGO_ENABLED=0, which is required to prevent the prewarm go build from failing
// on missing tree-sitter C headers.
func TestBuildPrewarmEnv_ContainsCGODisabled(t *testing.T) {
	env := buildPrewarmEnv()
	if !slices.Contains(env, "CGO_ENABLED=0") {
		t.Errorf("buildPrewarmEnv() missing CGO_ENABLED=0; got: %v", env)
	}
	if !slices.Contains(env, "GOWORK=off") {
		t.Errorf("buildPrewarmEnv() missing GOWORK=off; got: %v", env)
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("buildPrewarmEnv() missing GIT_TERMINAL_PROMPT=0; got: %v", env)
	}
}

// TestTrySCIPResolution_GoIsNoop asserts that TrySCIPResolution returns nil for
// a Go-dominant file set. Go analysis is handled by go/types (goanalysis package)
// so scip-go was removed from the indexer registry. DetectIndexer("go") now
// returns false, causing TrySCIPResolution to return nil without invoking any
// external binary.
func TestTrySCIPResolution_GoIsNoop(t *testing.T) {
	files := []*ingest.File{
		{Path: "/tmp/main.go", Language: "go"},
		{Path: "/tmp/util.go", Language: "go"},
	}
	result := TrySCIPResolution(context.Background(), t.TempDir(), files, nil)
	if result != nil {
		t.Errorf("TrySCIPResolution for Go files returned non-nil %+v; expected no-op", result)
	}
}

func TestBuildFromRepo_FallbackToTreeSitter(t *testing.T) {
	dir := t.TempDir()

	// Python file only — no go.mod, so no go/types resolution.
	src := `def helper():
    pass

def main():
    helper()
`
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	if cg.Tier != "basic" {
		t.Errorf("expected tier 'basic', got %q", cg.Tier)
	}
}

// NOTE: the var-func-binding BUG A fixture (b) formerly here
// (TestBuildCallGraph_VarFuncBindingCalleeUnresolved) asserted against raw
// callgraph.BuildCallGraph — the one seam the fix (EnrichWithTypedResolution
// gated by CODEGRAPH_TYPED_ENRICH on the AGE-graph indexing path) deliberately
// does not touch, so it could never turn GREEN. It has been replaced by
// internal/codegraph/satisfaction_test.go's TestAGEGraphMissesVarFuncBindingCallee,
// which asserts against the actual fixed seam (buildAGECallGraph), mirroring
// TestAGEGraphMissesHomonymousPkgVarMethodCall (fixture (a)). See
// internal/goanalysis/resolver_hardred_test.go's TestResolve_VarFuncBindingAlias
// for the resolver-level proof that goanalysis.Resolve itself emits the edge.
