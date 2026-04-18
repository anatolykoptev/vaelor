//go:build !nointegration
// +build !nointegration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/go-code/internal/learnings"
)

// TestE2E_ReviewPersistToStore_UnderstandReads verifies the composition
// between Task 10 (persistChangedSymbols → Store.Upsert) and Task 9
// (fetchPriorLearnings → Store.Nearest): records written under one
// (repo, symbol) key are discoverable by Nearest under the same key.
//
// Skipped unless DATABASE_URL is configured, so the default `go test ./...`
// run stays green without a database.
func TestE2E_ReviewPersistToStore_UnderstandReads(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping e2e learnings integration test")
	}

	ctx := context.Background()

	store, err := learnings.New(ctx, dsn, nil /* no embedder — nullable embedding column */)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Deterministic tag repo for isolation from real data.
	const testRepo = "__e2e_test__"
	const testSym = "TestSym_E2E"

	// Cleanup uses a sibling pool so we don't have to expose Store internals.
	// Option A from the plan: test-local, no public API change.
	cleanup := func() {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Logf("cleanup pool init failed (non-fatal): %v", err)
			return
		}
		defer pool.Close()
		if _, err := pool.Exec(ctx,
			`DELETE FROM review_learnings WHERE repo = $1`, testRepo); err != nil {
			t.Logf("cleanup DELETE failed (non-fatal): %v", err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// Simulate the review_pr_post persist path: three verdicts for the
	// same (repo, symbol). A 1ms sleep between inserts guarantees distinct
	// created_at values so ORDER BY created_at DESC is deterministic
	// without a tiebreaker column.
	recs := []learnings.Record{
		{
			Repo:    testRepo,
			Symbol:  testSym,
			Verdict: "good",
			Flag:    "style",
			Note:    "all good",
			PRURL:   "https://github.com/owner/repo/pull/1",
		},
		{
			Repo:    testRepo,
			Symbol:  testSym,
			Verdict: "neutral",
			Flag:    "minor",
			Note:    "ok",
			PRURL:   "https://github.com/owner/repo/pull/2",
		},
		{
			Repo:    testRepo,
			Symbol:  testSym,
			Verdict: "bad",
			Flag:    "critical",
			Note:    "please fix",
			PRURL:   "https://github.com/owner/repo/pull/3",
		},
	}
	for i, r := range recs {
		if i > 0 {
			time.Sleep(time.Millisecond)
		}
		if err := store.Upsert(ctx, r); err != nil {
			t.Fatalf("upsert %q: %v", r.Verdict, err)
		}
	}

	// Simulate the understand path: Nearest with same (repo, symbol).
	got, err := store.Nearest(ctx, testRepo, testSym, 10)
	if err != nil {
		t.Fatalf("nearest: %v", err)
	}
	if len(got) < 3 {
		t.Fatalf("expected >=3 records, got %d", len(got))
	}

	// Nearest orders by created_at DESC (store.go), so got[0] is the
	// last insert ("bad"). The 1ms sleeps guarantee distinct timestamps.
	if got[0].Verdict != "bad" || got[0].Note != "please fix" {
		t.Errorf("expected bad/please fix at [0], got %+v", got[0])
	}

	// Sanity check: all three verdicts are present in the result set
	// (defensive against any reordering at the storage layer).
	verdicts := map[string]bool{}
	for _, r := range got[:3] {
		verdicts[r.Verdict] = true
	}
	for _, want := range []string{"good", "neutral", "bad"} {
		if !verdicts[want] {
			t.Errorf("expected verdict %q in top 3 results, got verdicts=%v", want, verdicts)
		}
	}
}
