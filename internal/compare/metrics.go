package compare

import (
	"context"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-code/internal/freshness"
)

// FreshnessStats holds dependency freshness and vulnerability data.
type FreshnessStats struct {
	DepFreshnessRatio float64 `json:"depFreshnessRatio"`
	VulnSecurityRatio float64 `json:"vulnSecurityRatio"`
	TotalDeps         int     `json:"totalDeps"`
	OutdatedDeps      int     `json:"outdatedDeps"`
	VulnDeps          int     `json:"vulnDeps"`
}

// DataflowStats holds dead code and data flow findings from ox-codes.
type DataflowStats struct {
	DeadStores    int `json:"deadStores"`
	UnusedVars    int `json:"unusedVars"`
	TotalFindings int `json:"totalFindings"`
	FilesAnalyzed int `json:"filesAnalyzed"`
}

// collectFreshness checks dependency freshness and vulnerabilities for a repo.
// Returns nil if no manifests found. depRatio and vulnRatio contain the ratios.
func collectFreshness(ctx context.Context, root string) (*FreshnessStats, float64, float64) {
	manifests := freshness.DiscoverManifests(root)
	if len(manifests) == 0 {
		return nil, 0, 0
	}

	allDeps := freshness.CollectDeps(manifests)
	if len(allDeps) == 0 {
		return nil, 0, 0
	}

	client := &http.Client{Timeout: 5 * time.Second}
	stats := &FreshnessStats{}

	reg := freshness.NewMultiRegistry(client)
	if fr := freshness.CheckFreshness(ctx, allDeps, reg); fr != nil {
		stats.DepFreshnessRatio = fr.Ratio
		stats.TotalDeps = fr.Total
		stats.OutdatedDeps = fr.Total - fr.UpToDate
	}

	if vr := freshness.CheckVulnerabilities(ctx, allDeps, client, freshness.DefaultOSVURL); vr != nil {
		stats.VulnSecurityRatio = vr.Ratio
		stats.VulnDeps = vr.Vulnerable
	}

	return stats, stats.DepFreshnessRatio, stats.VulnSecurityRatio
}

// ComputeMetrics derives aggregate quality metrics from a RepoSnapshot.
//
// The snapshot must already have Symbols and Imports populated; FileCount and
// TotalLines are copied verbatim. Test-file detection is based on Symbol.File
// paths (unique files seen across all symbols).
func ComputeMetrics(snap *RepoSnapshot) RepoMetrics {
	fc := computeFuncComplexity(snap.Symbols)

	// --- test-file ratio ---
	testFilePaths := collectTestFilePaths(snap)
	var testFileRatio float64
	if snap.FileCount > 0 {
		testFileRatio = float64(len(testFilePaths)) / float64(snap.FileCount)
	}

	// --- doc-comment ratio ---
	docRatio := computeDocRatio(snap.Symbols)

	// --- external deps ---
	externalDeps := countExternalDeps(snap.Imports)

	// --- error handling ratio ---
	errorHandlingRatio := computeErrorHandlingRatio(snap.Symbols)

	// --- interface count ---
	interfaceCount := countInterfaces(snap.Symbols)

	// --- file-level metrics ---
	largeFileRatio := computeLargeFileRatio(snap.Files)
	duplicationRatio := computeDuplicationRatio(snap.Symbols)
	magicNumberRatio := computeMagicNumberRatio(snap.Symbols)

	// --- param metrics ---
	avgParams, maxParams := computeParamMetrics(snap.Symbols)

	result := RepoMetrics{
		Files:              snap.FileCount,
		TotalLines:         snap.TotalLines,
		AvgFuncLines:       fc.avgFuncLines,
		MaxFuncLines:       fc.maxFuncLines,
		AvgComplexity:      fc.avgCyclomatic,
		MaxComplexity:      fc.maxCyclomatic,
		TestRatio:          testFileRatio,
		DocRatio:           docRatio,
		ErrorHandlingRatio: errorHandlingRatio,
		Interfaces:         interfaceCount,
		ExternalDeps:       externalDeps,

		AvgCognitiveComplexity: fc.avgCognitive,
		MaxCognitiveComplexity: fc.maxCognitive,
		AvgNestingDepth:        fc.avgNesting,
		MaxNestingDepth:        fc.maxNesting,
		LargeFileRatio:         largeFileRatio,
		DuplicationRatio:       duplicationRatio,
		MagicNumberRatio:       magicNumberRatio,
		AvgParamCount:          avgParams,
		MaxParamCount:          maxParams,
	}
	result.Grade = ComputeGrade(result)
	result.Score = GradeScore(result)
	return result
}
