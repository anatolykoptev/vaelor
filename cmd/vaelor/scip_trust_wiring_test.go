package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// The production wiring (registerTools -> scipTrustedRoots) must not trust an
// auto-index dir that contains the clone workspace or the temp dir.
func TestScipTrustedRoots_DropsOverlaps(t *testing.T) {
	base := t.TempDir()
	good := filepath.Join(base, "good")
	withWorkspace := filepath.Join(base, "withws")
	workspace := filepath.Join(withWorkspace, "clones")
	for _, d := range []string{good, workspace} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// Keep os.TempDir() disjoint from the fixture (t.TempDir lives under it).
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "sys-tmp"))
	if err := os.MkdirAll(os.TempDir(), 0o750); err != nil {
		t.Fatal(err)
	}

	cfg := Config{AutoIndexDirs: []string{good, withWorkspace}, WorkspaceDir: workspace}
	if got := scipTrustedRoots(cfg); !slices.Equal(got, []string{good}) {
		t.Errorf("WORKSPACE_DIR inside a trusted dir: got %v, want only %v", got, good)
	}

	// A trusted dir that contains os.TempDir() (PR worktrees) is dropped too.
	t.Setenv("TMPDIR", filepath.Join(good, "tmp"))
	if err := os.MkdirAll(filepath.Join(good, "tmp"), 0o750); err != nil {
		t.Fatal(err)
	}
	cfg = Config{AutoIndexDirs: []string{good}, WorkspaceDir: filepath.Join(base, "elsewhere")}
	if got := scipTrustedRoots(cfg); len(got) != 0 {
		t.Errorf("TempDir inside a trusted dir: got %v, want none", got)
	}
}

// The same wiring (registerTools -> installRootTrust) must hand the operator's
// trust list to the Go typed-load env: an auto-index checkout keeps the inherited
// env (private-module credentials, cgo), any other root gets the allowlist.
//
// Mutation that must turn it RED: delete goanalysis.SetRootTrust(...) in
// installRootTrust (register.go), or the installRootTrust call in registerTools.
func TestInstallRootTrust_WiresGoEnv(t *testing.T) {
	base := t.TempDir()
	trusted := filepath.Join(base, "src")
	repo := filepath.Join(trusted, "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	// Keep os.TempDir() disjoint from the trusted dir (t.TempDir lives under it).
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "sys-tmp"))
	if err := os.MkdirAll(os.TempDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAELOR_CANARY_SECRET", "s3cr3t-canary")

	installRootTrust(Config{AutoIndexDirs: []string{trusted}, WorkspaceDir: filepath.Join(base, "ws")})
	t.Cleanup(func() {
		gocodescip.SetTrustedRoots(nil)
		goanalysis.SetRootTrust(nil)
	})

	has := func(dir string) bool {
		for _, kv := range goanalysis.GoEnv(dir) {
			if strings.HasPrefix(kv, "VAELOR_CANARY_SECRET=") {
				return true
			}
		}
		return false
	}
	if !has(repo) {
		t.Error("auto-index checkout lost the inherited env: Go trust is not wired")
	}
	if has(t.TempDir()) {
		t.Error("untrusted root received the server env")
	}
}
