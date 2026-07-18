package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// collectQualityStats runs ox-codes dataflow (quality + security) over the
// entire repository at deep mode and returns aggregated counts. Returns
// nil when the ox-codes backend is unavailable, the language cannot be
// detected, or both subqueries fail — at deep mode the absence of the
// section is the agent's signal that no static-analysis baseline could
// be obtained.
//
// Both calls run with their own short timeout so a slow backend cannot
// block repo_analyze; failures are logged at warn level and skipped.
func collectQualityStats(ctx context.Context, root, language string, deps analyze.Deps) *xmlQualitySummary {
	if deps.OxCodes == nil {
		return nil
	}
	if language == "" {
		language = detectDominantLanguage(root)
	}
	if language == "" {
		return nil
	}

	stats := &xmlQualitySummary{Language: language}
	any := false

	qctx, qcancel := context.WithTimeout(ctx, qualityTimeout)
	defer qcancel()
	if qResp, err := deps.OxCodes.DataflowAnalyze(qctx, oxcodes.DataflowInput{
		Root:       root,
		Language:   language,
		MaxResults: qualityMaxResults,
	}); err == nil {
		stats.QualityFindings = qResp.TotalFindings
		stats.FilesAnalyzed = qResp.FilesAnalyzed
		any = true
	} else {
		slog.Warn("repo_analyze: dataflow quality failed", "err", err)
	}

	sctx, scancel := context.WithTimeout(ctx, qualityTimeout)
	defer scancel()
	if sResp, err := deps.OxCodes.DataflowTaint(sctx, oxcodes.TaintInput{
		Root:       root,
		Language:   language,
		MaxResults: qualityMaxResults,
	}); err == nil {
		stats.SecurityFindings = sResp.TotalFindings
		if sResp.FilesAnalyzed > stats.FilesAnalyzed {
			stats.FilesAnalyzed = sResp.FilesAnalyzed
		}
		any = true
	} else {
		slog.Warn("repo_analyze: dataflow taint failed", "err", err)
	}

	if !any {
		return nil
	}
	return stats
}

// qualityTimeout caps each dataflow subquery so a slow ox-codes backend
// cannot stall a deep repo_analyze beyond the user's expectation. 15s is
// the same budget freshness collection uses; both run sequentially and
// share the parent ctx deadline.
const qualityTimeout = 15 * time.Second

// qualityMaxResults caps the per-call findings list. We only surface
// counts in repo_analyze, so a large cap is wasted work — 200 is enough
// to reflect the order-of-magnitude of issues without paying for a full
// findings dump that nobody reads here.
const qualityMaxResults = 200
