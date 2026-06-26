package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/freshness"
	"github.com/anatolykoptev/go-code/internal/semhealth"
)

// healthSnapshotResult bundles the snapshot and resolved focus flags.
type healthSnapshotResult struct {
	snap          *compare.RepoSnapshot
	isMagicMode   bool
	isSemanticDup bool
}

// buildHealthSnapshot parses the repository into a RepoSnapshot and resolves
// the special focus modes (magic_numbers, semantic_duplicates).
func buildHealthSnapshot(ctx context.Context, root, language, focus string) (*healthSnapshotResult, error) {
	isMagicMode := focus == "magic_numbers"
	isSemanticDup := focus == "semantic_duplicates"

	snapshotFocus := focus
	if isMagicMode || isSemanticDup {
		snapshotFocus = ""
	}

	snap, err := compare.BuildSnapshot(ctx, root, compare.SnapshotOpts{
		Focus:    snapshotFocus,
		Language: language,
	})
	if err != nil {
		return nil, err
	}
	return &healthSnapshotResult{
		snap:          snap,
		isMagicMode:   isMagicMode,
		isSemanticDup: isSemanticDup,
	}, nil
}

// collectSemanticDupGroups retrieves semantic duplication groups for the focused
// report (focus=semantic_duplicates path only). It runs the Phase-2 filter chain
// and assigns exact/very-close/related tiers via AnalyzeTriage.
//
// The grade-ratio path (gatherHealthSemanticDup) continues to use Analyze; this
// function is NOT called on that path — see the focus guard in the caller.
func collectSemanticDupGroups(ctx context.Context, semDeps *SemanticDeps, root string, snap *compare.RepoSnapshot) []semhealth.DupGroup {
	repoKey := codegraph.GraphNameFor(root)
	funcCount := countFuncs(snap.Symbols)

	// Pass the Expander only when non-nil: a nil *Expander assigned to the
	// graphPairFilter interface creates a non-nil interface wrapping a nil pointer,
	// which would panic inside graph filters. Explicit nil preserves the
	// graceful-degradation path (filters are no-ops on nil gf).
	var gf semhealth.GraphPairFilter
	if semDeps.Expander != nil {
		gf = semDeps.Expander
	}

	triage := semhealth.AnalyzeTriage(
		ctx,
		semDeps.Store,
		gf,
		repoKey, // graphName == repoKey for AGE
		repoKey,
		funcCount,
		semhealth.TriageOpts{Root: root},
	)
	if triage != nil {
		if triage.TimedOut {
			slog.Warn("semhealth: semantic dup search incomplete — triage results may be partial",
				slog.String("repo", repoKey))
		}
		return triage.Groups
	}
	return nil
}

// gatherHealthSemanticDup annotates metrics with semantic duplication ratio.
// No-op when semDeps is nil or the store is uninitialized.
func gatherHealthSemanticDup(ctx context.Context, semDeps *SemanticDeps, root string, snap *compare.RepoSnapshot, metrics *compare.RepoMetrics) {
	if semDeps == nil || semDeps.Store == nil {
		return
	}
	repoKey := codegraph.GraphNameFor(root)
	funcCount := countFuncs(snap.Symbols)
	if sem := semhealth.Analyze(ctx, semDeps.Store, repoKey, funcCount); sem != nil && sem.SemanticDupRatio > 0 {
		metrics.SemanticDupRatio = sem.SemanticDupRatio
		// Score/Grade are recomputed once after all stages complete (in computeCodeHealth
		// after wg.Wait()). Stages must not self-assign Score/Grade to avoid a race.
	}
}

// healthFreshnessResult bundles the freshness and vulnerability results.
type healthFreshnessResult struct {
	fr *freshness.FreshnessResult
	vr *freshness.VulnResult
}

// gatherHealthFreshness runs dependency freshness, vulnerability, and Go runtime
// version checks. Non-fatal: returns nil result when no manifests are found.
// On success, updates DepFreshnessRatio and VulnSecurityRatio in metrics.
func gatherHealthFreshness(ctx context.Context, root string, metrics *compare.RepoMetrics) healthFreshnessResult {
	// Hard cap so freshness never blocks the entire pipeline on large repos.
	// 313 PyPI deps at 20 concurrent × 2s = ~32s worst case; give 35s total.
	fCtx, fCancel := context.WithTimeout(ctx, 35*time.Second)
	defer fCancel()
	ctx = fCtx
	manifests := freshness.DiscoverManifests(root)
	if len(manifests) == 0 {
		// Scan ran but found no manifests — mark DepsScanned so the N/A guard fires.
		metrics.DepsScanned = true
		return healthFreshnessResult{}
	}

	allDeps := freshness.CollectDeps(manifests)
	if len(allDeps) == 0 {
		// Manifests found but no deps in them — still a completed scan.
		metrics.DepsScanned = true
		return healthFreshnessResult{}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	reg := freshness.NewMultiRegistryWithCache(client, nil)

	fr := freshness.CheckFreshness(ctx, allDeps, reg)
	metrics.DepFreshnessRatio = fr.Ratio
	metrics.TotalDeps = fr.Total // 0 means no manifests found (N/A for scoring)
	// Mark that a dep-freshness scan was attempted. This distinguishes a genuinely
	// zero-dependency repo (TotalDeps==0 after scan) from an unscanned path like
	// explore, which also leaves TotalDeps==0 but must NOT get the N/A neutral score.
	metrics.DepsScanned = true

	vr := freshness.CheckVulnerabilities(ctx, allDeps, client, freshness.DefaultOSVURL)
	metrics.VulnSecurityRatio = vr.Ratio

	for _, m := range manifests {
		if m.Language == "go" && m.RuntimeVersion != "" {
			if fr == nil {
				fr = &freshness.FreshnessResult{Ratio: 1.0}
			}
			fr.RuntimeStatus = freshness.CheckGoRuntime(ctx, client, m.RuntimeVersion)
			break
		}
	}

	// Score/Grade are recomputed once after all stages complete (in computeCodeHealth
	// after wg.Wait()). Stages must not self-assign Score/Grade to avoid a race.

	return healthFreshnessResult{fr: fr, vr: vr}
}

// gatherHealthHotspots runs git churn analysis and returns hotspot files.
// Returns nil when git is unavailable.
func gatherHealthHotspots(ctx context.Context, root, repoName string, snap *compare.RepoSnapshot) []compare.HotspotFile {
	churn, err := compare.CollectChurn(ctx, root, 0)
	if err != nil {
		slog.Debug("code_health: churn collection failed", slog.String("repo", repoName), slog.Any("error", err))
		return nil
	}
	return compare.ComputeHotspots(churn, compare.FileComplexityFromSnapshot(snap))
}

// gatherHealthArchMetrics queries the architecture graph store.
// When graphStore is nil or the repo is not indexed, falls back to
// compare.FallbackArchMetrics (in-memory call graph, 10s timeout).
func gatherHealthArchMetrics(ctx context.Context, graphStore *codegraph.Store, root string) *compare.ArchMetrics {
	if graphStore == nil {
		fb := compare.FallbackArchMetrics(ctx, root)
		if fb != nil {
			fb.Approximate = true
			fb.Hint = compare.HintApproxArchMetrics
			return fb
		}
		// FallbackArchMetrics returns nil when root is empty or when BuildFromRepo
		// fails (context timeout, parse error). Return nil so the caller omits
		// arch metrics entirely rather than showing misleading zeros.
		return nil
	}
	gctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return compare.CollectArchMetrics(gctx, graphStore, root)
}

// gatherHealthDeadCode queries pre-computed CE dead-code scores and populates
// candidate count and top function names. Non-fatal: no-op when graph unavailable.
func gatherHealthDeadCode(
	ctx context.Context,
	graphStore *codegraph.Store,
	root string,
	candidateCount *int,
	topNames *[]string,
) {
	if graphStore == nil {
		return
	}
	repoKey := codegraph.GraphNameFor(root)

	const minScore = float32(0.25)
	const maxTop = 3
	const queryLimit = 50

	candidates, err := graphStore.LoadTopDeadCodeCandidates(ctx, repoKey, minScore, queryLimit)
	if err != nil || len(candidates) == 0 {
		return
	}

	*candidateCount = len(candidates)

	// Collect top-3 names for the recommendation note.
	names := make([]string, 0, maxTop)
	seen := make(map[string]bool)
	for _, c := range candidates {
		if len(names) >= maxTop {
			break
		}
		if !seen[c.Name] {
			seen[c.Name] = true
			names = append(names, fmt.Sprintf("%s (%.0f%%)", c.Name, c.Score*100))
		}
	}
	*topNames = names

	slog.Info("codegraph: dead_code health metrics",
		slog.String("repo", root),
		slog.Int("candidates", *candidateCount),
	)
}
