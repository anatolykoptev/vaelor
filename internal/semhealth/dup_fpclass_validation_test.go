//go:build integration

package semhealth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDupFPClasses validates the two false-positive classes fixed by the
// signature-receiver interface-sibling discriminator and the build-tag filter,
// dogfooded against the LIVE go-code self-index.
//
// Required env:
//
//	DUP_TEST_DSN       — Postgres DSN pointing at the gocode database
//	DUP_TEST_REPO_KEY  — repo_key for the go-code self-index (code_72f78056)
//	DUP_TEST_ROOT      — on-disk repo root for the build-tag filter
//	                     (the checkout the index was built from)
//
// Run:
//
//	DUP_TEST_DSN=... DUP_TEST_REPO_KEY=code_72f78056 DUP_TEST_ROOT=/home/user/src/go-code \
//	  GOWORK=off CGO_ENABLED=1 go test -tags=integration -run TestDupFPClasses -v \
//	  ./internal/semhealth/...
func TestDupFPClasses(t *testing.T) {
	dsn := os.Getenv("DUP_TEST_DSN")
	repoKey := os.Getenv("DUP_TEST_REPO_KEY")
	root := os.Getenv("DUP_TEST_ROOT")
	if dsn == "" || repoKey == "" {
		t.Skip("set DUP_TEST_DSN + DUP_TEST_REPO_KEY to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("DB ping failed: %v", err)
	}

	store := embeddings.NewStore(pool)
	expander := embeddings.NewExpander(pool)

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("store.Stats: %v", err)
	}
	totalFuncs, ok := stats[repoKey]
	if !ok || totalFuncs == 0 {
		t.Skipf("repo %q has no embeddings", repoKey)
	}

	res := AnalyzeTriage(ctx, store, expander, repoKey, repoKey, totalFuncs,
		TriageOpts{Root: root})
	if res == nil {
		t.Fatal("AnalyzeTriage returned nil")
	}

	t.Logf("candidates=%d reported=%d timedOut=%v", res.Candidates, len(res.Groups), res.TimedOut)
	t.Logf("filter drops: tests=%d same_file=%d kind=%d build_tag=%d calls_edge=%d interface_sibling=%d",
		res.Dropped[dupFilterTests], res.Dropped[dupFilterSameFile], res.Dropped[dupFilterKind],
		res.Dropped[dupFilterBuildTag], res.Dropped[dupFilterCallsEdge], res.Dropped[dupFilterInterfaceSibling])

	// Build a set of reported symbol names for membership checks.
	reported := make(map[string]bool)
	for _, g := range res.Groups {
		for _, s := range g.Symbols {
			reported[s.Name] = true
		}
	}

	// ── FP class 1: interface-impl siblings must be DROPPED ───────────────────
	if reported["FetchREADME"] {
		t.Errorf("FP class 1 NOT fixed: FetchREADME (interface-impl sibling) still reported")
	}
	if res.Dropped[dupFilterInterfaceSibling] == 0 {
		t.Errorf("interface_sibling drop count is 0 — the discriminator never fired "+
			"(was a dead filter before the fix); expected >0 on %q", repoKey)
	}

	// ── FP class 2: build-tag platform variants must be DROPPED ───────────────
	if root != "" {
		if reported["atomicDirectorySwap"] {
			t.Errorf("FP class 2 NOT fixed: atomicDirectorySwap (build-tag variant) still reported")
		}
		if res.Dropped[dupFilterBuildTag] == 0 {
			t.Errorf("build_tag drop count is 0 — the filter never fired; "+
				"expected >0 (atomicDirectorySwap linux/!linux split) on %q", repoKey)
		}
	} else {
		t.Log("DUP_TEST_ROOT empty — build-tag filter no-op; skipping FP class 2 assertions")
	}

	// ── True positives must be RETAINED (no over-suppression) ─────────────────
	// These are synthetic parser-fixture strings used as representative inputs for
	// the signature-parsing and over-suppression logic below. The actual
	// cross-package duplicates (e.g. countSourceFiles, commonPrefixLen) were
	// consolidated in PR #258 — these names are kept only to exercise the
	// over-suppression guard against new regressions.
	truePositives := []string{"countSourceFiles", "commonPrefixLen", "hasTestAttribute", "upsertBatch"}
	for _, tp := range truePositives {
		if !reported[tp] {
			t.Errorf("OVER-SUPPRESSION: true-positive %q dropped — the fix removed a real duplicate", tp)
		}
	}

	// ── False-NEGATIVE class: cross-package unexported copy-paste must be REPORTED ─
	// removeFromOrder is a byte-identical method duplicated across distinct cache
	// types in DIFFERENT packages (internal/callgraph, internal/compare,
	// internal/federate). It is UNEXPORTED, so no interface can name it across
	// packages → it can never be an interface sibling. Before the carve-out the
	// signature-receiver discriminator wrongly suppressed it; it must now be
	// reported as a genuine duplicate.
	if !reported["removeFromOrder"] {
		t.Errorf("FALSE-NEGATIVE not closed: removeFromOrder (unexported cross-package copy-paste) " +
			"is suppressed — the interface-sibling discriminator must NOT suppress unexported " +
			"methods on receivers in different packages")
	}

	// Precision sample for human eyeball.
	const sampleN = 12
	for i, g := range res.Groups {
		if i >= sampleN {
			break
		}
		names := make([]string, len(g.Symbols))
		files := make([]string, len(g.Symbols))
		for j, s := range g.Symbols {
			names[j] = s.Name + "(" + s.Kind + ")"
			files[j] = s.File
		}
		t.Logf("  [%d] tier=%s sim=%.3f syms=[%s] files=[%s]",
			i, g.Tier, g.AvgSimilarity, strings.Join(names, ", "), strings.Join(files, " | "))
	}
}
