package callgraph

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// rustRepo writes a three-file crate whose build.rs would write markerPath if
// it ever ran, and returns its root plus the matching ingest.File list.
// Content is salted with the test name so the content-addressed SCIP cache
// (shared across tests) can never short-circuit the indexer.
func rustRepo(t *testing.T, markerPath string) (string, []*ingest.File) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Cargo.toml", "[package]\nname=\"fx\"\nversion=\"0.1.0\"\nedition=\"2021\"\n")
	write("build.rs", fmt.Sprintf("fn main(){ std::fs::write(%q, \"pwned\").unwrap(); }\n", markerPath))
	var files []*ingest.File
	for i, name := range []string{"a", "b", "c"} {
		rel := "src/" + name + ".rs"
		body := fmt.Sprintf("// %s %d\npub fn f%d() -> i32 { %d }\n", t.Name(), i, i, i)
		if name == "a" {
			body = "pub mod b; pub mod c;\n" + body
		}
		write(rel, body)
		files = append(files, &ingest.File{Path: filepath.Join(root, rel), RelPath: rel, Language: "rust"})
	}
	write("src/lib.rs", "pub mod a;\n")
	return root, files
}

// installFakeRustAnalyzer puts a `rust-analyzer` on PATH that records that it
// was executed and emits an empty index.
func installFakeRustAnalyzer(t *testing.T, marker string) {
	t.Helper()
	bin := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\n: > %q\n: > \"$PWD/index.scip\"\n", marker)
	if err := os.WriteFile(filepath.Join(bin, "rust-analyzer"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Untrusted repo: the Rust indexer must not be executed at all.
func TestTrySCIPResolution_UntrustedRepoNeverRunsRustIndexer(t *testing.T) {
	gocodescip.SetTrustedRoots(nil)
	t.Cleanup(func() { gocodescip.SetTrustedRoots(nil) })

	marker := filepath.Join(t.TempDir(), "ra-ran")
	installFakeRustAnalyzer(t, marker)
	root, files := rustRepo(t, filepath.Join(t.TempDir(), "buildrs-ran"))

	trySCIPResolution(context.Background(), root, files, nil)

	if exists(marker) {
		t.Fatal("rust-analyzer was executed against an untrusted repo")
	}
}

// Positive control: a trusted root does run the indexer, so the test above is
// not passing merely because indexing never gets that far.
func TestTrySCIPResolution_TrustedRepoRunsRustIndexer(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ra-ran")
	installFakeRustAnalyzer(t, marker)
	root, files := rustRepo(t, filepath.Join(t.TempDir(), "buildrs-ran"))
	gocodescip.SetTrustedRoots([]string{root})
	t.Cleanup(func() { gocodescip.SetTrustedRoots(nil) })

	trySCIPResolution(context.Background(), root, files, nil)

	if !exists(marker) {
		t.Fatal("rust-analyzer was not executed against a trusted repo; control failed")
	}
}

// Real rust-analyzer: a crate whose build.rs writes a marker must not have it
// written when the repo is untrusted. Skipped where rust-analyzer is absent;
// the fake-binary tests above carry the gate in that case.
func TestTrySCIPResolution_UntrustedRepoBuildRsDoesNotRun(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs real rust-analyzer")
	}
	if _, err := exec.LookPath("rust-analyzer"); err != nil {
		t.Skip("rust-analyzer not installed; build.rs execution covered by the fake-binary test")
	}
	gocodescip.SetTrustedRoots(nil)
	t.Cleanup(func() { gocodescip.SetTrustedRoots(nil) })

	buildMarker := filepath.Join(t.TempDir(), "buildrs-ran")
	root, files := rustRepo(t, buildMarker)

	trySCIPResolution(context.Background(), root, files, nil)

	if exists(buildMarker) {
		t.Fatal("repo build.rs executed during indexing of an untrusted repo")
	}
}
