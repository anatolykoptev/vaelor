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

// Match priority levels for sorting symbol matches in LLM context.
// Lower value = higher priority (more interesting for comparison).
const (
	priorityModified = 0
	priorityRenamed  = 1
	priorityFuzzy    = 2
	prioritySemantic = 3
	priorityExact    = 4
	priorityDefault  = 5
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

// BuildCompareContext assembles structured text context for the LLM (no hotspots).
func BuildCompareContext(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string) string {
	return BuildCompareContextV2(matches, metricsA, metricsB, query, nil, nil, nil, nil)
}

// BuildCompareContextV2 assembles structured text context for the LLM, including hotspot and type hierarchy data.
//
// Sections:
//  1. ## Query — the user's question
//  2. ## Metrics — JSON comparison of aggregate quality metrics
//  3. ## Maintenance Hotspots — high-churn x high-complexity files (if any)
//  4. ## Type Hierarchy — relationship stats (extends/implements/embeds) per repo
//  5. ## Matched Symbols (side-by-side) — non-gap pairs up to maxMatchedPairs
//  6. ## Coverage Gaps — symbols absent from one side, up to maxGapSymbols
//
// Content is truncated once the cumulative output exceeds maxContextChars.
func BuildCompareContextV2(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string, hotspotsA, hotspotsB []HotspotFile, relStatsA, relStatsB *RelStats) string {
	var sb strings.Builder

	writeQuery(&sb, query)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	writeMetrics(&sb, metricsA, metricsB)
	if sb.Len() >= maxContextChars {
		return sb.String()
	}

	if len(hotspotsA) > 0 || len(hotspotsB) > 0 {
		writeHotspots(&sb, hotspotsA, hotspotsB)
		if sb.Len() >= maxContextChars {
			return sb.String()
		}
	}

	if relStatsA != nil || relStatsB != nil {
		writeRelStats(&sb, relStatsA, relStatsB)
		if sb.Len() >= maxContextChars {
			return sb.String()
		}
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
		return priorityModified
	case MatchRenamed:
		return priorityRenamed
	case MatchFuzzy:
		return priorityFuzzy
	case MatchSemantic:
		return prioritySemantic
	case MatchExact:
		return priorityExact
	default:
		return priorityDefault
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

	if m.Diff != nil && m.Diff.TotalChanges > 0 {
		writeDiffSummary(sb, m.Diff)
	}
}

func writeDiffSummary(sb *strings.Builder, diff *DiffSummary) {
	fmt.Fprintf(sb, "**Structural changes** (%d total: +%d -%d ~%d move:%d):\n",
		diff.TotalChanges, diff.Inserts, diff.Deletes, diff.Updates, diff.Moves)
	for _, c := range diff.Changes {
		sb.WriteString("- ")
		sb.WriteString(c)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
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

// maxHotspotsInContext limits hotspots shown to LLM.
const maxHotspotsInContext = 10

func writeHotspots(sb *strings.Builder, hotspotsA, hotspotsB []HotspotFile) {
	sb.WriteString("## Maintenance Hotspots\n\n")

	if len(hotspotsA) > 0 {
		sb.WriteString("**Repo A hotspots** (high churn × high complexity):\n")
		limit := len(hotspotsA)
		if limit > maxHotspotsInContext {
			limit = maxHotspotsInContext
		}
		for _, h := range hotspotsA[:limit] {
			fmt.Fprintf(sb, "- `%s` — risk: %s, score: %.2f, churn: %d, complexity: %.1f\n",
				h.File, h.Risk, h.Score, h.Churn, h.Complexity)
		}
		sb.WriteString("\n")
	}

	if len(hotspotsB) > 0 {
		sb.WriteString("**Repo B hotspots** (high churn × high complexity):\n")
		limit := len(hotspotsB)
		if limit > maxHotspotsInContext {
			limit = maxHotspotsInContext
		}
		for _, h := range hotspotsB[:limit] {
			fmt.Fprintf(sb, "- `%s` — risk: %s, score: %.2f, churn: %d, complexity: %.1f\n",
				h.File, h.Risk, h.Score, h.Churn, h.Complexity)
		}
		sb.WriteString("\n")
	}
}

func writeRelStats(sb *strings.Builder, statsA, statsB *RelStats) {
	sb.WriteString("## Type Hierarchy\n\n")
	if statsA != nil {
		fmt.Fprintf(sb, "**Repo A**: %d relationships (%d extends, %d implements, %d embeds) across %d types\n",
			statsA.Total, statsA.Extends, statsA.Implements, statsA.Embeds, statsA.UniqueSubjects)
	} else {
		sb.WriteString("**Repo A**: no type relationships detected\n")
	}
	if statsB != nil {
		fmt.Fprintf(sb, "**Repo B**: %d relationships (%d extends, %d implements, %d embeds) across %d types\n",
			statsB.Total, statsB.Extends, statsB.Implements, statsB.Embeds, statsB.UniqueSubjects)
	} else {
		sb.WriteString("**Repo B**: no type relationships detected\n")
	}
	sb.WriteString("\n")
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
