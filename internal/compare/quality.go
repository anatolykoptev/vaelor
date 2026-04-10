package compare

import (
	"context"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

// QualityIndicators summarizes code quality signals from ox-codes.
type QualityIndicators struct {
	TodoCount     int `json:"todo_count"`
	ErrorPatterns int `json:"error_patterns"` // unhandled errors
	PanicCount    int `json:"panic_count"`    // panic() calls
	MagicNumbers  int `json:"magic_numbers"`
}

// GatherQualityIndicators runs ox-codes scoped searches to assess code quality.
func GatherQualityIndicators(ctx context.Context, client *oxcodes.Client, root, language string) *QualityIndicators {
	if client == nil || language == "" {
		return nil
	}

	qi := &QualityIndicators{}

	// TODO/FIXME count
	if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: "TODO|FIXME", Scope: "comments",
		Language: language, IsRegex: true, MaxResults: 200, CaseSensitive: false,
	}); err == nil {
		qi.TodoCount = result.TotalMatches
	} else {
		slog.Warn("compare: ox-codes TODO check failed", "err", err)
	}

	// Unhandled errors
	if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: `_ =`, Scope: "function_bodies",
		Language: language, MaxResults: 200, CaseSensitive: true,
	}); err == nil {
		qi.ErrorPatterns = result.TotalMatches
	}

	// Panic calls
	if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: `panic(`, Scope: "function_bodies",
		Language: language, MaxResults: 200, CaseSensitive: true,
	}); err == nil {
		qi.PanicCount = result.TotalMatches
	}

	// Magic numbers
	if result, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: `\b[2-9]\d{2,}\b`, Scope: "function_bodies",
		Language: language, IsRegex: true, MaxResults: 200, CaseSensitive: true,
	}); err == nil {
		qi.MagicNumbers = result.TotalMatches
	}

	return qi
}
