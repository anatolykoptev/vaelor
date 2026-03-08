package compare


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
