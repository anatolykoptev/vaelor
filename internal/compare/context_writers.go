package compare

import (
	"fmt"
	"sort"
	"strings"
)

func writeMatchedPairs(sb *strings.Builder, matches []SymbolMatch, hotspotFiles ...map[string]bool) {
	sb.WriteString("## Matched Symbols (side-by-side)\n\n")

	var hsFiles map[string]bool
	if len(hotspotFiles) > 0 {
		hsFiles = hotspotFiles[0]
	}

	type indexedMatch struct {
		idx      int
		priority int
	}
	var pairs []indexedMatch
	for i := range matches {
		if !matches[i].IsGap() {
			p := matchPriority(&matches[i])
			if hsFiles != nil {
				p = matchPriorityWeighted(&matches[i], hsFiles)
			}
			pairs = append(pairs, indexedMatch{idx: i, priority: p})
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
	label := fmt.Sprintf("### %s `%s` (match: %s, score: %.2f, category: %s)",
		m.SymbolA.Kind, m.SymbolA.Name, m.MatchType, m.Score, m.Category)
	if m.Diff != nil && m.Diff.TotalChanges > 0 {
		label += fmt.Sprintf(" [+%d -%d ~%d]", m.Diff.Inserts, m.Diff.Deletes, m.Diff.Updates)
	}
	sb.WriteString(label)
	sb.WriteString("\n\n")

	// Exact matches are byte-identical in both repos; dumping both bodies is
	// pure redundancy and the dominant source of context-budget bloat. Emit a
	// compact note and skip the bodies. Non-exact matches keep (truncated)
	// bodies so the diff summary has surrounding context.
	if m.MatchType == MatchExact {
		sb.WriteString("_Identical implementation in both repos._\n\n")
		return
	}

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
