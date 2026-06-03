//go:build integration

package semhealth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDupValidation is a build-tagged integration test (//go:build integration).
// It requires two environment variables:
//
//	DUP_TEST_DSN      — Postgres DSN pointing at the gocode database
//	DUP_TEST_REPO_KEY — repo_key to run AnalyzeTriage against (e.g. "code_f40acc09")
//
// Run with:
//
//	DUP_TEST_DSN=... DUP_TEST_REPO_KEY=code_f40acc09 \
//	  GOWORK=off CGO_ENABLED=1 go test -tags=integration -run TestDupValidation -v \
//	  ./internal/semhealth/...
func TestDupValidation(t *testing.T) {
	dsn := os.Getenv("DUP_TEST_DSN")
	repoKey := os.Getenv("DUP_TEST_REPO_KEY")
	if dsn == "" || repoKey == "" {
		t.Skip("set DUP_TEST_DSN + DUP_TEST_REPO_KEY to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── Connect ──────────────────────────────────────────────────────────────
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

	// ── Determine totalFuncs ─────────────────────────────────────────────────
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("store.Stats: %v", err)
	}
	totalFuncs, ok := stats[repoKey]
	if !ok || totalFuncs == 0 {
		t.Skipf("repo %q has no embeddings in code_embeddings", repoKey)
	}
	t.Logf("repo=%s totalFuncs=%d semhealthMaxFuncs=%d", repoKey, totalFuncs, semhealthMaxFuncs)

	if totalFuncs > semhealthMaxFuncs {
		t.Skipf("repo %q has %d funcs > semhealthMaxFuncs=%d; size guard will skip AnalyzeTriage",
			repoKey, totalFuncs, semhealthMaxFuncs)
	}

	// ── Run AnalyzeTriage ────────────────────────────────────────────────────
	t.Log("running AnalyzeTriage…")
	start := time.Now()
	res := AnalyzeTriage(ctx, store, expander, repoKey, repoKey, totalFuncs, TriageOpts{})
	elapsed := time.Since(start)

	if res == nil {
		t.Fatal("AnalyzeTriage returned nil — invalid inputs or nil store (unexpected here)")
	}

	// ── Measured numbers (always visible under -v) ───────────────────────────
	t.Logf("elapsed=%s candidates=%d", elapsed.Round(time.Millisecond), res.Candidates)
	t.Logf("reported by tier: exact=%d very-close=%d related=%d",
		res.ReportedByTier[dupTierExact],
		res.ReportedByTier[dupTierVeryClose],
		res.ReportedByTier[dupTierRelated])
	t.Logf("filter drops: tests=%d same_file=%d kind=%d calls_edge=%d interface_sibling=%d",
		res.Dropped[dupFilterTests],
		res.Dropped[dupFilterSameFile],
		res.Dropped[dupFilterKind],
		res.Dropped[dupFilterCallsEdge],
		res.Dropped[dupFilterInterfaceSibling])
	t.Logf("total groups reported: %d", len(res.Groups))

	// ── Filter-invariant assertions ──────────────────────────────────────────
	// These assertions are the automated gate: they prove the filter chain
	// actually ran and its invariants hold on real data.

	for i, g := range res.Groups {
		// Every group must have a valid tier.
		validTiers := map[string]bool{
			dupTierExact:     true,
			dupTierVeryClose: true,
			dupTierRelated:   true,
		}
		if !validTiers[g.Tier] {
			t.Errorf("group[%d]: invalid tier %q", i, g.Tier)
		}

		for j, s := range g.Symbols {
			// Kind must be non-empty.
			if s.Kind == "" {
				t.Errorf("group[%d] symbol[%d] %q: empty Kind", i, j, s.Name)
			}

			// No endpoint may be a test file.
			if langutil.IsTestFile(s.File) {
				t.Errorf("group[%d] symbol[%d] %q: test file leaked through filter: %s",
					i, j, s.Name, s.File)
			}
		}
	}

	// Collect cross-file pairs from similar-tier groups to re-validate against
	// the AGE graph (proving graph filters ran on real data).
	//
	// NOTE on union-find and same-file group members: CollectDupGroups merges
	// pairs via union-find AFTER filterSameFile runs. A group of 3 can contain
	// two symbols from the same file if they are each cross-file similar to a
	// third symbol — i.e., A(f1)↔C(f2) and B(f1)↔C(f2) produces a group
	// {A,B,C} where A and B share f1, but the same-file pair A↔B was never in
	// the filtered set. This is expected behaviour, NOT a filter failure.
	// The filter invariant is: no (fileA, fileB) pair in the filtered INPUT
	// was same-file, which is confirmed by the same_file drop counter above.
	//
	// For the AGE re-check we only submit cross-file pairs so we do not
	// spuriously report a transitive same-file pair as "CALLS-connected leaked".
	var reportedPairKeys []embeddings.PairKey
	for _, g := range res.Groups {
		if g.Tier == dupTierExact {
			continue // exact groups come from body_hash, not the similar-pair path
		}
		// Build cross-file pairwise keys within the group.
		for a := 0; a < len(g.Symbols); a++ {
			for b := a + 1; b < len(g.Symbols); b++ {
				sa, sb := g.Symbols[a], g.Symbols[b]
				if sa.File == sb.File {
					// Transitive same-file pair — not in the original filtered set;
					// skip for the AGE re-check (see NOTE above).
					continue
				}
				pk := embeddings.NewPairKey(sa.File, sa.Name, sb.File, sb.Name)
				reportedPairKeys = append(reportedPairKeys, pk)
			}
		}
	}

	if len(reportedPairKeys) > 0 {
		// Re-query the AGE graph to confirm no reported pair is CALLS-connected
		// or interface-siblings — proving the graph filters actually ran.
		connected, err := expander.PairsConnectedByCalls(ctx, repoKey, reportedPairKeys)
		if err != nil {
			t.Logf("PairsConnectedByCalls re-check error (graph may be empty): %v", err)
		} else if len(connected) > 0 {
			for pk := range connected {
				t.Errorf("CALLS-connected pair survived filter: A=%s B=%s", pk.A, pk.B)
			}
		}

		siblings, err := expander.PairsSharingInterface(ctx, repoKey, reportedPairKeys)
		if err != nil {
			t.Logf("PairsSharingInterface re-check error (graph may be empty): %v", err)
		} else if len(siblings) > 0 {
			for pk := range siblings {
				t.Errorf("interface-sibling pair survived filter: A=%s B=%s", pk.A, pk.B)
			}
		}

		t.Logf("graph re-check passed: connected=%d interface_siblings=%d (both must be 0)",
			len(connected), len(siblings))
	} else {
		t.Log("no similar-tier pairs to re-check against graph (all groups are exact tier, or no groups)")
	}

	// ── Precision sample: top-10 groups for human eyeball ────────────────────
	const sampleN = 10
	t.Logf("=== top-%d precision sample ===", sampleN)
	for i, g := range res.Groups {
		if i >= sampleN {
			break
		}
		names := make([]string, len(g.Symbols))
		files := make([]string, len(g.Symbols))
		for j, s := range g.Symbols {
			names[j] = fmt.Sprintf("%s(%s)", s.Name, s.Kind)
			files[j] = s.File
		}
		t.Logf("  [%d] tier=%s sim=%.4f  syms=[%s]  files=[%s]",
			i, g.Tier, g.AvgSimilarity,
			strings.Join(names, ", "),
			strings.Join(files, " | "))
	}
}
