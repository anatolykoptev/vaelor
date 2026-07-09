package analyze

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/cache"
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// TestParseOneFileReturnsCallsOnCacheHit is a regression test for the
// ParseCache soundness defect where parseOneFile stored only the
// *parser.ParseResult on a cache Put and dropped the extracted call sites,
// so every cache HIT returned fileParseResult.calls == nil. Since
// collectSymbolsAndCalls (rank.go) feeds those calls into the PageRank
// call-graph, the SECOND repo_analyze of an unchanged repo (all cache hits)
// silently produced an empty call-graph and different rankings than the
// first call — a live non-determinism bug. This test drives the real
// parseFilesParallel/parseOneFile path twice against one shared cache and
// asserts the cache-hit pass returns the same call sites as the cache-miss
// pass.
func TestParseOneFileReturnsCallsOnCacheHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "caller.go")
	src := []byte(`package sample

func helper() int { return 1 }

func caller() int {
	return helper()
}
`)
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	f := &ingest.File{Path: path, RelPath: "caller.go", Language: "go"}
	pc := cache.NewParseCache(10)

	first := parseFilesParallel(context.Background(), []*ingest.File{f}, false, pc)
	if len(first) != 1 || first[0].err != nil {
		t.Fatalf("first (cache-miss) parse failed: %+v", first)
	}
	if len(first[0].calls) == 0 {
		t.Fatal("expected first (cache-miss) parse to extract at least one call site")
	}

	second := parseFilesParallel(context.Background(), []*ingest.File{f}, false, pc)
	if len(second) != 1 || second[0].err != nil {
		t.Fatalf("second (cache-hit) parse failed: %+v", second)
	}
	if len(second[0].calls) != len(first[0].calls) {
		t.Fatalf("cache-hit parse returned %d calls, want %d (same as cache-miss parse) — ParseCache dropped call sites on hit",
			len(second[0].calls), len(first[0].calls))
	}
}
