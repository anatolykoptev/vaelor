package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DataflowInput is the input schema for the dataflow_analyze tool.
type DataflowInput struct {
	Repo        string `json:"repo" jsonschema_description:"Repository: GitHub slug, URL, or absolute local path"`
	Language    string `json:"language,omitempty" jsonschema_description:"Language to analyze (go, python). Auto-detected if omitted."`
	Focus       string `json:"focus,omitempty" jsonschema_description:"Analysis focus: 'all' (default), 'quality' (dead stores, unused vars), 'security' (taint/injection)"`
	FileGlob    string `json:"file_glob,omitempty" jsonschema_description:"Include only files matching glob"`
	ExcludeGlob string `json:"exclude_glob,omitempty" jsonschema_description:"Exclude files matching glob"`
}

// XML response types.

type xmlDataflowResponse struct {
	XMLName  xml.Name    `xml:"response"`
	Dataflow xmlDataflow `xml:"dataflow"`
}

type xmlDataflow struct {
	Repo          string          `xml:"repo,attr"`
	Language      string          `xml:"language,attr,omitempty"`
	Focus         string          `xml:"focus,attr"`
	FilesAnalyzed int             `xml:"filesAnalyzed,attr"`
	DurationMS    int64           `xml:"durationMs,attr"`
	Quality       *xmlDfQuality   `xml:"quality,omitempty"`
	DeadFunctions *xmlDfDeadFuncs `xml:"deadFunctions,omitempty"`
	Security      *xmlDfSecurity  `xml:"security,omitempty"`
}

type xmlDfQuality struct {
	Count    int                 `xml:"count,attr"`
	Findings []xmlQualityFinding `xml:"finding"`
}

type xmlQualityFinding struct {
	Kind     string `xml:"kind,attr"`
	Severity string `xml:"severity,attr"`
	File     string `xml:"file,attr"`
	Line     int    `xml:"line,attr"`
	Variable string `xml:"variable,attr,omitempty"`
	Message  string `xml:",chardata"`
}

type xmlDfSecurity struct {
	Count    int                  `xml:"count,attr"`
	Findings []xmlSecurityFinding `xml:"finding"`
}

type xmlSecurityFinding struct {
	RuleID   string `xml:"ruleId,attr"`
	Severity string `xml:"severity,attr"`
	File     string `xml:"file,attr"`
	Line     int    `xml:"line,attr,omitempty"`
	CWE      string `xml:"cwe,attr,omitempty"`
	Message  string `xml:",chardata"`
}

const dataflowMaxResults = 100

func registerDataflow(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "dataflow_analyze",
		Description: "Unified code quality and security analysis. " +
			"Quality: dead stores, unused variables (data-flow), dead functions (callgraph). " +
			"Security: SQL injection, command injection (taint tracking). " +
			"Combines variable-level (IL/CFG) and function-level (callgraph) dead code detection. " +
			"Use instead of separate dead_code + manual review. Requires ox-codes backend.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DataflowInput) (*mcp.CallToolResult, error) {
		return handleDataflow(ctx, input, deps, outputDir)
	})
}

func handleDataflow(ctx context.Context, input DataflowInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, error) {
	if deps.OxCodes == nil {
		return errResult("dataflow_analyze requires ox-codes backend (OX_CODES_URL not configured)"), nil
	}
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	focus := input.Focus
	if focus == "" {
		focus = "all"
	}
	if focus != "all" && focus != "quality" && focus != "security" {
		return errResult("focus must be 'all', 'quality', or 'security'"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	// Auto-detect language from repo when not specified.
	if input.Language == "" {
		input.Language = detectDominantLanguage(root)
		if input.Language != "" {
			slog.Debug("dataflow: auto-detected language", "lang", input.Language)
		}
	}

	resp := xmlDataflowResponse{
		Dataflow: xmlDataflow{
			Repo:     input.Repo,
			Language: input.Language,
			Focus:    focus,
		},
	}

	var totalDuration int64

	// Quality analysis (dead stores, unused vars).
	if focus == "all" || focus == "quality" {
		qResp, err := runQualityAnalysis(ctx, deps.OxCodes, root, input)
		if err != nil {
			slog.Warn("dataflow quality analysis failed", "err", err)
		} else {
			resp.Dataflow.Quality = qResp.xmlDfQuality
			resp.Dataflow.FilesAnalyzed = qResp.filesAnalyzed
			totalDuration += qResp.durationMS
		}
	}

	// Dead function analysis (callgraph-based).
	if focus == "all" || focus == "quality" {
		if dfResult := runDeadFunctionAnalysis(ctx, root, input.Language, deps); dfResult != nil {
			resp.Dataflow.DeadFunctions = dfResult.xmlDfDeadFuncs
			totalDuration += dfResult.durationMS
		}
	}

	// Security analysis (taint tracking).
	if focus == "all" || focus == "security" {
		sResp, err := runSecurityAnalysis(ctx, deps.OxCodes, root, input)
		if err != nil {
			slog.Warn("dataflow taint analysis failed", "err", err)
		} else {
			resp.Dataflow.Security = sResp.xmlDfSecurity
			if sResp.filesAnalyzed > resp.Dataflow.FilesAnalyzed {
				resp.Dataflow.FilesAnalyzed = sResp.filesAnalyzed
			}
			totalDuration += sResp.durationMS
		}
	}

	resp.Dataflow.DurationMS = totalDuration

	if resp.Dataflow.Quality == nil && resp.Dataflow.DeadFunctions == nil && resp.Dataflow.Security == nil {
		return errResult("dataflow analysis failed: no results from ox-codes"), nil
	}

	return xmlMarshalResult(resp, "dataflow_analyze", outputDir), nil
}

type qualityResult struct {
	*xmlDfQuality
	filesAnalyzed int
	durationMS    int64
}

func runQualityAnalysis(ctx context.Context, client *oxcodes.Client, root string, input DataflowInput) (*qualityResult, error) {
	result, err := client.DataflowAnalyze(ctx, oxcodes.DataflowInput{
		Root:        root,
		Language:    input.Language,
		MaxResults:  dataflowMaxResults,
		FileGlob:    input.FileGlob,
		ExcludeGlob: input.ExcludeGlob,
	})
	if err != nil {
		return nil, fmt.Errorf("quality: %w", err)
	}

	findings := make([]xmlQualityFinding, len(result.Findings))
	for i, f := range result.Findings {
		findings[i] = xmlQualityFinding{
			Kind:     f.Kind,
			Severity: f.Severity,
			File:     f.File,
			Line:     f.Span.StartLine,
			Variable: f.Variable,
			Message:  f.Message,
		}
	}

	return &qualityResult{
		xmlDfQuality:  &xmlDfQuality{Count: result.TotalFindings, Findings: findings},
		filesAnalyzed: result.FilesAnalyzed,
		durationMS:    result.DurationMS,
	}, nil
}

// detectDominantLanguage walks root and returns the most common programming language
// detected from file extensions. Returns "" if the root cannot be walked or no
// recognised source files are found.
func detectDominantLanguage(root string) string {
	counts := make(map[string]int)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if lang := ingest.DetectLanguage(path); lang != "" {
			counts[lang]++
		}
		return nil
	})
	best := ""
	bestCount := 0
	for lang, count := range counts {
		if count > bestCount {
			bestCount = count
			best = lang
		}
	}
	return best
}
