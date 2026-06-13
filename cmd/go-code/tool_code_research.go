package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-code/internal/research"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeResearchInput is the input schema for the code_research tool.
type CodeResearchInput struct {
	Repo             string `json:"repo" jsonschema_description:"GitHub repo (owner/repo) or local path"`
	Query            string `json:"query" jsonschema_description:"Natural language query describing what you're looking for (e.g. 'DAG parallel executor implementation', 'how retry logic works')"`
	Language         string `json:"language,omitempty" jsonschema_description:"Filter by language (e.g. go, python, typescript). Optional."`
	MaxTokens        int    `json:"max_tokens,omitempty" jsonschema_description:"Token budget for the output map (default 8000). Higher = more context, more tokens."`
	ExpandHops       int    `json:"expand_hops,omitempty" jsonschema_description:"Import-graph expansion hops from seed files (default 2). Higher = wider context."`
	IncludeBody      bool   `json:"include_body,omitempty" jsonschema_description:"Include full function bodies in the output (default false). Significantly increases token usage."`
	FileGlob         string `json:"file_glob,omitempty" jsonschema_description:"Restrict analysis to files matching this glob (e.g. 'internal/**', 'pkg/foo/*.go'). Optional."`
	IncludeTests     bool   `json:"include_tests,omitempty" jsonschema_description:"Include *_test.go / test files in retrieval (default false). Useful for 'how is X tested' queries."`
	IncludeCallGraph bool   `json:"include_call_graph,omitempty" jsonschema_description:"Expand retrieval via call-graph edges (callers + callees) in addition to imports. Slower but higher precision for 'what calls X' queries."`
	Compact          bool   `json:"compact,omitempty" jsonschema_description:"If true, return only the stats header and rendered map (skip <seeds>/<graph>). ~20% token savings."`
}

// registerCodeResearch registers the code_research MCP tool.
func registerCodeResearch(server *mcp.Server, _ Config, deps analyze.Deps, semDeps *SemanticDeps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_research",
		Description: "Deep code research for large repositories. " +
			"Combines keyword (BM25F with doc-comment indexing), semantic (embeddings), " +
			"import-graph DAG expansion, and optional call-graph BFS (callers+callees) " +
			"to find relevant code and produce a compact, LLM-ready map. " +
			"Features: file_glob scoping, include_tests/include_call_graph opt-ins, " +
			"compact output mode. " +
			"Better than repo_analyze for targeted questions: 'how does X work?', " +
			"'find the implementation of Y', 'what calls Z'. " +
			"Returns seed symbols (direct matches) + linked files (import/call-graph neighbours) " +
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
		BuildCallGraph: func(ctx context.Context, root string) (*callgraph.CallGraph, error) {
			cgCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			return callgraph.BuildFromRepo(cgCtx, callgraph.TraceRepoInput{Root: root})
		},
	}
	if semDeps != nil && semDeps.Client != nil && semDeps.QueryClient != nil && semDeps.Store != nil {
		// Use QueryClient (not Client) so model-specific prefixes are applied
		// on the query path. The research package's EmbedClient interface only
		// calls EmbedQuery, never Embed, so QueryEmbedder satisfies it.
		resDeps.EmbedClient = semDeps.QueryClient
		resDeps.EmbedStore = semDeps.Store
		resDeps.RepoKey = codegraph.GraphNameFor(root)
		// Wire pg_trgm symbol search for Step 3.5 augmentation.
		resDeps.SymbolSearcher = semDeps.Store
		// Trigger background re-index for freshness.
		if semDeps.Pipeline != nil {
			semDeps.Pipeline.IndexRepoAsyncWithTool("code_research", resDeps.RepoKey, root)
		}
	}
	if analyzeDeps.Graph != nil {
		resDeps.Graph = analyzeDeps.Graph
		resDeps.GraphRepoKey = root
	}

	result, err := research.Run(ctx, research.Input{
		Root:             root,
		Query:            input.Query,
		Language:         input.Language,
		MaxTokens:        input.MaxTokens,
		ExpandHops:       input.ExpandHops,
		IncludeBody:      input.IncludeBody,
		FileGlob:         input.FileGlob,
		IncludeTests:     input.IncludeTests,
		IncludeCallGraph: input.IncludeCallGraph,
	}, resDeps)
	if err != nil {
		return errResult(fmt.Sprintf("code_research: %s", err)), nil
	}

	return textResult(formatResearchResult(input, root, result)), nil
}

const (
	// maxSeedsOutput caps the number of seed files in the XML output.
	maxSeedsOutput = 30
	// maxGraphOutput caps the number of graph files in the XML output.
	maxGraphOutput = 50
)

func formatResearchResult(input CodeResearchInput, root string, r *research.Result) string {
	var sb strings.Builder

	// Strip workspace prefix from paths for cleaner output.
	stripRoot := root + "/"

	fmt.Fprintf(&sb, "<response tool=\"code_research\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <repo>%s</repo>\n", escapeXML(input.Repo))
	fmt.Fprintf(&sb, "  <mode>%s</mode>\n", escapeXML(r.Mode))
	fmt.Fprintf(&sb, "  <stats seeds=\"%d\" graph_files=\"%d\" pruned=\"%d\" estimated_tokens=\"%d\"/>\n",
		len(r.Seeds), len(r.Graph), r.PrunedFiles, r.EstimatedTokens)

	if !input.Compact {
		// Seeds section — top N by score.
		seeds := sortedSeeds(r.Seeds, maxSeedsOutput)
		if len(seeds) > 0 {
			fmt.Fprintf(&sb, "  <seeds>\n")
			seen := make(map[string]bool)
			for _, s := range seeds {
				relFile := strings.TrimPrefix(s.File, stripRoot)
				if seen[relFile] {
					continue
				}
				seen[relFile] = true
				fmt.Fprintf(&sb, "    <file path=%q score=\"%.4f\">\n", relFile, s.Score)
				for _, s2 := range seeds {
					if strings.TrimPrefix(s2.File, stripRoot) == relFile && s2.Name != "" {
						fmt.Fprintf(&sb, "      <symbol kind=%q line=\"%d\" source=%q>%s</symbol>\n",
							escapeXML(s2.Kind), s2.Line, escapeXML(s2.Source), escapeXML(s2.Name))
					}
				}
				fmt.Fprintf(&sb, "    </file>\n")
			}
			fmt.Fprintf(&sb, "  </seeds>\n")
		}

		// Graph section — top N by score, skip files with no symbols.
		graph := sortedGraph(r.Graph, maxGraphOutput)
		if len(graph) > 0 {
			fmt.Fprintf(&sb, "  <graph>\n")
			for _, lf := range graph {
				if len(lf.Symbols) == 0 {
					continue
				}
				relPath := strings.TrimPrefix(lf.RelPath, stripRoot)
				fmt.Fprintf(&sb, "    <file path=%q distance=\"%d\" why=%q score=\"%.4f\">\n",
					relPath, lf.Distance, escapeXML(lf.WhyLinked), lf.Score)
				for _, sym := range lf.Symbols {
					fmt.Fprintf(&sb, "      <symbol kind=%q line=\"%d\">%s</symbol>\n",
						escapeXML(string(sym.Kind)), sym.StartLine, escapeXML(sym.Name))
				}
				fmt.Fprintf(&sb, "    </file>\n")
			}
			fmt.Fprintf(&sb, "  </graph>\n")
		}
	}

	// Compact map — the primary LLM-consumable output.
	if r.Map != "" {
		fmt.Fprintf(&sb, "  <map>\n%s\n  </map>\n", r.Map)
	}

	sb.WriteString("</response>")
	return sb.String()
}

func sortedSeeds(seeds []research.SeedSymbol, limit int) []research.SeedSymbol {
	if len(seeds) <= limit {
		out := make([]research.SeedSymbol, len(seeds))
		copy(out, seeds)
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		return out
	}
	out := make([]research.SeedSymbol, len(seeds))
	copy(out, seeds)
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:limit]
}

func sortedGraph(graph []research.LinkedFile, limit int) []research.LinkedFile {
	if len(graph) <= limit {
		return graph
	}
	out := make([]research.LinkedFile, len(graph))
	copy(out, graph)
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:limit]
}

// embeddings.SearchOpts needs to be accessible — re-export via alias is not needed
// since we use it only through semDeps which is already typed.
var _ = embeddings.SearchOpts{}
