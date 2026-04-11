package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/review"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReviewDeltaInput struct {
	Repo            string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full URL, or absolute local host path"`
	Base            string `json:"base,omitempty" jsonschema_description:"Base ref to diff against (commit SHA, branch, tag, HEAD~N). Default: HEAD~1"`
	Depth           int    `json:"depth,omitempty" jsonschema_description:"Impact traversal depth (default 2, max 5)"`
	Language        string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
	ExcludeSnippets bool   `json:"exclude_snippets,omitempty" jsonschema_description:"Set true to omit source code snippets (included by default)"`
}

const (
	defaultReviewDepth   = 2
	maxReviewDepth       = 5
	maxReviewOutputChars = 40_000 // ~10K tokens; drop snippets then impacted if exceeded
	maxReviewImpacted    = 50     // max impacted symbols after truncation
)

func registerReviewDelta(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "review_delta",
		Description: "Analyze changes between two git refs and compute differential impact. " +
			"Returns changed files, changed symbols, impacted downstream symbols, " +
			"untested changes, and risk guidance. " +
			"Ideal for pre-merge review: shows blast radius of a branch's changes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewDeltaInput) (*mcp.CallToolResult, error) {
		return handleReviewDelta(ctx, input, deps)
	})
}

type xmlDeltaResponse struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Tier    string   `xml:"tier,attr,omitempty"`

	ChangedFiles    []xmlChangedFile   `xml:"changed_files>file"`
	ChangedSymbols  []xmlChangedSymbol `xml:"changed_symbols>symbol"`
	ImpactedSymbols []xmlImpacted      `xml:"impacted_symbols>symbol"`
	Untested        []string           `xml:"untested>symbol,omitempty"`
	Snippets        []xmlSnippet       `xml:"snippets>snippet,omitempty"`
	Risk            xmlRisk            `xml:"risk"`
	Verdict         *xmlVerdict        `xml:"verdict,omitempty"`
}

type xmlChangedFile struct {
	Path    string `xml:"path,attr"`
	Added   int    `xml:"added,attr"`
	Removed int    `xml:"removed,attr"`
}

type xmlChangedSymbol struct {
	Name       string `xml:"name,attr"`
	Kind       string `xml:"kind,attr"`
	File       string `xml:"file,attr"`
	ChangeType string `xml:"change,attr"`
}

type xmlImpacted struct {
	Name       string  `xml:"name,attr"`
	File       string  `xml:"file,attr"`
	Distance   int     `xml:"distance,attr"`
	ChangedBy  string  `xml:"changed_by,attr"`
	Confidence float64 `xml:"confidence,attr"`
}

type xmlRisk struct {
	Level       string   `xml:"level,attr"`
	Score       float64  `xml:"score,attr"`
	Flags       []string `xml:"flag,omitempty"`
	Suggestions []string `xml:"suggestion,omitempty"`
}

type xmlSnippet struct {
	File   string   `xml:"file,attr"`
	Symbol string   `xml:"symbol,attr"`
	Start  int      `xml:"start,attr"`
	End    int      `xml:"end,attr"`
	Code   xmlCDATA `xml:"code"`
}

func handleReviewDelta(ctx context.Context, input ReviewDeltaInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	depth := input.Depth
	if depth <= 0 {
		depth = defaultReviewDepth
	}
	if depth > maxReviewDepth {
		depth = maxReviewDepth
	}

	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:            root,
		Base:            input.Base,
		Depth:           depth,
		Language:        input.Language,
		IncludeSnippets: !input.ExcludeSnippets,
		OxCodes:         deps.OxCodes,
	})
	if err != nil {
		return errResult(fmt.Sprintf("delta review: %s", err)), nil
	}

	resp := buildDeltaXML(result)
	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	out := string(data)
	// Token-aware truncation: if output exceeds limit, drop snippets first,
	// then truncate impacted symbols to keep risk guidance visible.
	if len(out) > maxReviewOutputChars {
		resp.Snippets = nil
		data, _ = xml.MarshalIndent(resp, "", "  ")
		out = string(data)
	}
	if len(out) > maxReviewOutputChars && len(resp.ImpactedSymbols) > maxReviewImpacted {
		resp.ImpactedSymbols = resp.ImpactedSymbols[:maxReviewImpacted]
		data, _ = xml.MarshalIndent(resp, "", "  ")
		out = string(data)
	}

	return textResult(out), nil
}

func buildDeltaXML(r *review.DeltaResult) xmlDeltaResponse {
	resp := xmlDeltaResponse{
		Tool: "review_delta",
		Tier: r.Tier,
	}

	for _, f := range r.ChangedFiles {
		resp.ChangedFiles = append(resp.ChangedFiles, xmlChangedFile{
			Path: f.Path, Added: f.Added, Removed: f.Removed,
		})
	}
	for _, cs := range r.ChangedSymbols {
		resp.ChangedSymbols = append(resp.ChangedSymbols, xmlChangedSymbol{
			Name: cs.Symbol.Name, Kind: string(cs.Symbol.Kind),
			File: cs.FileDiff.Path, ChangeType: string(cs.ChangeType),
		})
	}
	for _, is := range r.ImpactedSymbols {
		resp.ImpactedSymbols = append(resp.ImpactedSymbols, xmlImpacted{
			Name: is.Name, File: is.File, Distance: is.Distance,
			ChangedBy: is.ChangedBy, Confidence: is.Confidence,
		})
	}
	for _, s := range r.Snippets {
		resp.Snippets = append(resp.Snippets, xmlSnippet{
			File: s.File, Symbol: s.Symbol,
			Start: s.StartLine, End: s.EndLine,
			Code: xmlCDATA{Inner: wrapCDATA(s.Code)},
		})
	}
	resp.Untested = r.UntestedSymbols
	resp.Risk = xmlRisk{
		Level: r.Risk.RiskLevel, Score: r.Risk.RiskScore,
		Flags: r.Risk.Flags, Suggestions: r.Risk.Suggestions,
	}

	return resp
}
