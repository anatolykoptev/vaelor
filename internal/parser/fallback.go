package parser

import (
	"math"
	"regexp"
	"strings"
)

// fallbackPatterns are regex patterns for common function/class/method declarations.
var fallbackPatterns = []*fallbackPattern{
	// function/def patterns: def foo(...), func foo(...), fn foo(...), function foo(...)
	{
		re:   regexp.MustCompile(`(?m)^[ \t]*(?:def|func|fun|fn|function|sub)\s+(?P<name>[a-zA-Z_]\w*)\s*\(`),
		kind: KindFunction,
	},
	// class patterns: class Foo, class Foo(Bar), class Foo:
	{
		re:   regexp.MustCompile(`(?m)^[ \t]*class\s+(?P<name>[A-Z]\w*)`),
		kind: KindClass,
	},
	// method patterns (indented function defs — 2+ spaces/tabs)
	{
		re:   regexp.MustCompile(`(?m)^[ \t]{2,}(?:def|func|fun|fn|function|sub)\s+(?P<name>[a-zA-Z_]\w*)\s*\(`),
		kind: KindMethod,
	},
}

type fallbackPattern struct {
	re   *regexp.Regexp
	kind NodeKind
}

// fallbackParse extracts symbols using regex patterns when no tree-sitter handler exists.
func fallbackParse(path string, source []byte, lang string) *ParseResult {
	result := &ParseResult{
		File:     path,
		Language: lang,
		Symbols:  make([]*Symbol, 0),
		Imports:  make([]string, 0),
	}

	text := string(source)
	lines := strings.Split(text, "\n")
	seen := make(map[string]struct{})

	for _, fp := range fallbackPatterns {
		nameIdx := fp.re.SubexpIndex("name")
		if nameIdx < 0 {
			continue
		}
		matches := fp.re.FindAllStringSubmatchIndex(text, -1)
		for _, loc := range matches {
			name := text[loc[nameIdx*2]:loc[nameIdx*2+1]]
			dedupeKey := string(fp.kind) + ":" + name
			if _, exists := seen[dedupeKey]; exists {
				continue
			}
			seen[dedupeKey] = struct{}{}

			lineNum := strings.Count(text[:loc[0]], "\n") + 1

			sig := ""
			if lineNum > 0 && lineNum <= len(lines) {
				sig = strings.TrimSpace(lines[lineNum-1])
			}

			lineU32 := safeIntToUint32(lineNum)
			result.Symbols = append(result.Symbols, &Symbol{
				Name:      name,
				Kind:      fp.kind,
				Language:  lang,
				File:      path,
				StartLine: lineU32,
				EndLine:   lineU32,
				Signature: sig,
			})
		}
	}

	return result
}

// safeIntToUint32 converts an int to uint32 with bounds clamping.
func safeIntToUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v) //nolint:gosec // bounds checked above
}
