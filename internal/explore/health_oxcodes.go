package explore

import (
	"context"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

// OxCodesHealthChecks holds additional quality indicators from ox-codes scoped search.
type OxCodesHealthChecks struct {
	TodoCount       int `json:"todo_count,omitempty"`
	UnhandledErrors int `json:"unhandled_errors,omitempty"`
	MagicNumbers    int `json:"magic_numbers_in_functions,omitempty"`
}

// RunOxCodesHealthChecks runs additional quality checks via ox-codes scoped search.
// Returns nil when client is nil or all checks fail.
func RunOxCodesHealthChecks(ctx context.Context, client *oxcodes.Client, root, language string) *OxCodesHealthChecks {
	if client == nil {
		return nil
	}

	checks := &OxCodesHealthChecks{}

	// 1. TODO/FIXME/HACK/XXX density in comments.
	todoResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: "TODO|FIXME|HACK|XXX", Scope: "comments",
		Language: language, IsRegex: true, MaxResults: 200, CaseSensitive: false,
	})
	if err != nil {
		slog.Warn("health: ox-codes TODO check failed", "err", err)
	} else {
		checks.TodoCount = todoResult.TotalMatches
	}

	// 2. Unhandled errors: blank discard "_ =" pattern in function bodies.
	errResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: `_ =`, Scope: "function_bodies",
		Language: language, MaxResults: 200, CaseSensitive: true,
	})
	if err != nil {
		slog.Warn("health: ox-codes unhandled error check failed", "err", err)
	} else {
		checks.UnhandledErrors = errResult.TotalMatches
	}

	// 3. Magic numbers in function bodies (2+ digit numbers, not 0 or 1).
	magicResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: `\b[2-9]\d+\b`, Scope: "function_bodies",
		Language: language, IsRegex: true, MaxResults: 200, CaseSensitive: true,
	})
	if err != nil {
		slog.Warn("health: ox-codes magic number check failed", "err", err)
	} else {
		checks.MagicNumbers = magicResult.TotalMatches
	}

	return checks
}
