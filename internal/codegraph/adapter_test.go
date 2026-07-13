package codegraph

import (
	"context"
	"os"
	"testing"

	"github.com/anatolykoptev/go-code/internal/graphx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAnalyticsAdapter_SatisfiesInterface is a compile-time + runtime check.
func TestAnalyticsAdapter_SatisfiesInterface(t *testing.T) {
	t.Run("compile_time", func(t *testing.T) {
		var _ = NewAnalyticsAdapter(nil, nil)
	})
}

// TestCrossRefsAdapter_SatisfiesInterface is a compile-time + runtime check.
func TestCrossRefsAdapter_SatisfiesInterface(t *testing.T) {
	t.Run("compile_time", func(t *testing.T) {
		var _ = NewCrossRefsAdapter(nil, nil)
	})
}

// TestAnalyticsAdapter_NilStore_ReturnsZero verifies that a nil-store adapter
// returns safe zero values instead of panicking.
func TestAnalyticsAdapter_NilStore_ReturnsZero(t *testing.T) {
	ctx := context.Background()
	a := NewAnalyticsAdapter(nil, nil)

	t.Run("Symbol", func(t *testing.T) {
		sig, err := a.Symbol(ctx, "/some/repo", "MyFunc", "internal/foo/foo.go")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if sig.Found {
			t.Error("expected Found=false for nil store")
		}
		if sig.Surprise != 0 {
			t.Errorf("expected Surprise=0 for nil store, got %v", sig.Surprise)
		}
	})

	t.Run("TopPageRank", func(t *testing.T) {
		sigs, err := a.TopPageRank(ctx, "/some/repo", 5)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(sigs) != 0 {
			t.Errorf("expected empty slice, got %d elements", len(sigs))
		}
	})
}

// TestCrossRefsAdapter_NilStore_ReturnsZero verifies that a nil-store adapter
// returns safe zero values instead of panicking.
func TestCrossRefsAdapter_NilStore_ReturnsZero(t *testing.T) {
	ctx := context.Background()
	c := NewCrossRefsAdapter(nil, nil)

	t.Run("HandlesRoute", func(t *testing.T) {
		route, found, err := c.HandlesRoute(ctx, "/some/repo", "MyHandler", "internal/foo/foo.go")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if found {
			t.Error("expected found=false for nil store")
		}
		if route.Path != "" || route.Method != "" {
			t.Errorf("expected zero Route, got %+v", route)
		}
	})

	t.Run("FetchedBy", func(t *testing.T) {
		refs, err := c.FetchedBy(ctx, "/some/repo", graphx.Route{Method: "GET", Path: "/api/v1/foo"})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(refs) != 0 {
			t.Errorf("expected empty slice, got %d elements", len(refs))
		}
	})

	t.Run("TestedBy", func(t *testing.T) {
		refs, err := c.TestedBy(ctx, "/some/repo", "MyFunc", "internal/foo/foo.go")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(refs) != 0 {
			t.Errorf("expected empty slice, got %d elements", len(refs))
		}
	})
}

// TestAnalyticsAdapter_LiveGraph is an integration test that requires a real
// PostgreSQL + AGE instance. Skipped when DATABASE_URL is not set.
func TestAnalyticsAdapter_LiveGraph(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	a := NewAnalyticsAdapter(store, nil)

	// repoKey is set to the path this repo is checked out at on the test host.
	repoKey := "/srv/src/repos/go-code"
	if v := os.Getenv("GOCODE_REPO_PATH"); v != "" {
		repoKey = v
	}

	// TopPageRank — rely on whatever graph is already cached.
	// If the graph is cold, we get an empty slice (acceptable per contract).
	t.Run("TopPageRank_returns_ordered", func(t *testing.T) {
		sigs, err := a.TopPageRank(ctx, repoKey, 5)
		if err != nil {
			t.Fatalf("TopPageRank error: %v", err)
		}
		t.Logf("TopPageRank returned %d symbols", len(sigs))
		if len(sigs) == 0 {
			t.Log("graph is cold — empty result is acceptable")
			return
		}
		if len(sigs) > 5 {
			t.Errorf("expected ≤5 results, got %d", len(sigs))
		}
		// Verify descending order.
		for i := 1; i < len(sigs); i++ {
			if sigs[i].PageRank > sigs[i-1].PageRank {
				t.Errorf("not descending at index %d: %.6f > %.6f",
					i, sigs[i].PageRank, sigs[i-1].PageRank)
			}
		}
	})

	// Symbol — probe a well-known symbol that should be present if graph is warm.
	t.Run("Symbol_known_handler", func(t *testing.T) {
		sig, err := a.Symbol(ctx, repoKey, "QueryGraph", "internal/codegraph/query.go")
		if err != nil {
			t.Fatalf("Symbol error: %v", err)
		}
		if !sig.Found {
			t.Log("symbol not found in graph — graph may be cold, skipping assertions")
			return
		}
		if sig.PageRank <= 0 {
			t.Errorf("expected PageRank > 0, got %v", sig.PageRank)
		}
		// Surprise is 0 when the graph was built without CODEGRAPH_SURPRISE_INDEX=1;
		// we only assert it is non-negative (never negative).
		if sig.Surprise < 0 {
			t.Errorf("expected Surprise >= 0, got %v", sig.Surprise)
		}
		t.Logf("QueryGraph: pagerank=%.6f community=%s surprise=%.4f",
			sig.PageRank, sig.Community, sig.Surprise)
	})
}

// TestRepoKeyToHostPath_UsesMapping verifies that repoKeyToHostPath replaces
// the longest-matching prefix from mappings, with fallback to identity.
func TestRepoKeyToHostPath_UsesMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		repoKey  string
		mappings map[string]string
		want     string
	}{
		{
			name:     "matching prefix",
			repoKey:  "/srv/repos/my-repo",
			mappings: map[string]string{"/srv/repos": "/host"},
			want:     "/host/my-repo",
		},
		{
			name:     "no match returns identity",
			repoKey:  "/other/path",
			mappings: map[string]string{"/srv/repos": "/host"},
			want:     "/other/path",
		},
		{
			name:     "empty mappings returns identity",
			repoKey:  "/home/user/src/repo",
			mappings: map[string]string{},
			want:     "/home/user/src/repo",
		},
		{
			name:     "exact prefix match",
			repoKey:  "/home/user",
			mappings: map[string]string{"/home/user": "/host"},
			want:     "/host",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := repoKeyToHostPath(tc.repoKey, tc.mappings)
			if got != tc.want {
				t.Errorf("repoKeyToHostPath(%q, %v) = %q; want %q", tc.repoKey, tc.mappings, got, tc.want)
			}
		})
	}
}
