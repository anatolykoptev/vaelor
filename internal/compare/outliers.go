package compare

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// OutlierFunc identifies a single function that holds a maximum metric value.
type OutlierFunc struct {
	Name  string
	File  string
	Line  int
	Value int
}

// Outliers holds the worst-offending function for each key metric.
type Outliers struct {
	MaxCyclomatic   OutlierFunc
	MaxCognitive    OutlierFunc
	MaxFuncLines    OutlierFunc
	MaxNesting      OutlierFunc
	MaxMagicNumbers OutlierFunc
}

// CollectOutliers iterates snapshot symbols and records which function
// holds each maximum metric value. File paths are stripped of the root
// prefix to produce relative paths (same pattern as FileComplexityFromSnapshot).
func CollectOutliers(snap *RepoSnapshot) Outliers {
	prefix := snap.Root + "/"
	var out Outliers

	for _, sym := range snap.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}

		rel := strings.TrimPrefix(sym.File, prefix)
		line := int(sym.StartLine)

		cc := cyclomaticComplexity(sym.Body, sym.Language)
		if cc > out.MaxCyclomatic.Value {
			out.MaxCyclomatic = OutlierFunc{Name: sym.Name, File: rel, Line: line, Value: cc}
		}

		cog := cognitiveComplexity(sym.Body, sym.Language)
		if cog > out.MaxCognitive.Value {
			out.MaxCognitive = OutlierFunc{Name: sym.Name, File: rel, Line: line, Value: cog}
		}

		fl := funcLines(sym)
		if fl > out.MaxFuncLines.Value {
			out.MaxFuncLines = OutlierFunc{Name: sym.Name, File: rel, Line: line, Value: fl}
		}

		nd := nestingDepth(sym.Body, sym.Language)
		if nd > out.MaxNesting.Value {
			out.MaxNesting = OutlierFunc{Name: sym.Name, File: rel, Line: line, Value: nd}
		}

		mn := countMagicNumbers(sym.Body, sym.Language)
		if mn > out.MaxMagicNumbers.Value {
			out.MaxMagicNumbers = OutlierFunc{Name: sym.Name, File: rel, Line: line, Value: mn}
		}
	}

	return out
}
