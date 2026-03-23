package impact

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

const maxHiddenCallerSearch = 30

// HiddenCaller represents a potential caller found via text search.
type HiddenCaller struct {
	File       string  `json:"file"`
	Line       int     `json:"line"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"` // 0.4-0.6
}

// FindHiddenCallers searches for string references to a symbol that
// the call graph might have missed (callbacks, config, reflection).
func FindHiddenCallers(ctx context.Context, client *oxcodes.Client, root, symbolName, language string) []HiddenCaller {
	if client == nil {
		return nil
	}

	// Search 1: symbol name in function bodies (catches callback registration).
	scopedResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root:          root,
		Pattern:       symbolName,
		Scope:         "function_bodies",
		Language:      language,
		MaxResults:    maxHiddenCallerSearch,
		CaseSensitive: true,
	})
	if err != nil {
		slog.Warn("impact: ox-codes scoped search failed", "symbol", symbolName, "err", err)
		return nil
	}

	// Search 2: symbol as string literal (catches reflection, config).
	stringResult, err := client.Search(ctx, oxcodes.SearchInput{
		Root:          root,
		Pattern:       `"` + symbolName + `"`,
		MaxResults:    maxHiddenCallerSearch,
		CaseSensitive: true,
	})
	if err != nil {
		slog.Warn("impact: ox-codes string search failed", "symbol", symbolName, "err", err)
		// Continue with scoped results only.
	}

	// Merge and deduplicate by file:line.
	seen := make(map[string]struct{})
	var hidden []HiddenCaller

	// Scoped results have higher confidence — pattern appears in function body.
	if scopedResult != nil {
		for _, m := range scopedResult.Matches {
			key := m.File + ":" + fmt.Sprint(m.Line)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hidden = append(hidden, HiddenCaller{
				File:       m.File,
				Line:       m.Line,
				Text:       m.Text,
				Confidence: 0.6,
			})
		}
	}

	// String literal results have lower confidence — might be documentation.
	if stringResult != nil {
		for _, m := range stringResult.Matches {
			key := m.File + ":" + fmt.Sprint(m.Line)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hidden = append(hidden, HiddenCaller{
				File:       m.File,
				Line:       m.Line,
				Text:       m.Text,
				Confidence: 0.4,
			})
		}
	}

	return hidden
}
