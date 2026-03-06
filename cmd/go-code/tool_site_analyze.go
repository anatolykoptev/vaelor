package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/webanalyze"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SiteAnalyzeInput is the input schema for the site_analyze tool.
type SiteAnalyzeInput struct {
	URL  string `json:"url" jsonschema_description:"Website URL to analyze (e.g. https://example.com)"`
	Mode string `json:"mode,omitempty" jsonschema_description:"detect (tech stack only, default) or full (detect + download source maps)"`
}

func registerSiteAnalyze(server *mcp.Server, cfg Config) {
	if cfg.OxBrowserURL == "" {
		return
	}
	client := webanalyze.NewClient(cfg.OxBrowserURL)
	workDir := cfg.WorkspaceDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "site_analyze",
		Description: "Analyze a website's technology stack and frontend code. " +
			"Detects CMS, JS frameworks, CSS frameworks, analytics, CDN, and server software. " +
			"In full mode, downloads JS bundles and extracts source maps for analysis " +
			"with explore, symbol_search, or dep_graph.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SiteAnalyzeInput) (*mcp.CallToolResult, error) {
		return handleSiteAnalyze(ctx, input, client, workDir)
	})
}

func handleSiteAnalyze(
	ctx context.Context, input SiteAnalyzeInput,
	client *webanalyze.Client, workDir string,
) (*mcp.CallToolResult, error) {
	if input.URL == "" {
		return errResult("url is required"), nil
	}

	resp, err := client.Analyze(ctx, input.URL)
	if err != nil {
		return errResult(fmt.Sprintf("analyze: %s", err)), nil
	}
	if resp.Error != "" {
		return errResult(fmt.Sprintf("ox-browser: %s", resp.Error)), nil
	}

	mode := input.Mode
	if mode == "" {
		mode = "detect"
	}

	if mode == "detect" {
		return textResult(formatDetectResponse(resp)), nil
	}

	// Full mode: download assets and extract source maps.
	domain := extractDomain(input.URL)
	outDir := filepath.Join(workDir, "sites", domain)

	stats, extractErr := extractSources(ctx, client, resp, outDir)
	return textResult(formatFullResponse(resp, outDir, stats, extractErr)), nil
}

func extractSources(
	ctx context.Context, client *webanalyze.Client,
	resp *webanalyze.AnalyzeResponse, outDir string,
) (*webanalyze.SourceStats, error) {
	totalStats := &webanalyze.SourceStats{Languages: make(map[string]int)}

	for _, scriptURL := range resp.Assets.Scripts {
		absURL := resolveURL(resp.URL, scriptURL)
		fetchResp, err := client.Fetch(ctx, absURL)
		if err != nil || fetchResp.Status != 200 {
			continue
		}

		mapURL := webanalyze.FindSourceMapURL(fetchResp.Body)
		if mapURL == "" {
			continue
		}

		absMapURL := resolveURL(absURL, mapURL)
		mapResp, err := client.Fetch(ctx, absMapURL)
		if err != nil || mapResp.Status != 200 {
			continue
		}

		sm, err := webanalyze.ParseSourceMap([]byte(mapResp.Body))
		if err != nil {
			continue
		}

		stats, err := webanalyze.WriteSourceTree(outDir, sm)
		if err != nil {
			continue
		}
		totalStats.Files += stats.Files
		for ext, count := range stats.Languages {
			totalStats.Languages[ext] += count
		}
	}
	return totalStats, nil
}

func formatDetectResponse(resp *webanalyze.AnalyzeResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"site_analyze\">\n")
	fmt.Fprintf(&sb, "  <site url=%q status=\"%d\">\n", resp.URL, resp.Status)
	formatTechnologies(&sb, resp.Technologies)
	fmt.Fprintf(&sb, "    <meta generator=%q server=%q title=%q/>\n",
		resp.Meta.Generator, resp.Meta.Server, resp.Meta.Title)
	fmt.Fprintf(&sb, "    <assets scripts=\"%d\" stylesheets=\"%d\"/>\n",
		len(resp.Assets.Scripts), len(resp.Assets.Stylesheets))
	sb.WriteString("  </site>\n</response>")
	return sb.String()
}

func formatFullResponse(
	resp *webanalyze.AnalyzeResponse, outDir string,
	stats *webanalyze.SourceStats, extractErr error,
) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"site_analyze\">\n")
	fmt.Fprintf(&sb, "  <site url=%q status=\"%d\">\n", resp.URL, resp.Status)
	formatTechnologies(&sb, resp.Technologies)
	if stats != nil && stats.Files > 0 {
		fmt.Fprintf(&sb, "    <sources path=%q files=\"%d\">\n", outDir, stats.Files)
		for ext, count := range stats.Languages {
			fmt.Fprintf(&sb, "      <language name=%q files=\"%d\"/>\n", ext, count)
		}
		sb.WriteString("    </sources>\n")
		fmt.Fprintf(&sb, "    <hint>Use explore, symbol_search, or dep_graph with repo=%q</hint>\n", outDir)
	} else {
		msg := "No source maps found"
		if extractErr != nil {
			msg = extractErr.Error()
		}
		fmt.Fprintf(&sb, "    <sources files=\"0\" reason=%q/>\n", msg)
	}
	sb.WriteString("  </site>\n</response>")
	return sb.String()
}

func formatTechnologies(sb *strings.Builder, techs []webanalyze.Technology) {
	fmt.Fprintf(sb, "    <technologies count=\"%d\">\n", len(techs))
	for _, t := range techs {
		fmt.Fprintf(sb, "      <tech category=%q name=%q confidence=\"%d\"/>\n",
			t.Category, t.Name, t.Confidence)
	}
	sb.WriteString("    </technologies>\n")
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	return u.Hostname()
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	u, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return u.ResolveReference(r).String()
}
