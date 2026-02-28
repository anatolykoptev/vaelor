package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RepoAnalyzeInput is the input schema for the repo_analyze tool.
type RepoAnalyzeInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo) or absolute local path"`

	// Query is a natural-language question about the repository.
	Query string `json:"query" jsonschema_description:"What you want to understand about the repository"`

	// Ref is the branch, tag, or commit SHA to analyze (default: default branch).
	Ref string `json:"ref,omitempty" jsonschema_description:"Branch, tag, or commit SHA (default: HEAD)"`

	// Focus narrows analysis to a subdirectory or file glob pattern.
	Focus string `json:"focus,omitempty" jsonschema_description:"Subdirectory or glob pattern to focus on (e.g. internal/auth or **/*.go)"`
}

// registerRepoAnalyze registers the repo_analyze MCP tool.
// Analyzes a repository: clones (if remote), walks the file tree, parses ASTs,
// and produces a structured LLM-powered answer about the codebase.
func registerRepoAnalyze(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "repo_analyze",
		Description: "Analyze a code repository (GitHub or local). " +
			"Clones the repo if remote, walks the file tree, parses ASTs with tree-sitter, " +
			"and answers a natural-language question about the codebase structure, " +
			"architecture, or implementation details.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RepoAnalyzeInput) (*mcp.CallToolResult, any, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil, nil
		}
		if input.Query == "" {
			return errResult("query is required"), nil, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, input.Ref, deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
		}
		defer cleanup()

		result, err := analyze.AnalyzeRepo(ctx, analyze.RepoAnalysisInput{
			Root:  root,
			Query: input.Query,
			Focus: input.Focus,
		}, deps)
		if err != nil {
			return errResult(fmt.Sprintf("analyze: %s", err)), nil, nil
		}

		return textResult(formatAnalysisResult(result)), nil, nil
	})
}

// formatAnalysisResult formats a RepoAnalysisResult as human-readable text.
func formatAnalysisResult(r *analyze.RepoAnalysisResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# Repository: %s\n", r.RepoName)
	fmt.Fprintf(&sb, "Language: %s | Files analyzed: %d\n\n", r.Language, r.FileCount)

	if len(r.Packages) > 0 {
		fmt.Fprintf(&sb, "## Packages (%d)\n", len(r.Packages))
		for _, pkg := range r.Packages {
			fmt.Fprintf(&sb, "  - %s\n", pkg)
		}
		sb.WriteString("\n")
	}

	if len(r.Symbols) > 0 {
		fmt.Fprintf(&sb, "## Key Symbols (%d)\n", len(r.Symbols))
		for _, sym := range r.Symbols {
			writeSymbolLine(&sb, sym)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "## Analysis\n%s\n", r.Answer)

	return sb.String()
}

// writeSymbolLine writes a single symbol summary line into sb.
func writeSymbolLine(sb *strings.Builder, sym *parser.Symbol) {
	if sym.Signature != "" {
		fmt.Fprintf(sb, "  [%s] %s — %s (line %d)\n", sym.Kind, sym.Name, sym.Signature, sym.StartLine)
	} else {
		fmt.Fprintf(sb, "  [%s] %s (line %d)\n", sym.Kind, sym.Name, sym.StartLine)
	}
}


// errResult returns a CallToolResult representing a tool-level error.
func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// textResult returns a CallToolResult with text content.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
