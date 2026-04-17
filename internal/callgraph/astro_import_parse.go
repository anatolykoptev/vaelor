package callgraph

import "strings"

// parseImportLine parses a single ES module import statement and adds
// binding-name → import-path entries to bindings.
//
// Handles:
//   - import Foo from './Foo.astro'
//   - import { A, B as C } from './lib'
//   - import * as Ns from './ns'
//   - import Foo, { Bar } from './combo'
//
// Lines that don't match are silently skipped.
func parseImportLine(line string, bindings map[string]string) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "import "))

	fromIdx := strings.LastIndex(rest, " from ")
	if fromIdx < 0 {
		return
	}
	importPath := extractQuoted(strings.TrimSpace(rest[fromIdx+6:]))
	if importPath == "" {
		return
	}

	clause := strings.TrimSpace(rest[:fromIdx])

	// Named imports: { A, B as C, ... }
	if strings.HasPrefix(clause, "{") {
		end := strings.Index(clause, "}")
		if end < 0 {
			return
		}
		for _, n := range strings.Split(clause[1:end], ",") {
			n = strings.TrimSpace(n)
			if asIdx := strings.Index(n, " as "); asIdx >= 0 {
				if alias := strings.TrimSpace(n[asIdx+4:]); alias != "" {
					bindings[alias] = importPath
				}
			} else if n != "" {
				bindings[n] = importPath
			}
		}
		return
	}

	// Namespace import: * as Ns
	if strings.HasPrefix(clause, "* as ") {
		if ns := strings.TrimSpace(strings.TrimPrefix(clause, "* as ")); ns != "" {
			bindings[ns] = importPath
		}
		return
	}

	// Default import, possibly with trailing named imports:
	// "Foo" or "Foo, { Bar }".
	comma := strings.Index(clause, ",")
	defaultName := clause
	if comma >= 0 {
		defaultName = strings.TrimSpace(clause[:comma])
	}
	if defaultName != "" && !strings.ContainsAny(defaultName, "{}* ") {
		bindings[defaultName] = importPath
	}
}

// extractQuoted extracts the content of a single- or double-quoted string.
// Returns "" if s does not start with a quote character.
func extractQuoted(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}
	q := s[0]
	if q != '\'' && q != '"' && q != '`' {
		return ""
	}
	end := strings.IndexByte(s[1:], q)
	if end < 0 {
		return ""
	}
	return s[1 : end+1]
}
