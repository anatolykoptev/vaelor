package compare

import (
	"sort"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// HotspotFile is a file identified as a maintenance risk (high churn x high complexity).
type HotspotFile struct {
	File       string  `json:"file"`
	Score      float64 `json:"score"`
	Churn      int     `json:"churn"`
	Complexity float64 `json:"complexity"`
	Risk       string  `json:"risk"` // "critical", "high", "moderate"
}

// Hotspot thresholds from Omen (percentile products).
const (
	hotspotCritical = 0.81
	hotspotHigh     = 0.64
	hotspotModerate = 0.36
)

// maxHotspots limits the number of hotspots returned.
const maxHotspots = 20

// ComputeHotspots combines churn data and per-file complexity to find maintenance hotspots.
// Uses the Omen formula: score = percentile(churn) x percentile(complexity).
func ComputeHotspots(churn map[string]ChurnStats, fileComplexity map[string]float64) []HotspotFile {
	if len(churn) == 0 || len(fileComplexity) == 0 {
		return nil
	}

	type entry struct {
		file       string
		churnScore float64
		complexity float64
	}

	var entries []entry
	for file, stats := range churn {
		cc, ok := fileComplexity[file]
		if !ok || cc <= 0 {
			continue
		}
		entries = append(entries, entry{file: file, churnScore: stats.ChurnScore(), complexity: cc})
	}

	if len(entries) == 0 {
		return nil
	}

	churnValues := make([]float64, len(entries))
	complexityValues := make([]float64, len(entries))
	for i, e := range entries {
		churnValues[i] = e.churnScore
		complexityValues[i] = e.complexity
	}
	sort.Float64s(churnValues)
	sort.Float64s(complexityValues)

	var hotspots []HotspotFile
	for _, e := range entries {
		churnPct := percentileRank(e.churnScore, churnValues)
		complexityPct := percentileRank(e.complexity, complexityValues)
		score := churnPct * complexityPct

		risk := classifyRisk(score)
		if risk == "" {
			continue
		}

		hotspots = append(hotspots, HotspotFile{
			File:       e.file,
			Score:      score,
			Churn:      int(e.churnScore),
			Complexity: e.complexity,
			Risk:       risk,
		})
	}

	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].Score > hotspots[j].Score
	})

	if len(hotspots) > maxHotspots {
		hotspots = hotspots[:maxHotspots]
	}

	return hotspots
}

// FileComplexityFromSnapshot computes average cyclomatic complexity per file
// from the snapshot's symbols.
func FileComplexityFromSnapshot(snap *RepoSnapshot) map[string]float64 {
	type acc struct {
		total int
		count int
	}

	byFile := make(map[string]*acc)
	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if sym.File == "" {
			continue
		}
		a, ok := byFile[sym.File]
		if !ok {
			a = &acc{}
			byFile[sym.File] = a
		}
		a.total += cyclomaticComplexity(sym.Body)
		a.count++
	}

	result := make(map[string]float64, len(byFile))
	for file, a := range byFile {
		if a.count > 0 {
			result[file] = float64(a.total) / float64(a.count)
		}
	}
	return result
}

// percentileRank returns the fraction of values in the sorted slice that are <= val.
func percentileRank(val float64, sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	count := 0
	for _, v := range sorted {
		if v <= val {
			count++
		}
	}
	return float64(count) / float64(n)
}

// classifyRisk returns the risk classification based on hotspot score thresholds.
func classifyRisk(score float64) string {
	switch {
	case score >= hotspotCritical:
		return "critical"
	case score >= hotspotHigh:
		return "high"
	case score >= hotspotModerate:
		return "moderate"
	default:
		return ""
	}
}
