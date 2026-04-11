package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// funcComplexityResult holds aggregated function-level complexity metrics.
type funcComplexityResult struct {
	avgFuncLines  float64
	maxFuncLines  int
	avgCyclomatic float64
	maxCyclomatic int
	avgCognitive  float64
	maxCognitive  int
	avgNesting    float64
	maxNesting    int
}

// computeFuncComplexity iterates function/method symbols and computes
// complexity metrics (cyclomatic, cognitive, nesting, size).
func computeFuncComplexity(symbols []*parser.Symbol) funcComplexityResult {
	var (
		totalFuncLines, totalCyclomatic, totalCognitive, totalNesting int
		maxFuncLines, maxCyclomatic, maxCognitive, maxNesting         int
		funcCount                                                     int
	)

	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		funcCount++

		lines := funcLines(sym)
		totalFuncLines += lines
		if lines > maxFuncLines {
			maxFuncLines = lines
		}

		cc := cyclomaticComplexity(sym.Body, sym.Language)
		totalCyclomatic += cc
		if cc > maxCyclomatic {
			maxCyclomatic = cc
		}

		cog := cognitiveComplexity(sym.Body, sym.Language)
		totalCognitive += cog
		if cog > maxCognitive {
			maxCognitive = cog
		}

		nd := nestingDepth(sym.Body, sym.Language)
		totalNesting += nd
		if nd > maxNesting {
			maxNesting = nd
		}
	}

	var r funcComplexityResult
	r.maxFuncLines = maxFuncLines
	r.maxCyclomatic = maxCyclomatic
	r.maxCognitive = maxCognitive
	r.maxNesting = maxNesting
	if funcCount > 0 {
		r.avgFuncLines = float64(totalFuncLines) / float64(funcCount)
		r.avgCyclomatic = float64(totalCyclomatic) / float64(funcCount)
		r.avgCognitive = float64(totalCognitive) / float64(funcCount)
		r.avgNesting = float64(totalNesting) / float64(funcCount)
	}
	return r
}

// funcLines returns the line count for a function/method symbol.
func funcLines(sym *parser.Symbol) int {
	if sym.EndLine >= sym.StartLine {
		return int(sym.EndLine-sym.StartLine) + 1
	}
	return 1
}

// computeDocRatio returns the fraction of exported symbols that have a doc comment.
func computeDocRatio(symbols []*parser.Symbol) float64 {
	exportedTotal := 0
	exportedWithDoc := 0
	for _, sym := range symbols {
		if !isExported(sym.Name) {
			continue
		}
		exportedTotal++
		if sym.DocComment != "" {
			exportedWithDoc++
		}
	}
	if exportedTotal == 0 {
		return 0
	}
	return float64(exportedWithDoc) / float64(exportedTotal)
}

// ioPatterns are substrings that indicate I/O operations requiring error handling.
var ioPatterns = []string{
	"os.Open", "os.Create", "os.ReadFile", "os.WriteFile", "os.Remove",
	"os.Mkdir", "os.Stat",
	"io.Read", "io.Write", "io.Copy",
	"http.Get", "http.Post", "http.Do", "http.NewRequest",
	"json.Marshal", "json.Unmarshal", "json.NewDecoder", "json.NewEncoder",
	"sql.", "exec.Command",
	".Query(", ".QueryRow(",
}

// errorHandlingPatterns are substrings that reliably indicate error handling in function bodies.
var errorHandlingPatterns = []string{
	"if err ",
	"if err!",
	"!= nil",
	"err :=",
	"err =",
	"return err",
	"fmt.Errorf",
	"errors.New",
	"errors.Is(",
	"errors.As(",
	"errors.Join(",
	".Error()",
	"except ",  // Python
	"catch (",  // Java/TS
	"catch(",
	"rescue ",       // Ruby
	"filepath.Skip", // Go sentinel errors (SkipDir, SkipAll)
}

// maxPropagationLines is the threshold for implicit error propagation detection.
// Short functions that return error without explicit handling are thin wrappers
// propagating errors from the callee (e.g. `return os.RemoveAll(path)`).
const maxPropagationLines = 12

// computeErrorHandlingRatio returns the fraction of eligible functions/methods whose body
// contains reliable error-handling patterns. Only functions that need error handling
// (return error, receive errors, or do I/O) are counted. Test files are excluded.
func computeErrorHandlingRatio(symbols []*parser.Symbol) float64 {
	eligible := 0
	withHandling := 0
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if isTestFile(sym.File) {
			continue
		}
		if !needsErrorHandling(sym) {
			continue
		}
		eligible++
		if hasErrorHandling(sym.Body) {
			withHandling++
			continue
		}
		// Short functions returning error propagate errors implicitly.
		if returnsError(sym.Signature) && funcLines(sym) <= maxPropagationLines {
			withHandling++
		}
	}
	if eligible == 0 {
		return 0
	}
	return float64(withHandling) / float64(eligible)
}

