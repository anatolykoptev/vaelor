package explore

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// contentFilter returns the set of file paths where ANY keyword from focus
// matches a symbol name, import path, or call site (case-insensitive, OR logic).
func contentFilter(focus string, symbols []*parser.Symbol, imports map[string][]string, calls []parser.CallSite) map[string]bool {
	keywords := strings.Fields(strings.ToLower(focus))
	if len(keywords) == 0 {
		return nil
	}

	symsByFile := symbolsByFile(symbols)
	callsByF := callsByFile(calls)

	// Collect all unique file paths.
	allFiles := make(map[string]bool)
	for path := range symsByFile {
		allFiles[path] = false
	}
	for path := range imports {
		allFiles[path] = false
	}
	for path := range callsByF {
		allFiles[path] = false
	}

	matched := make(map[string]bool)
	for path := range allFiles {
		if fileMatchesAnyKeyword(symsByFile[path], imports[path], callsByF[path], keywords) {
			matched[path] = true
		}
	}
	return matched
}

// symbolsByFile groups symbols by their file path.
func symbolsByFile(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		m[s.File] = append(m[s.File], s)
	}
	return m
}

// callsByFile groups call sites by their file path.
func callsByFile(calls []parser.CallSite) map[string][]parser.CallSite {
	m := make(map[string][]parser.CallSite)
	for _, c := range calls {
		m[c.File] = append(m[c.File], c)
	}
	return m
}

// fileMatchesAnyKeyword returns true if ANY keyword appears in the file's
// symbol names, import paths, or call site names/receivers.
func fileMatchesAnyKeyword(syms []*parser.Symbol, imps []string, fileCalls []parser.CallSite, keywords []string) bool {
	for _, kw := range keywords {
		if keywordInSymbols(syms, kw) || keywordInImports(imps, kw) || keywordInCalls(fileCalls, kw) {
			return true
		}
	}
	return false
}

func keywordInSymbols(syms []*parser.Symbol, kw string) bool {
	for _, s := range syms {
		if strings.Contains(strings.ToLower(s.Name), kw) {
			return true
		}
	}
	return false
}

func keywordInImports(imps []string, kw string) bool {
	for _, imp := range imps {
		if strings.Contains(strings.ToLower(imp), kw) {
			return true
		}
	}
	return false
}

func keywordInCalls(calls []parser.CallSite, kw string) bool {
	for _, c := range calls {
		if strings.Contains(strings.ToLower(c.Name), kw) || strings.Contains(strings.ToLower(c.Receiver), kw) {
			return true
		}
	}
	return false
}
