package codegraph

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// ExtractTestedByEdges creates TESTED_BY edges mapping test functions to tested symbols.
func ExtractTestedByEdges(root string, symbols []*parser.Symbol) []edgeData {
	byName := make(map[string][]*parser.Symbol)
	byFile := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		if isTestSymbol(s) {
			continue
		}
		byName[s.Name] = append(byName[s.Name], s)
		byFile[s.File] = append(byFile[s.File], s)
	}

	var edges []edgeData
	seen := make(map[string]bool)

	for _, s := range symbols {
		if !isTestSymbol(s) {
			continue
		}

		targets := resolveTestTarget(s, byName)
		if len(targets) == 0 {
			srcFile := guessSourceFile(s.File, s.Language)
			if srcFile != "" {
				targets = byFile[srcFile]
			}
		}

		for _, tgt := range targets {
			fromKey := s.Name + ":" + relPathOrSelf(s.File, root)
			toKey := tgt.Name + ":" + relPathOrSelf(tgt.File, root)
			key := fromKey + "->" + toKey
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, edgeData{
				FromLabel: "Symbol",
				FromKey:   fromKey,
				ToLabel:   "Symbol",
				ToKey:     toKey,
				EdgeLabel: "TESTED_BY",
				Props:     map[string]string{},
			})
		}
	}

	return edges
}

func resolveTestTarget(test *parser.Symbol, byName map[string][]*parser.Symbol) []*parser.Symbol {
	switch test.Language {
	case "go":
		return resolveGoTest(test, byName)
	case "python":
		return resolvePythonTest(test, byName)
	default:
		return nil
	}
}

func resolveGoTest(test *parser.Symbol, byName map[string][]*parser.Symbol) []*parser.Symbol {
	name := test.Name
	for _, prefix := range []string{"Test_", "Test", "Benchmark"} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		if parts := strings.SplitN(rest, "_", 2); len(parts) > 0 {
			if targets := byName[parts[0]]; len(targets) > 0 {
				return targets
			}
		}
		if targets := byName[rest]; len(targets) > 0 {
			return targets
		}
	}
	return nil
}

func resolvePythonTest(test *parser.Symbol, byName map[string][]*parser.Symbol) []*parser.Symbol {
	name := test.Name
	if strings.HasPrefix(name, "test_") {
		target := strings.TrimPrefix(name, "test_")
		if targets := byName[target]; len(targets) > 0 {
			return targets
		}
	}
	if strings.HasPrefix(name, "Test") {
		target := strings.TrimPrefix(name, "Test")
		if targets := byName[target]; len(targets) > 0 {
			return targets
		}
	}
	return nil
}

func isTestSymbol(s *parser.Symbol) bool {
	if s.Kind != parser.KindFunction && s.Kind != parser.KindMethod && s.Kind != parser.KindType {
		return false
	}
	switch s.Language {
	case "go":
		return strings.HasPrefix(s.Name, "Test") || strings.HasPrefix(s.Name, "Benchmark")
	case "python":
		return strings.HasPrefix(s.Name, "test_") || strings.HasPrefix(s.Name, "Test")
	default:
		return langutil.IsTestFile(s.File)
	}
}

func guessSourceFile(testFile, lang string) string {
	base := filepath.Base(testFile)
	dir := filepath.Dir(testFile)
	switch lang {
	case "go":
		return filepath.Join(dir, strings.TrimSuffix(base, "_test.go")+".go")
	case "python":
		if strings.HasPrefix(base, "test_") {
			return filepath.Join(dir, strings.TrimPrefix(base, "test_"))
		}
	}
	return ""
}

func relPathOrSelf(path, root string) string {
	return langutil.RelPath(path, root)
}
