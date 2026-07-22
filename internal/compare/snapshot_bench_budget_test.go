package compare_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/cache"
	"github.com/anatolykoptev/vaelor/internal/compare"
)

// BenchmarkBuildSnapshot_WithParseCache measures the cache-hit path: first
// call populates the cache, subsequent calls should hit. This verifies
// whether the ParseCache actually short-circuits re-parsing on the
// code_compare hot path (#572 profiling).
func BenchmarkBuildSnapshot_WithParseCache(b *testing.B) {
	root := findRepoRootTB(b)
	opts := compare.SnapshotOpts{
		Focus:      filepath.Join("internal", "parser"),
		ParseCache: cache.NewParseCache(cache.DefaultParseCacheSize),
	}
	slog.SetLogLoggerLevel(slog.LevelError)

	// Warm the cache with one call (not counted).
	if _, err := compare.BuildSnapshot(context.Background(), root, opts); err != nil {
		b.Fatalf("warmup BuildSnapshot: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := compare.BuildSnapshot(context.Background(), root, opts); err != nil {
			b.Fatalf("BuildSnapshot: %v", err)
		}
	}
}

// BenchmarkBuildSnapshot_FullRepo measures a full-repo snapshot (no focus)
// to estimate the cost on a large repo — the code_compare hot path.
func BenchmarkBuildSnapshot_FullRepo(b *testing.B) {
	root := findRepoRootTB(b)
	opts := compare.SnapshotOpts{}
	slog.SetLogLoggerLevel(slog.LevelError)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := compare.BuildSnapshot(context.Background(), root, opts); err != nil {
			b.Fatalf("BuildSnapshot: %v", err)
		}
	}
}
