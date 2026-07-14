package compare_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/compare"
)

// findRepoRootTB is findRepoRoot (snapshot_test.go) generalized over
// testing.TB so it also serves *testing.B; the original is bound to
// *testing.T specifically.
func findRepoRootTB(tb testing.TB) string {
	tb.Helper()

	dir, err := os.Getwd()
	if err != nil {
		tb.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("go.mod not found — cannot locate repo root")
		}
		dir = parent
	}
}

// BenchmarkBuildSnapshot benchmarks compare.BuildSnapshot scoped to
// internal/parser (mirrors TestBuildSnapshotWithFocus in snapshot_test.go),
// not the whole repo: a full-repo build is the "heavy integration test" that
// TestBuildSnapshot itself skips under testing.Short() (snapshot_test.go:36).
// No ParseCache is passed — BuildSnapshot needs no DB/embed dependency, only
// a filesystem walk (internal/ingest.IngestRepo) plus parser.ParseFile per
// file, so nothing here requires a b.Skip.
func BenchmarkBuildSnapshot(b *testing.B) {
	root := findRepoRootTB(b)
	opts := compare.SnapshotOpts{Focus: filepath.Join("internal", "parser")}

	// Mute per-iteration ingest INFO logs so benchstat -benchtime=Nx output
	// is not flooded with N "starting repo walk" lines.
	slog.SetLogLoggerLevel(slog.LevelError)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := compare.BuildSnapshot(context.Background(), root, opts); err != nil {
			b.Fatalf("BuildSnapshot: %v", err)
		}
	}
}
