package ingest

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// ContentFilter returns file paths where ANY keyword from focus matches
// a symbol name, import path, or call site (case-insensitive, OR logic).
// Returns nil when focus is empty or has no keywords.
func ContentFilter(focus string, symbols []*parser.Symbol, imports map[string][]string, calls []parser.CallSite) map[string]bool {
	keywords := strings.Fields(strings.ToLower(focus))
	if len(keywords) == 0 {
		return nil
	}

	symsByFile := groupSymbolsByFile(symbols)
	callsByFile := groupCallsByFile(calls)

	allFiles := make(map[string]struct{})
	for path := range symsByFile {
		allFiles[path] = struct{}{}
	}
	for path := range imports {
		allFiles[path] = struct{}{}
	}
	for path := range callsByFile {
		allFiles[path] = struct{}{}
	}

	matched := make(map[string]bool)
	for path := range allFiles {
		if fileMatchesAnyKeyword(symsByFile[path], imports[path], callsByFile[path], keywords) {
			matched[path] = true
		}
	}
	return matched
}

// FilterFiles returns only files whose absolute path is in the matched set.
func FilterFiles(files []*File, matched map[string]bool) []*File {
	if len(matched) == 0 {
		return nil
	}
	out := make([]*File, 0, len(matched))
	for _, f := range files {
		if matched[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

func groupSymbolsByFile(symbols []*parser.Symbol) map[string][]*parser.Symbol {
	m := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		m[s.File] = append(m[s.File], s)
	}
	return m
}

func groupCallsByFile(calls []parser.CallSite) map[string][]parser.CallSite {
	m := make(map[string][]parser.CallSite)
	for _, c := range calls {
		m[c.File] = append(m[c.File], c)
	}
	return m
}

func fileMatchesAnyKeyword(syms []*parser.Symbol, imps []string, fileCalls []parser.CallSite, keywords []string) bool {
	for _, kw := range keywords {
		if kwInSymbols(syms, kw) || kwInImports(imps, kw) || kwInCalls(fileCalls, kw) {
			return true
		}
	}
	return false
}

func kwInSymbols(syms []*parser.Symbol, kw string) bool {
	for _, s := range syms {
		if strings.Contains(strings.ToLower(s.Name), kw) {
			return true
		}
	}
	return false
}

func kwInImports(imps []string, kw string) bool {
	for _, imp := range imps {
		if strings.Contains(strings.ToLower(imp), kw) {
			return true
		}
	}
	return false
}

func kwInCalls(calls []parser.CallSite, kw string) bool {
	for _, c := range calls {
		if strings.Contains(strings.ToLower(c.Name), kw) || strings.Contains(strings.ToLower(c.Receiver), kw) {
			return true
		}
	}
	return false
}
