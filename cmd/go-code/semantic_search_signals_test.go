package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSignalHitsLiveIntegration exercises buildSignalHits against the real AGE
// graph and the filesystem for the go-code repo. It is gated by DATABASE_URL.
//
// Practical check: hotspot and recency arms are not only wired but actually
// produce ranked hits from stored mtimes and symbol complexity.
func TestSignalHitsLiveIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("live integration test")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	defer pool.Close()

	graphStore := codegraph.NewStore(pool)

	root := "/host/src/go-code"
	repoKey := codegraph.GraphNameFor(root)

	candidates := []embeddings.GraphHit{
		{FilePath: "cmd/go-code/main.go", SymbolName: "main", SymbolKind: "function", Line: 79},
		{FilePath: "cmd/go-code/tool_semantic_search.go", SymbolName: "handleSemanticSearch", SymbolKind: "function", Line: 116},
		{FilePath: "internal/embeddings/rrf.go", SymbolName: "MergeRRF", SymbolKind: "function", Line: 98},
	}

	deps := SemanticDeps{
		GraphStore: graphStore,
		RRFWeights: embeddings.RRFWeights{Semantic: 1.0, Keyword: 1.0, Hotspot: 0.15, Recency: 0.1},
	}

	// Debug: inspect the raw data sources.
	churnMap, err := compare.CollectChurn(ctx, root, 0)
	if err != nil {
		t.Fatalf("CollectChurn: %v", err)
	}
	if churnMap == nil {
		t.Fatal("CollectChurn returned nil")
	}
	mtimes, err := graphStore.GetFileMtimes(ctx, repoKey)
	if err != nil {
		t.Fatalf("GetFileMtimes: %v", err)
	}
	complexityByFile, err := queryComplexityByFile(ctx, deps, repoKey)
	if err != nil {
		t.Fatalf("queryComplexityByFile: %v", err)
	}
	t.Logf("churn keys=%d mtime keys=%d complexity keys=%d", len(churnMap), len(mtimes), len(complexityByFile))
	t.Logf("candidate files: %s", func() string {
		files := make([]string, len(candidates))
		for i, c := range candidates {
			files[i] = c.FilePath
		}
		return strings.Join(files, " ")
	}())
	for _, c := range candidates {
		cs, okc := churnMap[c.FilePath]
		mt, okm := mtimes[c.FilePath]
		cx, okx := complexityByFile[c.FilePath]
		t.Logf("file=%s churn=%v/%v mtime=%v/%v complexity=%v/%v", c.FilePath, cs, okc, mt, okm, cx, okx)
	}

	hotspot, recency := buildSignalHits(ctx, deps, repoKey, root, candidates, 100)

	t.Logf("hotspot=%d recency=%d", len(hotspot), len(recency))

	if len(recency) == 0 {
		t.Fatal("recency arm produced no hits (code_file_mtimes missing or candidates not matched)")
	}
	if len(hotspot) == 0 {
		t.Fatal("hotspot arm produced no hits (churn/complexity missing or candidates not matched)")
	}

	// Recency should preserve the candidate with a known mtime.
	if recency[0].FilePath == "" {
		t.Fatal("recency leader has no file path")
	}

	// Hotspot should preserve at least one candidate whose file has churn/complexity.
	if hotspot[0].FilePath == "" {
		t.Fatal("hotspot leader has no file path")
	}
}
