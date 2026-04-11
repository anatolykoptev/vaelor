package compare

// CrossLangReport summarizes cross-language comparison findings.
type CrossLangReport struct {
	LanguageA       string           `json:"languageA"`
	LanguageB       string           `json:"languageB"`
	SemanticMatches int              `json:"semanticMatches"`
	TopMatches      []CrossLangMatch `json:"topMatches,omitempty"`
}

// CrossLangMatch describes a semantic match between symbols in different languages.
type CrossLangMatch struct {
	NameA      string  `json:"nameA"`
	NameB      string  `json:"nameB"`
	FileA      string  `json:"fileA"`
	FileB      string  `json:"fileB"`
	Similarity float64 `json:"similarity"`
}

// maxCrossLangMatches limits the top matches shown in the report.
const maxCrossLangMatches = 10

// BuildCrossLangReport creates a cross-language report from symbol matches.
// Returns nil if both repos use the same language.
func BuildCrossLangReport(matches []SymbolMatch, langA, langB string) *CrossLangReport {
	if langA == langB || langA == "" || langB == "" {
		return nil
	}

	report := &CrossLangReport{
		LanguageA: langA,
		LanguageB: langB,
	}

	for _, m := range matches {
		if m.MatchType != MatchSemantic {
			continue
		}
		report.SemanticMatches++
		if len(report.TopMatches) < maxCrossLangMatches && m.SymbolA != nil && m.SymbolB != nil {
			report.TopMatches = append(report.TopMatches, CrossLangMatch{
				NameA:      m.SymbolA.Name,
				NameB:      m.SymbolB.Name,
				FileA:      m.SymbolA.File,
				FileB:      m.SymbolB.File,
				Similarity: m.Score,
			})
		}
	}

	if report.SemanticMatches == 0 {
		return report // still return report to indicate cross-lang was attempted
	}
	return report
}
