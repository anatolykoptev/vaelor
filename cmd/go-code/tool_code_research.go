package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/research"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeResearchInput is the input schema for the code_research tool.
type CodeResearchInput struct {
	Repo        string `json:"repo" jsonschema_description:"GitHub repo (owner/repo) or local path"`
	Query       string `json:"query" jsonschema_description:"Natural language query describing what you're looking for (e.g. 'DAG parallel executor implementation', 'how retry logic works')"`
	Language    string `json:"language,omitempty" jsonschema_description:"Filter by language (e.g. go, python, typescript). Optional."`
	MaxTokens   int    `json:"max_tokens,omitempty" jsonschema_description:"Token budget for the output map (default 8000). Higher = more context, more tokens."`
	ExpandHops  int    `json:"expand_hops,omitempty" jsonschema_description:"Import-graph expansion hops from seed files (default 2). Higher = wider context."`
	IncludeBody bool   `json:"include_body,omitempty" jsonschema_description:"Include full function bodies in the output (default false). Significantly increases token usage."`
}

// registerCodeResearch registers the code_research MCP tool.
func registerCodeResearch(server *mcp.Server, _ Config, deps analyze.Deps, semDeps *SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_research",
		Description: "Deep code research for large repositories. " +
			"Combines keyword (BM25F), semantic (embeddings), and import-graph DAG expansion " +
			"to find relevant code and produce a compact, LLM-ready map. " +
			"Better than repo_analyze for targeted questions: 'how does X work?', " +
			"'find the implementation of Y', 'what calls Z'. " +
			"Returns seed symbols (direct matches) + linked files (import-graph neighbours) " +
			"+ Aider-style compact map within a token budget.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeResearchInput) (*mcp.CallToolResult, error) {
		return handleCodeResearch(ctx, input, deps, semDeps)
	})
}

func handleCodeResearch(
	ctx context.Context, input CodeResearchInput,
	analyzeDeps analyze.Deps, semDeps *SemanticDeps,
) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", analyzeDeps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	resDeps := research.Deps{
		AnalyzeDeps: analyzeDeps,
	}
	if semDeps != nil && semDeps.Client != nil && semDeps.Store != nil {
		resDeps.EmbedClient = semDeps.Client
		resDeps.EmbedStore = semDeps.Store
		resDeps.RepoKey = codegraph.GraphNameFor(root)
		// Trigger background re-index for freshness.
		if semDeps.Pipeline != nil {
			semDeps.Pipeline.IndexRepoAsync(resDeps.RepoKey, root)
		}
	}

	result, err := research.Run(ctx, research.Input{
		Root:        root,
		Query:       input.Query,
		Language:    input.Language,
		MaxTokens:   input.MaxTokens,
		ExpandHops:  input.ExpandHops,
		IncludeBody: input.IncludeBody,
	}, resDeps)
	if err != nil {
		return errResult(fmt.Sprintf("code_research: %s", err)), nil
	}

	return textResult(formatResearchResult(input, result)), nil
}

func formatResearchResult(input CodeResearchInput, r *research.Result) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "<response tool=\"code_research\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <repo>%s</repo>\n", escapeXML(input.Repo))
	fmt.Fprintf(&sb, "  <mode>%s</mode>\n", escapeXML(r.Mode))
	fmt.Fprintf(&sb, "  <stats seeds=\"%d\" graph_files=\"%d\" pruned=\"%d\" estimated_tokens=\"%d\"/>\n",
		len(r.Seeds), len(r.Graph), r.PrunedFiles, r.EstimatedTokens)

	// Seeds section.
	if len(r.Seeds) > 0 {
		fmt.Fprintf(&sb, "  <seeds>\n")
		seen := make(map[string]bool)
		for _, s := range r.Seeds {
			if seen[s.File] {
				continue
			}
			seen[s.File] = true
			fmt.Fprintf(&sb, "    <file path=%q score=\"%.4f\">\n", s.File, s.Score)
			// Emit distinct symbols for this file.
			for _, s2 := range r.Seeds {
				if s2.File == s.File && s2.Name != "" {
					fmt.Fprintf(&sb, "      <symbol kind=%q line=\"%d\">%s</symbol>\n",
						escapeXML(s2.Kind), s2.Line, escapeXML(s2.Name))
				}
			}
			fmt.Fprintf(&sb, "    </file>\n")
		}
		fmt.Fprintf(&sb, "  </seeds>\n")
	}

	// Graph section.
	if len(r.Graph) > 0 {
		fmt.Fprintf(&sb, "  <graph>\n")
		for _, lf := range r.Graph {
			fmt.Fprintf(&sb, "    <file path=%q distance=\"%d\" why=%q score=\"%.4f\">\n",
				lf.RelPath, lf.Distance, escapeXML(lf.WhyLinked), lf.Score)
			for _, sym := range lf.Symbols {
				fmt.Fprintf(&sb, "      <symbol kind=%q line=\"%d\">%s</symbol>\n",
					escapeXML(string(sym.Kind)), sym.StartLine, escapeXML(sym.Name))
			}
			fmt.Fprintf(&sb, "    </file>\n")
		}
		fmt.Fprintf(&sb, "  </graph>\n")
	}

	// Compact map — the primary LLM-consumable output.
	if r.Map != "" {
		fmt.Fprintf(&sb, "  <map>\n%s\n  </map>\n", r.Map)
	}

	sb.WriteString("</response>")
	return sb.String()
}

// embeddings.SearchOpts needs to be accessible — re-export via alias is not needed
// since we use it only through semDeps which is already typed.
var _ = embeddings.SearchOpts{}
