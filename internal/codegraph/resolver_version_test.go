package codegraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/ingest"
)

// A graph persisted by a binary with an older call resolver reads as stale even
// when the repo is byte-for-byte unchanged and inside its TTL. Before the
// resolver version was part of the fingerprint a release that changed which
// edges are produced left the old edges in the AGE graph for up to
// GRAPH_TTL_REMOTE (24h): content hash and BuiltAt are both inputs a binary
// upgrade does not change.
func TestGraphCache_OldResolverVersionReadsStale(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := func(hash string) *GraphMeta {
		return &GraphMeta{BuiltAt: time.Now().UTC(), TTLSeconds: 3600, ContentHash: hash}
	}

	current := contentFingerprint(root)
	if got := classifyGraphCache(fresh(current), current); got != "hit" {
		t.Fatalf("graph built by this binary must be a hit, got %q", got)
	}
	// What the previous binary persisted: the bare content hash, no version.
	legacy := ingest.RepoContentHash(root)
	if got := classifyGraphCache(fresh(legacy), contentFingerprint(root)); got != "stale" {
		t.Errorf("graph built by an older resolver must read stale, got %q", got)
	}
}
