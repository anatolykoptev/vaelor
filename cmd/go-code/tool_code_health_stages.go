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
	snap         *compare.RepoSnapshot
	isMagicMode  bool
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

// collectSemanticDupGroups retrieves semantic duplication groups for the focused report.
func collectSemanticDupGroups(ctx context.Context, semDeps *SemanticDeps, root string, snap *compare.RepoSnapshot) []semhealth.DupGroup {
	repoKey := codegraph.GraphNameFor(root)
	funcCount := countFuncs(snap.Symbols)
	if sem := semhealth.Analyze(ctx, semDeps.Store, repoKey, funcCount); sem != nil {
		return sem.DupGroups
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
		metrics.Score = compare.GradeScore(*metrics)
		metrics.Grade = compare.ComputeGrade(*metrics)
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
	manifests := freshness.DiscoverManifests(root)
	if len(manifests) == 0 {
		return healthFreshnessResult{}
	}

	allDeps := freshness.CollectDeps(manifests)
	if len(allDeps) == 0 {
		return healthFreshnessResult{}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	reg := freshness.NewMultiRegistryWithCache(client, nil)

	fr := freshness.CheckFreshness(ctx, allDeps, reg)
	metrics.DepFreshnessRatio = fr.Ratio

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

	metrics.Score = compare.GradeScore(*metrics)
	metrics.Grade = compare.ComputeGrade(*metrics)

	return healthFreshnessResult{fr: fr, vr: vr}
}

// gatherHealthHotspots runs git churn analysis and returns hotspot files.
// Returns nil when git is unavailable.
func gatherHealthHotspots(ctx context.Context, root, repoName string, snap *compare.RepoSnapshot) []compare.HotspotFile {
	churn, err := compare.CollectChurn(ctx, root)
	if err != nil {
		slog.Debug("code_health: churn collection failed", slog.String("repo", repoName), slog.Any("error", err))
		return nil
	}
	return compare.ComputeHotspots(churn, compare.FileComplexityFromSnapshot(snap))
}

// gatherHealthArchMetrics queries the architecture graph store.
// Returns nil when graphStore is nil.
func gatherHealthArchMetrics(ctx context.Context, graphStore *codegraph.Store, root string) *compare.ArchMetrics {
	if graphStore == nil {
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
