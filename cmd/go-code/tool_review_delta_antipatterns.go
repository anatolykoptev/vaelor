package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

// xmlAntiPatternSignals holds structural anti-pattern findings from ast-grep.
type xmlAntiPatternSignals struct {
	Count    int                  `xml:"count,attr"`
	Findings []xmlAntiPatternFind `xml:"finding,omitempty"`
}

type xmlAntiPatternFind struct {
	Rule    string `xml:"rule,attr"`
	File    string `xml:"file,attr"`
	Line    int    `xml:"line,attr"`
	Pattern string `xml:"pattern,attr,omitempty"`
	Message string `xml:",chardata"`
}

// antiPatternProbeTimeout bounds the structural anti-pattern scan so a
// cold ast-grep compile cannot stall a delta review.
const antiPatternProbeTimeout = 15 * time.Second

// defaultAntiPatterns are language-aware structural anti-pattern rules
// that ast-grep can match. These are common code-quality issues that
// are easy to introduce in a PR and easy to catch with structural search.
var defaultAntiPatterns = map[string][]struct {
	Rule    string
	Pattern string
	Message string
}{
	"go": {
		{"panic-in-prod", "panic($$$)", "panic() in production code — use error return instead"},
		{"fmt-println", "fmt.Println($$$)", "fmt.Println in production code — use slog or structured logging"},
		{"fmt-printf", "fmt.Printf($$$)", "fmt.Printf in production code — use slog or structured logging"},
		{"bare-error", "if $ERR != nil { return $ERR }", "bare error return — consider wrapping with fmt.Errorf(\"%w\", err) for context"},
		{"log-fatal", "log.Fatal($$$)", "log.Fatal in library code — use error return instead"},
	},
	"python": {
		{"print-stmt", "print($$$)", "print() in production code — use logging module instead"},
		{"bare-except", "except: $$$", "bare except: — catch specific exceptions instead"},
		{"assert-in-prod", "assert $$$", "assert in production code — raises AssertionError with -O flag"},
	},
	"typescript": {
		{"console-log", "console.log($$$)", "console.log in production code — use structured logger instead"},
		{"any-type", ": any", "explicit 'any' type — use specific types for type safety"},
	},
	"javascript": {
		{"console-log", "console.log($$$)", "console.log in production code — use structured logger instead"},
	},
	"rust": {
		{"unwrap-in-prod", "$EXPR.unwrap()", "unwrap() in production code — use expect() with context or ? operator"},
		{"todo-macro", "todo!($$$)", "todo!() in production code — implement or use unimplemented!()"},
	},
}

// collectAntiPatterns runs structural anti-pattern search via ox-codes
// ast-grep. Returns nil when ox-codes is unavailable or no patterns match.
func collectAntiPatterns(ctx context.Context, root, language string, oxCodes *oxcodes.Client) *xmlAntiPatternSignals {
	if oxCodes == nil || language == "" {
		return nil
	}

	patterns, ok := defaultAntiPatterns[language]
	if !ok {
		return nil
	}

	apCtx, cancel := context.WithTimeout(ctx, antiPatternProbeTimeout)
	defer cancel()

	var findings []xmlAntiPatternFind
	for _, p := range patterns {
		result, err := oxCodes.SearchStructural(apCtx, oxcodes.StructuralSearchInput{
			Root:       root,
			Pattern:    p.Pattern,
			Language:   language,
			MaxResults: 20, // cap per rule — we want signal, not exhaustive scan
		})
		if err != nil {
			slog.Debug("review_delta: anti-pattern search failed",
				slog.String("rule", p.Rule),
				slog.Any("error", err))
			continue
		}
		for _, m := range result.Matches {
			findings = append(findings, xmlAntiPatternFind{
				Rule:    p.Rule,
				File:    m.File,
				Line:    m.Line,
				Pattern: p.Pattern,
				Message: p.Message,
			})
		}
	}

	if len(findings) == 0 {
		return nil
	}
	return &xmlAntiPatternSignals{
		Count:    len(findings),
		Findings: findings,
	}
}

// formatAntiPatternsXML converts anti-pattern findings to XML string.
func formatAntiPatternsXML(ap *xmlAntiPatternSignals) string {
	if ap == nil {
		return ""
	}
	out := fmt.Sprintf("<antiPatterns count=\"%d\">", ap.Count)
	for _, f := range ap.Findings {
		out += fmt.Sprintf("<finding rule=\"%s\" file=\"%s\" line=\"%d\">%s</finding>",
			f.Rule, f.File, f.Line, f.Message)
	}
	out += "</antiPatterns>"
	return out
}
