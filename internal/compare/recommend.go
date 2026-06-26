package compare

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	minScoreGap  = 0.01 // minimum gap to consider for improvement recommendations
	percentScale = 100  // multiplier to convert [0,1] scores to percentage points
)

// Recommendation is a single actionable improvement suggestion.
type Recommendation struct {
	Priority  int
	Potential int // estimated score points recoverable
	Area      string
	Message   string
}

// subScore tracks one of the 14 scoring dimensions.
type subScore struct {
	Name   string
	Score  float64 // [0, 1]
	Weight float64
	Points float64 // score * weight * 100
}

// computeSubScores replicates the 14 sub-score formulas from GradeScore
// (same package, same constants). Tests guard against drift.
func computeSubScores(m RepoMetrics) []subScore {
	if m.Files == 0 {
		return nil
	}
	return []subScore{
		{"cognitive_complexity", clamp01(1.0 - (m.AvgCognitiveComplexity-targetCognitiveComplexity)/rangeCognitiveComplexity), weightCognitiveComplexity, 0},
		{"cyclomatic_avg", clamp01(1.0 - (m.AvgComplexity-targetCyclomaticAvg)/rangeCyclomaticAvg), weightCyclomaticAvg, 0},
		{"cyclomatic_max", clamp01(1.0 - (float64(m.MaxComplexity)-targetCyclomaticMax)/rangeCyclomaticMax), weightCyclomaticMax, 0},
		{"test_coverage", clamp01(m.TestRatio / targetTestRatio), weightTestCoverage, 0},
		{"doc_coverage", clamp01(m.DocRatio / targetDocRatio), weightDocCoverage, 0},
		{"func_size", clamp01(1.0 - (m.AvgFuncLines-targetFuncSize)/rangeFuncSize), weightFuncSize, 0},
		{"error_handling", clamp01(m.ErrorHandlingRatio / targetErrorHandlingRatio), weightErrorHandling, 0},
		{"nesting_depth", clamp01(1.0 - (float64(m.MaxNestingDepth)-targetNestingDepth)/rangeNestingDepth), weightNestingDepth, 0},
		{"file_size", clamp01(1.0 - m.LargeFileRatio*fileSizeMultiplier), weightFileSize, 0},
		{"duplication", clamp01(1.0 - m.DuplicationRatio*duplicationMultiplier), weightDuplication, 0},
		{"magic_numbers", clamp01(1.0 - m.MagicNumberRatio*magicNumberMultiplier), weightMagicNumbers, 0},
		{"semantic_duplication", clamp01(1.0 - m.SemanticDupRatio*semanticDupMultiplier), weightSemanticDup, 0},
		// When TotalDeps == 0, no dependency manifests were found.
		// Use score 1.0 (neutral/best) so the gap is zero and no dep/vuln
		// recommendation is emitted. Mirrors the guard in GradeScore.
		{"dep_freshness", func() float64 {
			if m.TotalDeps == 0 {
				return 1.0
			}
			return clamp01(m.DepFreshnessRatio / targetDepFreshness)
		}(), weightDepFreshness, 0},
		{"vuln_security", func() float64 {
			if m.TotalDeps == 0 {
				return 1.0
			}
			return clamp01(m.VulnSecurityRatio / targetVulnSecurity)
		}(), weightVulnSecurity, 0},
	}
}

// SubScoreSum returns the total score from sub-scores (should equal GradeScore).
// Exported for drift-guard tests.
func SubScoreSum(m RepoMetrics) float64 {
	ss := computeSubScores(m)
	if ss == nil {
		return 0
	}
	total := 0.0
	for _, s := range ss {
		total += s.Score * s.Weight
	}
	return math.Round(total * percentScale)
}

// ComputeRecommendations identifies the weakest scoring areas and returns
// actionable recommendations sorted by potential score impact (descending).
// maxItems limits the number of returned recommendations (0 = unlimited).
func ComputeRecommendations(m RepoMetrics, out Outliers, maxItems int) []Recommendation {
	ss := computeSubScores(m)
	if ss == nil {
		return nil
	}

	// Fill points and find items with room for improvement.
	type candidate struct {
		sub       subScore
		potential float64 // score points recoverable
	}
	var candidates []candidate
	for i := range ss {
		ss[i].Points = ss[i].Score * ss[i].Weight * percentScale
		gap := 1.0 - ss[i].Score
		if gap < minScoreGap {
			continue
		}
		pot := gap * ss[i].Weight * percentScale
		candidates = append(candidates, candidate{sub: ss[i], potential: pot})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].potential > candidates[j].potential
	})

	if maxItems > 0 && len(candidates) > maxItems {
		candidates = candidates[:maxItems]
	}

	recs := make([]Recommendation, len(candidates))
	for i, c := range candidates {
		recs[i] = Recommendation{
			Priority:  i + 1,
			Potential: int(math.Round(c.potential)),
			Area:      c.sub.Name,
			Message:   buildMessage(c.sub, m, out),
		}
	}

	// Dead code recommendation (from CE reranker scores).
	if m.DeadCodeCandidates > 0 {
		note := fmt.Sprintf("Remove %d likely dead functions (CE confidence >= 25%%)", m.DeadCodeCandidates)
		if len(m.DeadCodeTopNames) > 0 {
			note += ". Top: " + strings.Join(m.DeadCodeTopNames, ", ")
		}
		impact := int(math.Min(8, float64(m.DeadCodeCandidates)*0.4))
		if impact < 1 {
			impact = 1
		}
		recs = append(recs, Recommendation{
			Priority:  len(recs) + 1,
			Area:      "dead_code",
			Message:   note,
			Potential: impact,
		})
	}
	return recs
}

func buildMessage(s subScore, m RepoMetrics, out Outliers) string {
	switch s.Name {
	case "test_coverage":
		return fmt.Sprintf("Add more test files (current: %.0f%%, target: %.0f%%)",
			m.TestRatio*percentScale, targetTestRatio*percentScale)
	case "doc_coverage":
		return fmt.Sprintf("Add doc comments to exported symbols (current: %.0f%%, target: %.0f%%)",
			m.DocRatio*percentScale, targetDocRatio*percentScale)
	case "error_handling":
		return fmt.Sprintf("Improve error handling coverage (current: %.0f%%, target: %.0f%%)",
			m.ErrorHandlingRatio*percentScale, targetErrorHandlingRatio*percentScale)
	case "cognitive_complexity":
		msg := fmt.Sprintf("Reduce avg cognitive complexity (current: %.1f, target: ≤%.0f)", m.AvgCognitiveComplexity, targetCognitiveComplexity)
		return appendOutlier(msg, out.MaxCognitive)
	case "cyclomatic_avg":
		msg := fmt.Sprintf("Reduce avg cyclomatic complexity (current: %.1f, target: ≤%.0f)", m.AvgComplexity, targetCyclomaticAvg)
		return appendOutlier(msg, out.MaxCyclomatic)
	case "cyclomatic_max":
		msg := fmt.Sprintf("Refactor most complex function (cyclomatic: %d, target: ≤%.0f)", m.MaxComplexity, targetCyclomaticMax)
		return appendOutlier(msg, out.MaxCyclomatic)
	case "func_size":
		msg := fmt.Sprintf("Reduce avg function size (current: %.0f lines, target: ≤%.0f)", m.AvgFuncLines, targetFuncSize)
		return appendOutlier(msg, out.MaxFuncLines)
	case "nesting_depth":
		msg := fmt.Sprintf("Reduce max nesting depth (current: %d, target: ≤%.0f)", m.MaxNestingDepth, targetNestingDepth)
		return appendOutlier(msg, out.MaxNesting)
	case "file_size":
		return fmt.Sprintf("Split large files (%.0f%% exceed threshold, target: 0%%)", m.LargeFileRatio*percentScale)
	case "duplication":
		return fmt.Sprintf("Reduce code duplication (ratio: %.0f%%, target: 0%%)", m.DuplicationRatio*percentScale)
	case "magic_numbers":
		msg := fmt.Sprintf("Extract magic numbers into named constants (%.0f%% of functions affected)", m.MagicNumberRatio*percentScale)
		return appendOutlier(msg, out.MaxMagicNumbers)
	case "semantic_duplication":
		return fmt.Sprintf("Reduce semantic duplication — %.0f%% of functions are semantically similar to others. Extract shared logic into reusable helpers", m.SemanticDupRatio*percentScale)
	case "dep_freshness":
		return fmt.Sprintf("Update outdated dependencies (%.0f%% current, target: %.0f%%, %d deps)",
			m.DepFreshnessRatio*percentScale, targetDepFreshness*percentScale, m.TotalDeps)
	case "vuln_security":
		return fmt.Sprintf("Fix vulnerable dependencies (%d%% safe, target: 100%%, %d deps)",
			int(m.VulnSecurityRatio*percentScale), m.TotalDeps)
	default:
		return fmt.Sprintf("Improve %s (score: %.0f%%)", s.Name, s.Score*percentScale)
	}
}

func appendOutlier(msg string, o OutlierFunc) string {
	if o.Name == "" {
		return msg
	}
	return fmt.Sprintf("%s.\nWorst: %s in %s:%d", msg, o.Name, o.File, o.Line)
}
