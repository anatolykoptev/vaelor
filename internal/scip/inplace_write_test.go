package scip_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// A writable untrusted repo must never be indexed in place: the indexer
// writes <cwd>/index.scip and (scip-typescript --infer-tsconfig) tsconfig.json,
// and both writes follow repo symlinks to arbitrary files.
func TestRunIndexerSafe_WritableUntrustedRepoNeverWritesThroughSymlinks(t *testing.T) {
	gocodescip.SetTrustedRoots(nil)

	bin := t.TempDir()
	// Mimics scip-typescript: honours --output, otherwise writes ./index.scip,
	// and always writes ./tsconfig.json.
	script := `#!/bin/sh
out=index.scip
while [ $# -gt 0 ]; do
  if [ "$1" = "--output" ]; then out="$2"; shift; fi
  shift
done
printf 'SCIP-PROTOBUF' > "$out"
printf '{}' > tsconfig.json
`
	if err := os.WriteFile(filepath.Join(bin, "scip-typescript"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	const original = "do not touch"
	if err := os.WriteFile(victim, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	danglingTarget := filepath.Join(outside, "created-by-dangling-link")

	repo := t.TempDir() // writable
	mustLink := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(repo, name)); err != nil {
			t.Fatal(err)
		}
	}
	mustLink(victim, "index.scip")
	mustLink(danglingTarget, "tsconfig.json")
	if err := os.WriteFile(filepath.Join(repo, "a.ts"), []byte("export const a = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := gocodescip.RunIndexerSafe(context.Background(),
		gocodescip.IndexerConfig{Name: "scip-typescript", Args: []string{"index", "--infer-tsconfig"}}, repo)
	if err != nil {
		t.Fatalf("RunIndexerSafe: %v", err)
	}
	if res.Cleanup == nil {
		t.Error("untrusted writable repo was indexed in place (no temp copy to clean up)")
	} else {
		defer res.Cleanup()
	}

	if got, _ := os.ReadFile(victim); string(got) != original {
		t.Errorf("victim file outside the repo was overwritten: %q", got)
	}
	if _, err := os.Lstat(danglingTarget); err == nil {
		t.Error("dangling symlink target outside the repo was created")
	}
	// Positive control: the indexer ran and produced its index elsewhere.
	if b, err := os.ReadFile(res.IndexPath); err != nil || string(b) != "SCIP-PROTOBUF" {
		t.Errorf("index not produced at %s: %v %q", res.IndexPath, err, b)
	}
	if filepath.Dir(res.IndexPath) == repo {
		t.Errorf("index written inside the untrusted repo: %s", res.IndexPath)
	}
}
