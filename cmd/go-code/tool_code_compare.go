package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeCompareInput is the input schema for the code_compare tool.
type CodeCompareInput struct {
	// RepoA is the first repository (GitHub slug or local path).
	RepoA string `json:"repo_a" jsonschema_description:"First repository: GitHub slug (owner/repo) or absolute local path"`

	// RepoB is the second repository (GitHub slug or local path).
	RepoB string `json:"repo_b" jsonschema_description:"Second repository: GitHub slug (owner/repo) or absolute local path"`

	// Focus is what to compare: architecture, api, dependencies, patterns, quality.
	Focus string `json:"focus,omitempty" jsonschema_description:"What to compare: architecture | api | dependencies | patterns | quality (default: architecture)"`

	// Language filters comparison to files of a specific language.
	Language string `json:"language,omitempty" jsonschema_description:"Limit comparison to files of this language (e.g. go, python)"`
}

// registerCodeCompare registers the code_compare MCP tool.
// Compares two repositories structurally and semantically.
func registerCodeCompare(server *mcp.Server, _ Config) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "code_compare",
		Description: "Compare two code repositories structurally and semantically. " +
			"Ingests both repos, parses ASTs, extracts symbols and patterns, " +
			"and produces a diff-style analysis highlighting architectural differences, " +
			"API design choices, dependency strategies, and code quality metrics. " +
			"Useful for evaluating libraries, understanding forks, or porting code.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ CodeCompareInput) (*mcp.CallToolResult, any, error) {
		// Phase 3 feature — not yet implemented.
		return errResult("code_compare is not yet implemented — coming in Phase 3"), nil, nil
	})
}
