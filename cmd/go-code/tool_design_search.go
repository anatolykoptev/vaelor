package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/designmd"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DesignSearchInput struct {
	Query string `json:"query" jsonschema_description:"Natural language description of the UI you want (e.g. 'dark minimal SaaS dashboard with green accent', 'premium fintech with purple gradients')"`
	TopK  int    `json:"top_k,omitempty" jsonschema_description:"Number of results (default 5, max 20)"`
}

// DesignDeps holds dependencies for design_search tool.
type DesignDeps struct {
	Client *embeddings.Client
	Store  *designmd.Store
}

const (
	defaultDesignTopK = 5
	maxDesignTopK     = 20
)

func registerDesignSearch(server *mcp.Server, cfg Config, deps DesignDeps) {
	var metaIndex map[string]designmd.BrandMeta
	if cfg.DesignMDDir != "" {
		metaPath := cfg.DesignMDDir + "/index.json"
		if data, err := os.ReadFile(metaPath); err != nil {
			data, _ = os.ReadFile("/tmp/go-code-output/design-md-index.json")
			_ = json.Unmarshal(data, &metaIndex)
		} else {
			_ = json.Unmarshal(data, &metaIndex)
		}
	}

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "design_search",
		Description: "Find the best DESIGN.md for your UI by describing the look and feel. " +
			"Returns ranked brands with visual theme, colors, and copy command. " +
			"Uses semantic search over 66 design systems (Stripe, Linear, Tesla, etc.).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DesignSearchInput) (*mcp.CallToolResult, error) {
		return handleDesignSearch(ctx, input, deps, metaIndex, cfg.DesignMDDir, cfg.PathMappings)
	})
}

func handleDesignSearch(
	ctx context.Context, input DesignSearchInput, deps DesignDeps,
	metaIndex map[string]designmd.BrandMeta, designDir string, mappings []analyze.PathMapping,
) (*mcp.CallToolResult, error) {
	if input.Query == "" {
		return errResult("query is required"), nil
	}
	if deps.Client == nil || deps.Store == nil {
		return errResult("design_search requires DESIGN_EMBED_URL and DATABASE_URL"), nil
	}

	topK := input.TopK
	if topK <= 0 {
		topK = defaultDesignTopK
	}
	if topK > maxDesignTopK {
		topK = maxDesignTopK
	}

	vector, err := deps.Client.EmbedQuery(ctx, input.Query)
	if err != nil {
		return errResult(fmt.Sprintf("embed query: %s", err)), nil
	}

	results, err := deps.Store.Search(ctx, vector, topK*3)
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	if len(results) == 0 {
		return textResult(fmt.Sprintf(
			"<response tool=\"design_search\">\n  <query>%s</query>\n"+
				"  <status>not_indexed</status>\n"+
				"  <message>No design embeddings found. Run: go-code index-designs /path/to/design-md/</message>\n"+
				"</response>", escapeXML(input.Query))), nil
	}

	type brandHit struct {
		brand    string
		section  string
		distance float32
		excerpt  string
	}
	seen := make(map[string]bool)
	var hits []brandHit
	for _, r := range results {
		if seen[r.Brand] {
			continue
		}
		seen[r.Brand] = true

		excerpt := r.Section
		if designDir != "" {
			if content, err := os.ReadFile(designDir + "/" + r.FilePath); err == nil {
				for _, s := range designmd.SplitSections(string(content)) {
					if s.Title == r.Section {
						body := strings.SplitN(s.Body, "\n", 2)
						if len(body) > 1 {
							excerpt = strings.TrimSpace(body[1])
							if len(excerpt) > 200 {
								cut := strings.LastIndex(excerpt[:200], " ")
								if cut > 100 {
									excerpt = excerpt[:cut] + "..."
								} else {
									excerpt = excerpt[:200] + "..."
								}
							}
						}
						break
					}
				}
			}
		}

		hits = append(hits, brandHit{
			brand:    r.Brand,
			section:  r.Section,
			distance: r.Distance,
			excerpt:  excerpt,
		})
		if len(hits) >= topK {
			break
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"design_search\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(input.Query))
	fmt.Fprintf(&sb, "  <results count=\"%d\">\n", len(hits))
	for i, h := range hits {
		score := 1.0 - float64(h.distance)
		fmt.Fprintf(&sb, "    <result rank=\"%d\" score=\"%.2f\" brand=\"%s\">\n",
			i+1, score, escapeXML(h.brand))
		fmt.Fprintf(&sb, "      <file>%s/DESIGN.md</file>\n", escapeXML(h.brand))

		if meta, ok := metaIndex[h.brand]; ok {
			if meta.Vibe != "" {
				fmt.Fprintf(&sb, "      <vibe>%s</vibe>\n", escapeXML(meta.Vibe))
			}
			if len(meta.Colors) > 0 {
				fmt.Fprintf(&sb, "      <colors>%s</colors>\n", escapeXML(strings.Join(meta.Colors, ", ")))
			}
			if meta.BestFor != "" {
				fmt.Fprintf(&sb, "      <best_for>%s</best_for>\n", escapeXML(meta.BestFor))
			}
		}

		fmt.Fprintf(&sb, "      <matched_section>%s</matched_section>\n", escapeXML(h.section))
		fmt.Fprintf(&sb, "      <excerpt>%s</excerpt>\n", escapeXML(h.excerpt))
		fmt.Fprintf(&sb, "    </result>\n")
	}
	sb.WriteString("  </results>\n")
	if designDir != "" {
		hostDir := reversePathMapping(designDir, mappings)
		fmt.Fprintf(&sb, "  <usage>Copy: cp %s/BRAND/DESIGN.md ./</usage>\n", escapeXML(hostDir))
	}
	sb.WriteString("</response>")
	return textResult(sb.String()), nil
}

// reversePathMapping converts container-internal paths back to host paths.
// e.g. /host/tools/... → /path/to/repos/tools/... using PathMappings (External:/path/to/repos, Internal:/host).
func reversePathMapping(path string, mappings []analyze.PathMapping) string {
	for _, m := range mappings {
		if strings.HasPrefix(path, m.Internal) {
			return m.External + path[len(m.Internal):]
		}
	}
	return path
}
