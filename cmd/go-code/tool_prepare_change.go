package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/compound"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PrepareChangeInput is the input schema for the prepare_change tool.
type PrepareChangeInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol   string `json:"symbol" jsonschema_description:"Function or method name to assess change risk for"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit scope"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit to files of this language (e.g. go, python)"`
	Depth    int    `json:"depth,omitempty" jsonschema_description:"Max impact traversal depth (default 5, max 10)"`
}

const maxPrepareChangeDepth = 10

// couplingMinCoChanges is the minimum number of shared git commits required
// to consider two files "coupled" — matches compare.defaultMinCoChanges so
// the signal is comparable across tools.
const couplingMinCoChanges = 3

func registerPrepareChange(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "prepare_change",
		Description: "Pre-change risk assessment for a function or method. Aggregates: impact_analysis (blast radius, affected callers) + dead_code (is it even used?). " +
			"Returns risk level, affected packages, and dead code status. " +
			"Use before refactoring to understand what might break. " +
			"Suggests similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareChangeInput) (*mcp.CallToolResult, error) {
		return handlePrepareChange(ctx, input, deps, sem)
	})
}

func handlePrepareChange(ctx context.Context, input PrepareChangeInput, deps analyze.Deps, sem *SemanticDeps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	depth := input.Depth
	if depth <= 0 || depth > maxPrepareChangeDepth {
		depth = maxPrepareChangeDepth
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

	result := compound.PrepareChange(ctx, cg, input.Symbol, compound.PrepareChangeOpts{
		MaxDepth: depth,
	})

	if !result.Found {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(fmt.Sprintf("<response tool=\"prepare_change\">\n"+
				"  <error>%s</error>\n%s\n</response>", escapeXML(msg), suggestions)), nil
		}
		return errResult(msg), nil
	}

	// Collect git-coupled files that frequently change together with the target.
	var relatedFiles []string
	targetFile := result.Symbol.File
	if rel, err := filepath.Rel(root, targetFile); err == nil {
		targetFile = rel
	}
	cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
	defer ccancel()
	pairs := compare.CollectCoupling(cctx, root, couplingMinCoChanges)
	seen := make(map[string]bool)
	for _, p := range pairs {
		var other string
		switch {
		case p.FileA == targetFile:
			other = p.FileB
		case p.FileB == targetFile:
			other = p.FileA
		}
		if other != "" && !seen[other] {
			seen[other] = true
			relatedFiles = append(relatedFiles, other)
		}
	}

	response := prepareChangeResponse{
		PrepareChangeResult: result,
		RelatedFiles:        relatedFiles,
	}
	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	return textResult(string(data)), nil
}

// prepareChangeResponse wraps PrepareChangeResult with additional git coupling data.
type prepareChangeResponse struct {
	*compound.PrepareChangeResult
	RelatedFiles []string `json:"related_files,omitempty"`
}
