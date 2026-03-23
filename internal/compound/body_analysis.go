package compound

import (
	"context"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// BodyAnalysis contains additional analysis of a symbol's implementation.
type BodyAnalysis struct {
	ErrorExits      int  `json:"error_exits"`       // panic/return err patterns
	HasDeferCleanup bool `json:"has_defer_cleanup"`
	HasTODO         bool `json:"has_todo"`
}

// AnalyzeBody uses ox-codes to examine a symbol's internal patterns.
func AnalyzeBody(ctx context.Context, client *oxcodes.Client, root string, sym *parser.Symbol) *BodyAnalysis {
	if client == nil || sym == nil {
		return nil
	}

	lang := detectLanguage(sym.File)
	if lang == "" {
		return nil
	}

	analysis := &BodyAnalysis{}

	// Search for error exits (panic, return err) in this file.
	errResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: `panic\(|return err|return fmt\.Errorf`,
		Scope: "function_bodies", Language: lang, IsRegex: true,
		MaxResults: 50, CaseSensitive: true,
	})
	if err != nil {
		slog.Warn("understand: ox-codes error analysis failed", "err", err)
	} else {
		for _, m := range errResult.Matches {
			if m.File == sym.File && m.Line >= int(sym.StartLine) && m.Line <= int(sym.EndLine) {
				analysis.ErrorExits++
			}
		}
	}

	// Search for defer in this file.
	deferResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: "defer ",
		Scope: "function_bodies", Language: lang,
		MaxResults: 50, CaseSensitive: true,
	})
	if err == nil {
		for _, m := range deferResult.Matches {
			if m.File == sym.File && m.Line >= int(sym.StartLine) && m.Line <= int(sym.EndLine) {
				analysis.HasDeferCleanup = true
				break
			}
		}
	}

	// Search for TODO/FIXME in comments near this symbol.
	todoResult, err := client.SearchScoped(ctx, oxcodes.ScopedSearchInput{
		Root: root, Pattern: "TODO|FIXME",
		Scope: "comments", Language: lang, IsRegex: true,
		MaxResults: 50, CaseSensitive: false,
	})
	if err == nil {
		for _, m := range todoResult.Matches {
			if m.File == sym.File && m.Line >= int(sym.StartLine)-5 && m.Line <= int(sym.EndLine)+5 {
				analysis.HasTODO = true
				break
			}
		}
	}

	return analysis
}

// detectLanguage returns the ox-codes language name for the given file path.
func detectLanguage(filePath string) string {
	switch {
	case strings.HasSuffix(filePath, ".go"):
		return "go"
	case strings.HasSuffix(filePath, ".rs"):
		return "rust"
	case strings.HasSuffix(filePath, ".py"):
		return "python"
	case strings.HasSuffix(filePath, ".ts"), strings.HasSuffix(filePath, ".tsx"),
		strings.HasSuffix(filePath, ".js"), strings.HasSuffix(filePath, ".jsx"):
		return "typescript"
	case strings.HasSuffix(filePath, ".java"):
		return "java"
	case strings.HasSuffix(filePath, ".rb"):
		return "ruby"
	case strings.HasSuffix(filePath, ".c"), strings.HasSuffix(filePath, ".h"):
		return "c"
	case strings.HasSuffix(filePath, ".cpp"), strings.HasSuffix(filePath, ".cc"):
		return "cpp"
	case strings.HasSuffix(filePath, ".cs"):
		return "csharp"
	case strings.HasSuffix(filePath, ".php"):
		return "php"
	default:
		return ""
	}
}
