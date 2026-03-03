package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// ComputeMetrics derives aggregate quality metrics from a RepoSnapshot.
//
// The snapshot must already have Symbols and Imports populated; FileCount and
// TotalLines are copied verbatim. Test-file detection is based on Symbol.File
// paths (unique files seen across all symbols).
func ComputeMetrics(snap *RepoSnapshot) RepoMetrics {
	// --- function/method lines + complexity ---
	totalFuncLines := 0
	totalCyclomatic := 0
	totalCognitive := 0
	funcCount := 0
	maxFuncLines := 0
	maxCyclomatic := 0
	maxCognitive := 0
	maxNesting := 0
	totalNesting := 0

	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		lines := funcLines(sym)
		totalFuncLines += lines
		funcCount++
		if lines > maxFuncLines {
			maxFuncLines = lines
		}

		cc := cyclomaticComplexity(sym.Body)
		totalCyclomatic += cc
		if cc > maxCyclomatic {
			maxCyclomatic = cc
		}

		cog := cognitiveComplexity(sym.Body)
		totalCognitive += cog
		if cog > maxCognitive {
			maxCognitive = cog
		}

		nd := nestingDepth(sym.Body)
		totalNesting += nd
		if nd > maxNesting {
			maxNesting = nd
		}
	}

	var avgFuncLines float64
	if funcCount > 0 {
		avgFuncLines = float64(totalFuncLines) / float64(funcCount)
	}
	var avgCyclomatic float64
	if funcCount > 0 {
		avgCyclomatic = float64(totalCyclomatic) / float64(funcCount)
	}
	var avgCognitive float64
	if funcCount > 0 {
		avgCognitive = float64(totalCognitive) / float64(funcCount)
	}
	var avgNesting float64
	if funcCount > 0 {
		avgNesting = float64(totalNesting) / float64(funcCount)
	}

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

	// --- param metrics ---
	avgParams, maxParams := computeParamMetrics(snap.Symbols)

	result := RepoMetrics{
		Files:              snap.FileCount,
		TotalLines:         snap.TotalLines,
		AvgFuncLines:       avgFuncLines,
		MaxFuncLines:       maxFuncLines,
		AvgComplexity:      avgCyclomatic,
		MaxComplexity:      maxCyclomatic,
		TestRatio:          testFileRatio,
		DocRatio:           docRatio,
		ErrorHandlingRatio: errorHandlingRatio,
		Interfaces:         interfaceCount,
		ExternalDeps:       externalDeps,

		AvgCognitiveComplexity: avgCognitive,
		MaxCognitiveComplexity: maxCognitive,
		AvgNestingDepth:        avgNesting,
		MaxNestingDepth:        maxNesting,
		LargeFileRatio:         largeFileRatio,
		DuplicationRatio:       duplicationRatio,
		AvgParamCount:          avgParams,
		MaxParamCount:          maxParams,
	}
	result.Grade = ComputeGrade(result)
	result.Score = GradeScore(result)
	return result
}
