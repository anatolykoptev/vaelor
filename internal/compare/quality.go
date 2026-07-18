package compare

import (
	"context"
	"log/slog"
	"sync"

	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// DataflowStats holds dead code and data flow findings from ox-codes.
type DataflowStats struct {
	DeadStores    int `json:"deadStores"`
	UnusedVars    int `json:"unusedVars"`
	TotalFindings int `json:"totalFindings"`
	FilesAnalyzed int `json:"filesAnalyzed"`
}

// QualityIndicators summarizes code quality signals from ox-codes.
type QualityIndicators struct {
	TodoCount     int `json:"todo_count"`
	ErrorPatterns int `json:"error_patterns"` // unhandled errors
	PanicCount    int `json:"panic_count"`    // panic() calls
	MagicNumbers  int `json:"magic_numbers"`
}

// GatherQualityIndicators runs ox-codes scoped searches to assess code quality.
// All searches run in parallel for speed.
func GatherQualityIndicators(ctx context.Context, client *oxcodes.Client, root, language string) *QualityIndicators {
	if client == nil || language == "" {
		return nil
	}

	qi := &QualityIndicators{}
	var wg sync.WaitGroup

	wg.Add(4)

	// TODO/FIXME count
	go func() {
		defer wg.Done()
		if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
			Root: root, Pattern: "TODO|FIXME", Scope: "comments",
			Language: language, IsRegex: true, MaxResults: 200, CaseSensitive: false,
		}); err == nil {
			qi.TodoCount = result.TotalMatches
		} else {
			slog.Warn("compare: ox-codes TODO check failed", "err", err)
		}
	}()

	// Unhandled errors
	go func() {
		defer wg.Done()
		if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
			Root: root, Pattern: `_ =`, Scope: "function_bodies",
			Language: language, MaxResults: 200, CaseSensitive: true,
		}); err == nil {
			qi.ErrorPatterns = result.TotalMatches
		}
	}()

	// Panic calls
	go func() {
		defer wg.Done()
		if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
			Root: root, Pattern: `panic(`, Scope: "function_bodies",
			Language: language, MaxResults: 200, CaseSensitive: true,
		}); err == nil {
			qi.PanicCount = result.TotalMatches
		}
	}()

	// Magic numbers
	go func() {
		defer wg.Done()
		if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
			Root: root, Pattern: `\b[2-9]\d{2,}\b`, Scope: "function_bodies",
			Language: language, IsRegex: true, MaxResults: 200, CaseSensitive: true,
		}); err == nil {
			qi.MagicNumbers = result.TotalMatches
		}
	}()

	wg.Wait()
	return qi
}

// GatherDataflow runs ox-codes dataflow analysis on a repo.
func GatherDataflow(ctx context.Context, client *oxcodes.Client, root, language string) *DataflowStats {
	if client == nil || language == "" {
		return nil
	}

	result, err := client.DataflowAnalyze(ctx, oxcodes.DataflowInput{
		Root:       root,
		Language:   language,
		MaxResults: 500,
	})
	if err != nil {
		slog.Warn("compare: ox-codes dataflow failed", "err", err)
		return nil
	}

	stats := &DataflowStats{
		TotalFindings: result.TotalFindings,
		FilesAnalyzed: result.FilesAnalyzed,
	}
	for _, f := range result.Findings {
		switch f.Kind {
		case "dead_store":
			stats.DeadStores++
		case "unused_variable":
			stats.UnusedVars++
		}
	}
	return stats
}
