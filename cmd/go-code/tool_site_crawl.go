package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/webanalyze"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SiteCrawlInput is the input schema for the site_crawl tool.
type SiteCrawlInput struct {
	URL             string `json:"url" jsonschema_description:"Seed URL to start crawling from (e.g. https://example.com)"`
	MaxDepth        int    `json:"max_depth,omitempty" jsonschema_description:"Maximum crawl depth (default: 3)"`
	MaxPages        int    `json:"max_pages,omitempty" jsonschema_description:"Maximum number of pages to crawl (default: 50)"`
	Scope           string `json:"scope,omitempty" jsonschema_description:"URL scope: same_domain (default) or same_host"`
	IncludeMarkdown *bool  `json:"include_markdown,omitempty" jsonschema_description:"Include markdown content for each page (default: true)"`
}

const (
	defaultCrawlMaxDepth = 3
	defaultCrawlMaxPages = 50
)

func registerSiteCrawl(server *mcp.Server, cfg Config) {
	if cfg.OxBrowserURL == "" {
		return
	}
	client := webanalyze.NewClient(cfg.OxBrowserURL)

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "site_crawl",
		Description: "BFS site crawler via ox-browser. Starts from a seed URL, " +
			"discovers pages up to max_depth, respects robots.txt, deduplicates URLs. " +
			"Returns all crawled pages with titles, markdown content, and link counts. " +
			"Use for site-wide content extraction, documentation crawling, or site mapping.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SiteCrawlInput) (*mcp.CallToolResult, error) {
		return handleSiteCrawl(ctx, input, client, cfg.OutputDir)
	})
}

func handleSiteCrawl(
	ctx context.Context, input SiteCrawlInput,
	client *webanalyze.Client, outputDir string,
) (*mcp.CallToolResult, error) {
	if input.URL == "" {
		return errResult("url is required"), nil
	}

	maxDepth := input.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultCrawlMaxDepth
	}
	maxPages := input.MaxPages
	if maxPages <= 0 {
		maxPages = defaultCrawlMaxPages
	}

	includeMarkdown := input.IncludeMarkdown == nil || *input.IncludeMarkdown

	resp, err := client.Crawl(ctx, webanalyze.CrawlInput{
		URL:             input.URL,
		MaxDepth:        maxDepth,
		MaxPages:        maxPages,
		Scope:           input.Scope,
		IncludeMarkdown: includeMarkdown,
	})
	if err != nil {
		return errResult(fmt.Sprintf("crawl: %s", err)), nil
	}

	return largeTextResult(formatCrawlResponse(resp), "site_crawl", outputDir), nil
}

func formatCrawlResponse(resp *webanalyze.CrawlResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"site_crawl\">\n")
	fmt.Fprintf(&sb, "  <summary pages=\"%d\" errors=\"%d\" elapsed_ms=\"%d\"/>\n",
		resp.Summary.PagesCrawled, resp.Summary.Errors, resp.Summary.ElapsedMs)

	for _, p := range resp.Pages {
		if p.Error != nil {
			fmt.Fprintf(&sb, "  <page url=%q depth=\"%d\" error=%q/>\n",
				p.URL, p.Depth, *p.Error)
			continue
		}
		fmt.Fprintf(&sb, "  <page url=%q status=\"%d\" depth=\"%d\" title=%q links=\"%d\" bytes=\"%d\">\n",
			p.URL, p.Status, p.Depth, escapeXML(p.Title), p.LinksFound, p.ContentLength)
		if p.Markdown != "" {
			sb.WriteString("    <content>")
			sb.WriteString(wrapCDATA(p.Markdown))
			sb.WriteString("</content>\n")
		}
		sb.WriteString("  </page>\n")
	}

	sb.WriteString("</response>")
	return sb.String()
}
