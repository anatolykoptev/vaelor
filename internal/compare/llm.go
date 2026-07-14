package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/anatolykoptev/go-kit/llm"
)

// runLLMAnalysis sends the comparison context to the LLM and parses its response.
// Returns a fallback analysis with the error message if the LLM call fails.
func runLLMAnalysis(ctx context.Context, client llm.Completer, matches []SymbolMatch,
	metricsA, metricsB RepoMetrics, query string,
	hotspotsA, hotspotsB []HotspotFile, relStatsA, relStatsB *RelStats,
	freshnessA, freshnessB *FreshnessStats, dataflowA, dataflowB *DataflowStats,
	apiDiff *APIDiff, routeDiff *RouteDiff,
	archA, archB *ArchMetrics) LLMAnalysis {
	compareCtx := BuildCompareContextV2(matches, metricsA, metricsB, query,
		hotspotsA, hotspotsB, relStatsA, relStatsB,
		freshnessA, freshnessB, dataflowA, dataflowB, apiDiff, routeDiff,
		archA, archB)
	llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	answer, err := client.Complete(llmCtx, prompts.SystemPromptCodeCompare, compareCtx)
	if err != nil {
		return LLMAnalysis{
			Recommendations: []string{fmt.Sprintf("LLM analysis unavailable: %v", err)},
		}
	}
	return parseAnalysis(answer)
}

// parseAnalysis tries to parse LLM response as JSON LLMAnalysis.
// Falls back to wrapping raw text in recommendations.
func parseAnalysis(raw string) LLMAnalysis {
	cleaned := extractJSON(raw)

	var analysis LLMAnalysis
	if err := json.Unmarshal([]byte(cleaned), &analysis); err != nil {
		return LLMAnalysis{
			Recommendations: []string{raw},
		}
	}
	return analysis
}

// extractJSON tries to extract a JSON block from markdown-wrapped LLM output.
func extractJSON(s string) string {
	start := strings.Index(s, "```json")
	if start >= 0 {
		s = s[start+7:]
		end := strings.Index(s, "```")
		if end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	start = strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
