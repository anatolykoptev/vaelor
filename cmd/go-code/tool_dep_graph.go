package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DepGraphInput is the input schema for the dep_graph tool.
type DepGraphInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo) or absolute local path"`

	// Type selects what to graph: imports (file-level), packages, modules, or calls (function call graph).
	Type string `json:"type,omitempty" jsonschema_description:"Graph type: imports | packages | modules | calls (default: packages)"`

	// Format controls output: json (adjacency list), dot (Graphviz), mermaid, or summary.
	Format string `json:"format,omitempty" jsonschema_description:"Output format: json | dot | mermaid | summary (default: mermaid)"`

	// Focus limits the graph to a specific package or module.
	Focus string `json:"focus,omitempty" jsonschema_description:"Focus on a specific package or module (e.g. internal/auth)"`

	// MaxDepth limits graph traversal depth from focused node.
	MaxDepth int `json:"max_depth,omitempty" jsonschema_description:"Max traversal depth from focus node (default: 3, 0=unlimited)"`

	// IncludeStdlib includes Go standard library imports in the graph.
	IncludeStdlib bool `json:"include_stdlib,omitempty" jsonschema_description:"Include standard library imports in graph. Default false (stdlib excluded)."`
}

// registerDepGraph registers the dep_graph MCP tool.
// Builds and visualizes the dependency graph of a repository.
func registerDepGraph(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "dep_graph",
		Description: "Build and visualize the dependency graph of a repository. " +
			"Parses import/require/use statements across all source files using tree-sitter, " +
			"then constructs a directed graph of package or module dependencies. " +
			"Supports output as Mermaid diagrams, Graphviz DOT, or JSON adjacency lists. " +
			"Can detect cycles, highly-connected nodes (hotspots), and layering violations.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DepGraphInput) (*mcp.CallToolResult, any, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
		}
		defer cleanup()

		graph, err := analyze.BuildDepGraph(ctx, analyze.DepGraphInput{
			Root:          root,
			Type:          input.Type,
			Format:        input.Format,
			Focus:         input.Focus,
			MaxDepth:      input.MaxDepth,
			IncludeStdlib: input.IncludeStdlib,
		})
		if err != nil {
			return errResult(fmt.Sprintf("build dep graph: %s", err)), nil, nil
		}

		return textResult(graph), nil, nil
	})
}
