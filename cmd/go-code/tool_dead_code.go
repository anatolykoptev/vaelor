package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/deadcode"
	"github.com/anatolykoptev/go-code/internal/prompts"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlDeadCodeResponse struct {
	XMLName  xml.Name    `xml:"response"`
	DeadCode xmlDeadCode `xml:"deadCode"`
}

type xmlDeadCode struct {
	Total     int             `xml:"total,attr"`
	Dead      int             `xml:"dead,attr"`
	Ratio     float64         `xml:"ratio,attr"`
	Symbols   []xmlDeadSymbol `xml:"symbol"`
	Narrative xmlCDATA        `xml:"narrative,omitempty"`
}

type xmlDeadSymbol struct {
	Kind       string `xml:"kind,attr"`
	Name       string `xml:"name,attr"`
	File       string `xml:"file,attr"`
	Package    string `xml:"package,attr"`
	Line       int    `xml:"line,attr"`
	Lines      int    `xml:"lines,attr"`
	Exported   bool   `xml:"exported,attr,omitempty"`
	Confidence string `xml:"confidence,attr"`
}

// DeadCodeInput is the input schema for the dead_code tool.
type DeadCodeInput struct {
	Repo            string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language        string `json:"language,omitempty" jsonschema_description:"Limit analysis to files of this language (e.g. go, python)"`
	IncludeExported bool   `json:"include_exported,omitempty" jsonschema_description:"Include exported/public functions (usually false positives, default: false)"`
	Focus           string `json:"focus,omitempty" jsonschema_description:"Optional focus area for the LLM narrative"`
}

func registerDeadCode(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "dead_code",
		Description: "Detect functions and methods with zero incoming calls. " +
			"Filters out entry points (main, init), test functions, and exported symbols " +
			"to reduce false positives. Shows confidence levels: high (unexported), " +
			"medium (methods, may satisfy interfaces), low (exported).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeadCodeInput) (*mcp.CallToolResult, error) {
		return handleDeadCode(ctx, input, deps, outputDir)
	})
}

func handleDeadCode(ctx context.Context, input DeadCodeInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	result := deadcode.Analyze(cg, deadcode.Options{
		IncludeExported: input.IncludeExported,
		HookCallbacks:   cg.HookCallbacks,
	})

	// Convert dead symbols to XML structs.
	symbols := make([]xmlDeadSymbol, len(result.DeadSymbols))
	for i, s := range result.DeadSymbols {
		symbols[i] = xmlDeadSymbol{
			Kind:       s.Kind,
			Name:       s.Name,
			File:       s.File,
			Package:    s.Package,
			Line:       s.StartLine,
			Lines:      s.Lines,
			Exported:   s.Exported,
			Confidence: s.Confidence,
		}
	}

	resp := xmlDeadCodeResponse{
		DeadCode: xmlDeadCode{
			Total:   result.TotalFunctions,
			Dead:    result.DeadCount,
			Ratio:   result.DeadRatio,
			Symbols: symbols,
		},
	}

	// LLM narrative (optional, non-fatal).
	if result.DeadCount > 0 {
		prefix := "Repository dead code analysis:\n"
		if input.Focus != "" {
			prefix = fmt.Sprintf("Focus area: %s\n\n%s", input.Focus, prefix)
		}
		if n := generateNarrative(ctx, deps.LLM, prompts.SystemPromptDeadCode, result, prefix); n != "" {
			resp.DeadCode.Narrative = xmlCDATA{Inner: wrapCDATA(n)}
		}
	}

	return xmlMarshalResult(resp, "dead_code", outputDir), nil
}
