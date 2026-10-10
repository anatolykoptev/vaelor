package scip_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// writeFakeIndexer installs an executable named `name` on PATH that dumps its
// environment into <cwd>/env.dump and emits an empty index.scip.
func writeFakeIndexer(t *testing.T, name string) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\nenv > \"$PWD/env.dump\"\n: > \"$PWD/index.scip\"\n"
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake indexer: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRunIndexer_DoesNotInheritServerEnv drives the real exec path with canary
// secrets set in the parent and asserts none reach the indexer child.
func TestRunIndexer_DoesNotInheritServerEnv(t *testing.T) {
	canaries := map[string]string{
		"VAELOR_CANARY_SECRET":    "canary-value-1",
		"GITHUB_TOKEN":            "canary-gh",
		"DATABASE_URL":            "postgres://canary",
		"INTERNAL_SERVICE_SECRET": "canary-internal",
		"LLM_API_KEY":             "canary-llm",
	}
	for k, v := range canaries {
		t.Setenv(k, v)
	}
	const fake = "fake-scip-indexer-env"
	writeFakeIndexer(t, fake)

	dir := t.TempDir()
	if _, err := gocodescip.RunIndexer(context.Background(), gocodescip.IndexerConfig{Name: fake}, dir); err != nil {
		t.Fatalf("RunIndexer: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "env.dump"))
	if err != nil {
		t.Fatalf("read env.dump: %v", err)
	}
	dump := string(raw)

	for k, v := range canaries {
		if strings.Contains(dump, k+"=") || strings.Contains(dump, v) {
			t.Errorf("indexer child inherited %s", k)
		}
	}
	// Positive control: the dump is a real env, PATH survives, HOME is the
	// throwaway dir and not the server's home.
	if !strings.Contains(dump, "PATH=") {
		t.Errorf("PATH missing from child env; dump=%q", dump)
	}
	home := envValue(dump, "HOME")
	if home == "" || home == os.Getenv("HOME") {
		t.Errorf("child HOME = %q, want a throwaway dir distinct from server HOME", home)
	}
}

func envValue(dump, key string) string {
	for _, line := range strings.Split(dump, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	return ""
}
