package compare

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// isExported reports whether a symbol name is exported (starts with an uppercase letter).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
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

// errorHandlingPatterns are substrings that reliably indicate error handling in function bodies.
var errorHandlingPatterns = []string{
	"if err ",
	"if err!",
	"!= nil",
	"err :=",
	"err =",
	"return err",
	"return fmt.Errorf",
	"errors.New",
	"errors.Is(",
	"errors.As(",
	"errors.Join(",
	".Error()",
	"except ",  // Python
	"catch (",  // Java/TS
	"catch(",
	"rescue ",  // Ruby
}

// computeErrorHandlingRatio returns the fraction of functions/methods whose body
// contains reliable error-handling patterns.
func computeErrorHandlingRatio(symbols []*parser.Symbol) float64 {
	funcCount := 0
	withErrorHandling := 0
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		funcCount++
		if hasErrorHandling(sym.Body) {
			withErrorHandling++
		}
	}
	if funcCount == 0 {
		return 0
	}
	return float64(withErrorHandling) / float64(funcCount)
}

// hasErrorHandling checks whether a function body contains reliable error handling patterns.
func hasErrorHandling(body string) bool {
	for _, pattern := range errorHandlingPatterns {
		if strings.Contains(body, pattern) {
			return true
		}
	}
	return false
}

// countInterfaces returns the number of interface symbols.
func countInterfaces(symbols []*parser.Symbol) int {
	count := 0
	for _, sym := range symbols {
		if sym.Kind == parser.KindInterface {
			count++
		}
	}
	return count
}

// countFuncParams counts parameters in a function signature.
// Skips Go receivers and Python self/cls.
func countFuncParams(sig string) int {
	start := strings.IndexByte(sig, '(')
	if start < 0 {
		return 0
	}
	inner := extractParamList(sig, start)
	if inner == "" {
		return 0
	}
	params := splitParams(inner)
	count := 0
	for _, p := range params {
		p = strings.TrimSpace(p)
		if p == "" || p == "self" || p == "cls" {
			continue
		}
		count++
	}
	return count
}

// extractParamList finds the parameter list, skipping Go receivers.
func extractParamList(sig string, firstParen int) string {
	end := findMatchingParen(sig, firstParen)
	if end < 0 {
		return ""
	}
	// Check if there's another paren group after (means first was receiver).
	rest := sig[end+1:]
	nextParen := strings.IndexByte(rest, '(')
	if nextParen >= 0 {
		between := strings.TrimSpace(rest[:nextParen])
		if len(between) > 0 && isIdent(between) {
			nextEnd := findMatchingParen(rest, nextParen)
			if nextEnd >= 0 {
				return rest[nextParen+1 : nextEnd]
			}
		}
	}
	return sig[firstParen+1 : end]
}

func findMatchingParen(s string, open int) int {
	depth := 1
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isIdent(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '*' && r != '[' && r != ']' && r != ' ' {
			return false
		}
	}
	return true
}

// splitParams splits a parameter list by commas, respecting parentheses depth.
// Commas inside nested parens (e.g., func(K, V)) are not treated as separators.
func splitParams(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// computeParamMetrics returns average and max parameter count across functions/methods.
func computeParamMetrics(symbols []*parser.Symbol) (avg float64, max int) {
	total := 0
	count := 0
	for _, sym := range symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if sym.Signature == "" {
			continue
		}
		params := countFuncParams(sym.Signature)
		total += params
		count++
		if params > max {
			max = params
		}
	}
	if count > 0 {
		avg = float64(total) / float64(count)
	}
	return avg, max
}
