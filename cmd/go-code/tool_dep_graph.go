package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlDepGraphResponse struct {
	XMLName  xml.Name    `xml:"response"`
	DepGraph xmlDepGraph `xml:"depGraph"`
}

type xmlDepGraph struct {
	Format  string   `xml:"format,attr,omitempty"`
	Content xmlCDATA `xml:"content"`
}

// DepGraphInput is the input schema for the dep_graph tool.
type DepGraphInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	// Type selects what to graph: imports (file-level), packages, modules, or calls (function call graph).
	Type string `json:"type,omitempty" jsonschema_description:"Graph type: imports | packages | modules | calls (default: packages)"`

	// Format controls output: json (adjacency list), dot (Graphviz), mermaid, or summary.
	Format string `json:"format,omitempty" jsonschema_description:"Output format: json | dot | mermaid | summary (default: mermaid)"`

	// Focus limits the graph to a specific package or module.
	Focus string `json:"focus,omitempty" jsonschema_description:"Package or subdirectory to focus on (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`

	// MaxDepth limits graph traversal depth from focused node.
	MaxDepth int `json:"max_depth,omitempty" jsonschema_description:"Max traversal depth from focus node (default: 3, 0=unlimited)"`

	// IncludeStdlib includes Go standard library imports in the graph.
	IncludeStdlib bool `json:"include_stdlib,omitempty" jsonschema_description:"Include standard library imports in graph. Default false (stdlib excluded)."`

	// CrossLanguage includes cross-language API route connections between layers.
	// Cross-language dependencies are available via code_graph polyglot_overview and layer_deps templates.
	CrossLanguage bool `json:"cross_language,omitempty" jsonschema_description:"Include cross-language API route connections between layers"`
}

// registerDepGraph registers the dep_graph MCP tool.
// Builds and visualizes the dependency graph of a repository.
func registerDepGraph(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

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
			CrossLanguage: input.CrossLanguage,
		})
		if err != nil {
			return errResult(fmt.Sprintf("build dep graph: %s", err)), nil, nil
		}

		resp := xmlDepGraphResponse{
			DepGraph: xmlDepGraph{
				Format:  input.Format,
				Content: xmlCDATA{Inner: wrapCDATA(graph)},
			},
		}
		data, err := xml.MarshalIndent(resp, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
		}
		return largeTextResult(xml.Header+string(data), "dep_graph", outputDir), nil, nil
	})
}
