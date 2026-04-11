package compare

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// hasErrorHandling checks whether a function body contains reliable error handling patterns.
func hasErrorHandling(body string) bool {
	for _, pattern := range errorHandlingPatterns {
		if strings.Contains(body, pattern) {
			return true
		}
	}
	return false
}

// returnsError checks whether a Go-style function signature has error in its return type.
func returnsError(sig string) bool {
	idx := strings.IndexByte(sig, '(')
	if idx < 0 {
		return false
	}
	end := findMatchingParen(sig, idx)
	if end < 0 {
		return false
	}
	rest := sig[end+1:]
	// Skip receiver: if a second paren group follows an identifier, that's the real param list.
	if nextParen := strings.IndexByte(rest, '('); nextParen >= 0 {
		between := strings.TrimSpace(rest[:nextParen])
		if len(between) > 0 && isIdent(between) {
			if end2 := findMatchingParen(rest, nextParen); end2 >= 0 {
				rest = rest[end2+1:]
			}
		}
	}
	return strings.Contains(rest, "error")
}

// needsErrorHandling returns true if a function likely needs error handling:
// returns error, assigns err, or performs I/O operations.
func needsErrorHandling(sym *parser.Symbol) bool {
	if returnsError(sym.Signature) {
		return true
	}
	if strings.Contains(sym.Body, "err :=") || strings.Contains(sym.Body, "err =") {
		return true
	}
	for _, pat := range ioPatterns {
		if strings.Contains(sym.Body, pat) {
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
