package compare

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Budget constants for LLM context size limits.
const (
	maxContextChars = 20_000
	maxSnippetChars = 400
	maxMatchedPairs = 20
	maxGapSymbols   = 20
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

// contextWriter wraps a strings.Builder with a budget check.
// Each Write call is a no-op once the budget is exceeded.
type contextWriter struct {
	sb     strings.Builder
	budget int
}

func newContextWriter() *contextWriter { return &contextWriter{budget: maxContextChars} }

// over reports whether the budget has been exhausted.
func (w *contextWriter) over() bool { return w.sb.Len() >= w.budget }

// write calls fn only when budget is not yet exceeded.
// Returns true if the budget was hit after the call.
func (w *contextWriter) write(fn func(*strings.Builder)) bool {
	if w.over() {
		return true
	}
	fn(&w.sb)
	return w.over()
}

// BuildCompareContext assembles structured text context for the LLM (no hotspots).
func BuildCompareContext(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string) string {
	return BuildCompareContextV2(matches, metricsA, metricsB, query, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
func BuildCompareContextV2(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string,
	hotspotsA, hotspotsB []HotspotFile, relStatsA, relStatsB *RelStats,
	freshnessA, freshnessB *FreshnessStats, dataflowA, dataflowB *DataflowStats,
	apiDiff *APIDiff, routeDiff *RouteDiff,
	archA, archB *ArchMetrics) string {
	w := newContextWriter()

	hsFiles := hotspotFileSet(hotspotsA, hotspotsB)

	sections := []func(*strings.Builder){
		func(sb *strings.Builder) { writeQuery(sb, query) },
		func(sb *strings.Builder) { writeMetrics(sb, metricsA, metricsB) },
		func(sb *strings.Builder) {
			if len(hotspotsA) > 0 || len(hotspotsB) > 0 {
				writeHotspots(sb, hotspotsA, hotspotsB)
			}
		},
		func(sb *strings.Builder) {
			if relStatsA != nil || relStatsB != nil {
				writeRelStats(sb, relStatsA, relStatsB)
			}
		},
		func(sb *strings.Builder) { writeFreshness(sb, freshnessA, freshnessB) },
		func(sb *strings.Builder) { writeDataflow(sb, dataflowA, dataflowB) },
		func(sb *strings.Builder) { writeAPISurface(sb, apiDiff) },
		func(sb *strings.Builder) { writeRoutesDiff(sb, routeDiff) },
		func(sb *strings.Builder) { writeArchMetrics(sb, archA, archB) },
		func(sb *strings.Builder) { writeMatchedPairs(sb, matches, hsFiles) },
		func(sb *strings.Builder) { writeGaps(sb, matches) },
	}

	for _, fn := range sections {
		if w.write(fn) {
			break
		}
	}

	return w.sb.String()
}

// hotspotFileSet builds a set of file paths from hotspot slices.
func hotspotFileSet(hotspotsA, hotspotsB []HotspotFile) map[string]bool {
	files := make(map[string]bool, len(hotspotsA)+len(hotspotsB))
	for _, h := range hotspotsA {
		files[h.File] = true
	}
	for _, h := range hotspotsB {
		files[h.File] = true
	}
	return files
}

func writeQuery(sb *strings.Builder, query string) {
	sb.WriteString("## Query\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")
}

func writeMetrics(sb *strings.Builder, metricsA, metricsB RepoMetrics) {
	payload := metricsJSON{RepoA: metricsA, RepoB: metricsB}
	data, err := json.Marshal(payload)
	if err != nil {
		sb.WriteString("## Metrics\n(unavailable)\n\n")
		return
	}
	sb.WriteString("## Metrics\n")
	sb.Write(data)
	sb.WriteString("\n\n")
}

// hotspotBoost is subtracted from priority for symbols in hotspot files.
const hotspotBoost = 10

// matchPriorityWeighted returns priority adjusted by hotspot membership.
// Lower = higher priority.
func matchPriorityWeighted(m *SymbolMatch, hotspotFiles map[string]bool) int {
	p := matchPriority(m)
	if m.SymbolA != nil && hotspotFiles[m.SymbolA.File] {
		p -= hotspotBoost
	}
	return p
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
