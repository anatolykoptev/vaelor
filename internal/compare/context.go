package compare

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Budget constants for LLM context size limits.
const (
	maxContextChars = 180_000
	maxSnippetChars = 3_000
	maxMatchedPairs = 80
	maxGapSymbols   = 40
)

// truncate returns s unchanged when len(s) <= maxLen, otherwise truncates
// at a valid UTF-8 boundary and appends a marker.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Walk back from maxLen to find a valid rune boundary.
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n... (truncated)"
}

// metricsJSON is the JSON shape written into the Metrics section.
type metricsJSON struct {
	RepoA RepoMetrics `json:"repo_a"`
	RepoB RepoMetrics `json:"repo_b"`
}

// BuildCompareContext assembles a structured text context for the LLM.
//
// Sections:
//  1. ## Query — the user's question
//  2. ## Metrics — JSON comparison of aggregate quality metrics
//  3. ## Matched Symbols (side-by-side) — non-gap pairs up to maxMatchedPairs
//  4. ## Coverage Gaps — symbols absent from one side, up to maxGapSymbols
//
// Content is truncated once the cumulative output exceeds maxContextChars.
func BuildCompareContext(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string) string {
	var sb strings.Builder

	writeQuery(&sb, query)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	writeMetrics(&sb, metricsA, metricsB)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	writeMatchedPairs(&sb, matches)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	writeGaps(&sb, matches)

	return sb.String()
}

func writeQuery(sb *strings.Builder, query string) {
	sb.WriteString("## Query\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")
}

func writeMetrics(sb *strings.Builder, metricsA, metricsB RepoMetrics) {
	payload := metricsJSON{RepoA: metricsA, RepoB: metricsB}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		sb.WriteString("## Metrics\n(unavailable)\n\n")
		return
	}
	sb.WriteString("## Metrics\n")
	sb.Write(data)
	sb.WriteString("\n\n")
}

// matchPriority returns a sort key for symbol matches. Lower = higher priority.
// Modified/renamed/fuzzy matches are more interesting than identical exact matches.
func matchPriority(m *SymbolMatch) int {
	switch m.MatchType {
	case MatchModified:
		return 0
	case MatchRenamed:
		return 1
	case MatchFuzzy:
		return 2
	case MatchSemantic:
		return 3
	case MatchExact:
		return 4
	default:
		return 5
	}
}

func writeMatchedPairs(sb *strings.Builder, matches []SymbolMatch) {
	sb.WriteString("## Matched Symbols (side-by-side)\n\n")

	type indexedMatch struct {
		idx      int
		priority int
	}
	var pairs []indexedMatch
	for i := range matches {
		if !matches[i].IsGap() {
			pairs = append(pairs, indexedMatch{idx: i, priority: matchPriority(&matches[i])})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].priority < pairs[j].priority
	})

	written := 0
	for _, p := range pairs {
		if written >= maxMatchedPairs {
			break
		}
		if sb.Len() >= maxContextChars {
			break
		}
		writePair(sb, &matches[p.idx])
		written++
	}
}

func writePair(sb *strings.Builder, m *SymbolMatch) {
	fmt.Fprintf(sb, "### %s `%s` (match: %s, score: %.2f, category: %s)\n\n",
		m.SymbolA.Kind, m.SymbolA.Name, m.MatchType, m.Score, m.Category)

	sb.WriteString("**Repo A** (`")
	sb.WriteString(m.SymbolA.File)
	sb.WriteString("`):\n```\n")
	sb.WriteString(truncate(m.SymbolA.Body, maxSnippetChars))
	sb.WriteString("\n```\n\n")

	sb.WriteString("**Repo B** (`")
	sb.WriteString(m.SymbolB.File)
	sb.WriteString("`):\n```\n")
	sb.WriteString(truncate(m.SymbolB.Body, maxSnippetChars))
	sb.WriteString("\n```\n\n")
}

func writeGaps(sb *strings.Builder, matches []SymbolMatch) {
	sb.WriteString("## Coverage Gaps\n\n")

	written := 0
	for i := range matches {
		if written >= maxGapSymbols {
			break
		}
		m := &matches[i]
		if !m.IsGap() {
			continue
		}
		if sb.Len() >= maxContextChars {
			break
		}
		writeGap(sb, m)
		written++
	}
}

func writeGap(sb *strings.Builder, m *SymbolMatch) {
	missing := m.MissingIn()
	sym := m.SymbolA
	if sym == nil {
		sym = m.SymbolB
	}
	if sym == nil {
		return // both nil — skip this malformed gap
	}

	fmt.Fprintf(sb, "- MISSING in %s: %s `%s` (%s:%d)\n",
		missing, sym.Kind, sym.Name, sym.File, sym.StartLine)

	if sym.Body != "" {
		sb.WriteString("  ```\n  ")
		sb.WriteString(truncate(sym.Body, maxSnippetChars))
		sb.WriteString("\n  ```\n")
	}
	sb.WriteString("\n")
}
