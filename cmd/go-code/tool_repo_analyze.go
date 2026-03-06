package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/github"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/prompts"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Mode and format constants for repo_analyze.
const (
	modeQuick  = "quick"
	modeRaw    = "raw"
	formatJSON = "json"
	formatText = "text"
	formatXML  = "xml"
)

// RepoAnalyzeInput is the input schema for the repo_analyze tool.
type RepoAnalyzeInput struct {
	Repo    string   `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Query   string   `json:"query" jsonschema_description:"What to search for / analyze in the repository"`
	Ref     string   `json:"ref,omitempty" jsonschema_description:"Branch, tag, or commit SHA (default: HEAD)"`
	Focus   string   `json:"focus,omitempty" jsonschema_description:"Subdirectory path or glob to limit scope (e.g. internal/auth, **/*.go), or space-separated keywords (e.g. 'auth handler')"`
	Mode    string   `json:"mode,omitempty" jsonschema_description:"quick (GitHub Code Search, no clone) | raw (code fragments without summary). Default: full AST analysis."`
	Depth   string   `json:"depth,omitempty" jsonschema_description:"Analysis depth: overview (compact) | module (balanced, default) | deep (all files, all symbols)"`
	Type    string   `json:"type,omitempty" jsonschema_description:"Search type: pr (pull requests) or issue (GitHub issues). Switches to GitHub Issues Search API."`
	Repos   []string `json:"repos,omitempty" jsonschema_description:"Multiple repos for quick mode (e.g. ['owner/repo1','owner/repo2'])"`
	Pattern string   `json:"pattern,omitempty" jsonschema_description:"File include pattern for filtering"`
	Format  string   `json:"format,omitempty" jsonschema_description:"Output format: xml (default, structured for AI agents) | text (human-readable) | json (structured envelope)"`
}

// registerRepoAnalyze registers the repo_analyze MCP tool.
func registerRepoAnalyze(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "repo_analyze",
		Description: "Analyze a code repository (GitHub or local) using AST parsing. " +
			"Returns structured mechanical data: symbols with complexity, " +
			"import graph, file relevance scores (BM25F+PageRank), directory tree. " +
			"No LLM involved — all data extracted from tree-sitter ASTs. " +
			"Use mode=quick for fast GitHub Code Search without cloning. " +
			"Use type=pr or type=issue to search pull requests and issues.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RepoAnalyzeInput) (*mcp.CallToolResult, error) {
		if input.Type != "" && input.Type != "pr" && input.Type != "issue" {
			return errResult(fmt.Sprintf("invalid type %q: use pr or issue", input.Type)), nil
		}
		if input.Type == "pr" || input.Type == "issue" {
			return handleIssuesMode(ctx, input, deps)
		}
		if input.Mode == modeQuick || input.Mode == modeRaw {
			return handleQuickMode(ctx, input, deps)
		}
		return handleDeepMode(ctx, input, deps, outputDir)
	})
}

// handleDeepMode performs a full clone + AST analysis of a repository.
func handleDeepMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil
	}
	if input.Depth != "" {
		normalized, ok := analyze.NormalizeDepth(input.Depth)
		if !ok {
			return errResult(fmt.Sprintf("invalid depth %q: use overview, module, or deep", input.Depth)), nil
		}
		input.Depth = normalized
	}
	if input.Format != "" && input.Format != formatText && input.Format != formatJSON && input.Format != formatXML {
		return errResult(fmt.Sprintf("invalid format %q: use xml, text, or json", input.Format)), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, input.Ref, deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	result, err := analyze.AnalyzeRepo(ctx, analyze.RepoAnalysisInput{
		Root:  root,
		Query: input.Query,
		Focus: input.Focus,
		Depth: input.Depth,
	}, deps)
	if err != nil {
		return errResult(fmt.Sprintf("analyze: %s", err)), nil
	}

	formatted := formatAnalysisResult(result, input.Format, input.Depth)

	if outputDir != "" && len(formatted) > maxInlineCharsDefault {
		if path, ok := saveAnalysisFile(formatted, input.Format, outputDir); ok {
			return textResult(buildFileSummary(result, path, len(formatted))), nil
		}
	}

	return textResult(formatted), nil
}

// saveAnalysisFile writes analysis content to a timestamped file in outputDir
// with a format-dependent extension. Returns the file path and true on success.
func saveAnalysisFile(content, format, outputDir string) (string, bool) {
	ext := ".xml"
	switch format {
	case formatJSON:
		ext = ".json"
	case formatText:
		ext = ".txt"
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil { //nolint:mnd
		return "", false
	}

	filename := fmt.Sprintf("repo_analyze_%d%s", time.Now().UnixMilli(), ext)
	path := filepath.Join(outputDir, filename)

	// File must be world-readable so the consuming agent (running as a different user) can access it.
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:mnd,gosec
		return "", false
	}

	return path, true
}

// buildFileSummary creates a concise summary with metadata and the file path.
func buildFileSummary(r *analyze.RepoAnalysisResult, path string, chars int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "repo_analyze: %s | %d files | %s\n", r.RepoName, r.FileCount, r.Language)
	fmt.Fprintf(&sb, "Full output (%d chars) saved to: %s\n\n", chars, path)

	sb.WriteString("Contents:\n")
	fmt.Fprintf(&sb, "- %d packages\n", len(r.Packages))
	if len(r.Files) > 0 {
		fmt.Fprintf(&sb, "- %d files ranked by relevance (BM25F+PageRank)\n", len(r.Files))
	}
	if len(r.Symbols) > 0 {
		fmt.Fprintf(&sb, "- %d symbols with signatures\n", len(r.Symbols))
	}
	if len(r.ImportGraph) > 0 {
		sb.WriteString("- Import dependency graph\n")
	}
	if r.FileTree != "" {
		sb.WriteString("- Directory tree\n")
	}

	sb.WriteString("\nUse Read tool to access the file. Use Grep to search for specific symbols.")

	return sb.String()
}

// handleQuickMode performs a fast GitHub Code Search without cloning the repo.
// For local paths, it returns a directory tree + README without any AST parsing.
func handleQuickMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if isLocalPath(input.Repo) {
		return handleLocalQuickMode(ctx, input, deps)
	}
	repos := resolveQuickRepos(input)
	if len(repos) == 0 {
		return errResult("repo or repos is required for quick mode"), nil
	}

	repoSlug := strings.Join(repos, ", ")
	codeQuery := sanitizeCodeSearchQuery(input.Query)

	results, err := deps.GitHub.SearchCode(ctx, codeQuery, repos)
	if err != nil {
		return errResult(fmt.Sprintf("code search: %s", err)), nil
	}

	if len(results) == 0 {
		return handleQuickFallback(ctx, input, repos, repoSlug, deps)
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s (%s)\n%s\n\n", i+1, r.Path, r.Repo, r.Content)
	}

	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d code matches in %s:\n\n%s", len(results), repoSlug, sb.String())), nil
	}

	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptQuickSearch,
		fmt.Sprintf("Query: %s\n\nCode search results:\n%s", input.Query, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d code matches (LLM unavailable):\n\n%s", len(results), sb.String())), nil
	}

	return textResult(fmt.Sprintf("# Quick Search: %s\nRepos: %s\n\n%s", input.Query, repoSlug, summary)), nil
}

// handleLocalQuickMode returns a directory tree + README for a local repository.
// No LLM, no AST parsing — just a filesystem scan.
func handleLocalQuickMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	root, cleanup, err := resolveRoot(ctx, input.Repo, input.Ref, deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		Focus:        input.Focus,
		MaxFileBytes: 0, // skip file content — only need file list
	})
	if err != nil {
		return errResult(fmt.Sprintf("ingest: %s", err)), nil
	}

	tree := ingest.RenderTree(ir.Files)

	readme := readREADME(root)

	repoName := filepath.Base(root)
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response><quick repo=%q type=\"local\">", repoName)
	fmt.Fprintf(&sb, "<tree><![CDATA[%s]]></tree>", strings.ReplaceAll(tree, "]]>", "]]]]><![CDATA[>"))
	if readme != "" {
		fmt.Fprintf(&sb, "<readme><![CDATA[%s]]></readme>", strings.ReplaceAll(readme, "]]>", "]]]]><![CDATA[>"))
	}
	sb.WriteString("</quick></response>")

	return textResult(sb.String()), nil
}

// readREADME tries to read README.md from root, returning empty string on failure.
func readREADME(root string) string {
	for _, name := range []string{"README.md", "readme.md", "Readme.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err == nil {
			const maxReadmeLen = 8000
			s := string(data)
			if len(s) > maxReadmeLen {
				return s[:maxReadmeLen] + "\n...(truncated)"
			}
			return s
		}
	}
	return ""
}

// handleIssuesMode searches GitHub issues or pull requests via the Issues Search API.
func handleIssuesMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	kind := input.Type
	if isLocalPath(input.Repo) {
		return errResult(kind + " search requires a GitHub repo (owner/repo), not a local path"), nil
	}
	repos := resolveQuickRepos(input)
	if len(repos) == 0 {
		return errResult(fmt.Sprintf("repo is required for %s search", kind)), nil
	}

	repoSlug := strings.Join(repos, ", ")

	var qb strings.Builder
	qb.WriteString("is:")
	qb.WriteString(kind)
	for _, r := range repos {
		qb.WriteString(" repo:")
		qb.WriteString(r)
	}
	if input.Query != "" {
		qb.WriteString(" ")
		qb.WriteString(input.Query)
	}

	issues, err := deps.GitHub.SearchIssues(ctx, qb.String())
	if err != nil {
		return errResult(fmt.Sprintf("issues search: %s", err)), nil
	}

	if len(issues) == 0 {
		return textResult(fmt.Sprintf("No %ss found for query: %s", kind, input.Query)), nil
	}

	var sb strings.Builder
	for i, item := range issues {
		state := item.State
		if item.MergedAt != "" {
			state = "merged"
		}
		fmt.Fprintf(&sb, "[%d] #%d %s\nURL: %s | State: %s | Author: %s | Comments: %d\n",
			i+1, item.Number, item.Title, item.URL, state, item.Author, item.Comments)
		if len(item.Labels) > 0 {
			fmt.Fprintf(&sb, "Labels: %s\n", strings.Join(item.Labels, ", "))
		}
		if item.Body != "" {
			body := item.Body
			const maxBodyLen = 500
			if len(body) > maxBodyLen {
				body = body[:maxBodyLen] + "..."
			}
			fmt.Fprintf(&sb, "Body: %s\n", body)
		}
		sb.WriteString("\n")
	}

	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d %ss:\n\n%s", len(issues), kind, sb.String())), nil
	}

	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptIssuesAnalysis,
		fmt.Sprintf("Query: %s\n\n%s results:\n%s", input.Query, kind, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d %ss (LLM unavailable):\n\n%s", len(issues), kind, sb.String())), nil
	}

	return textResult(fmt.Sprintf("# %s Search: %s\nRepo: %s | Found: %d\n\n%s",
		capitalizeFirst(kind), input.Query, repoSlug, len(issues), summary)), nil
}

// handleQuickFallback fetches repo metadata and README when code search returns nothing.
func handleQuickFallback(ctx context.Context, input RepoAnalyzeInput, repos []string, repoSlug string, deps analyze.Deps) (*mcp.CallToolResult, error) {
	var sb strings.Builder
	for _, r := range repos {
		meta, err := deps.GitHub.FetchRepoMeta(ctx, r)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "Repository: %s\nDescription: %s\nLanguage: %s\nStars: %d\n\n",
			meta.FullName, meta.Description, meta.Language, meta.Stars)
		readme, err := deps.GitHub.FetchREADME(ctx, r)
		if err == nil && readme != "" {
			const maxReadmeLen = 8000
			if len(readme) > maxReadmeLen {
				readme = readme[:maxReadmeLen] + "\n...(truncated)"
			}
			fmt.Fprintf(&sb, "README:\n%s\n\n", readme)
		}
	}

	if sb.Len() == 0 {
		return textResult("No code matches found. Try mode=deep for full repository analysis."), nil
	}

	summary, err := deps.LLM.Complete(ctx, prompts.SystemPromptQuickSearch,
		fmt.Sprintf("Query: %s\n\nRepository overview:\n%s", input.Query, sb.String()))
	if err != nil {
		return textResult("No code matches found. Try mode=deep for full repository analysis."), nil
	}

	return textResult(fmt.Sprintf("# Quick Search: %s\nRepos: %s\n(No code matches — overview from README)\n\n%s",
		input.Query, repoSlug, summary)), nil
}

// ---- Input helpers ----

// isLocalPath returns true if the repo string looks like a local filesystem path.
func isLocalPath(repo string) bool {
	return strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "~")
}

// resolveQuickRepos returns the list of owner/repo slugs for quick/issues modes.
func resolveQuickRepos(input RepoAnalyzeInput) []string {
	if len(input.Repos) > 0 {
		return input.Repos
	}
	if input.Repo == "" || isLocalPath(input.Repo) {
		return nil
	}
	if strings.HasPrefix(input.Repo, "http") {
		owner, repo, ok := github.ExtractOwnerRepo(input.Repo)
		if ok {
			return []string{owner + "/" + repo}
		}
		return nil
	}
	parts := strings.SplitN(input.Repo, "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return []string{input.Repo}
	}
	return nil
}

// sanitizeCodeSearchQuery trims the query to a form safe for GitHub Code Search.
func sanitizeCodeSearchQuery(q string) string {
	const maxLen = 60
	const minWordBoundary = 10

	for _, sep := range []string{",", ";"} {
		if idx := strings.Index(q, sep); idx > 0 {
			q = q[:idx]
		}
	}
	q = strings.TrimSpace(q)
	if len(q) > maxLen {
		if idx := strings.LastIndex(q[:maxLen], " "); idx > minWordBoundary {
			q = q[:idx]
		} else {
			q = q[:maxLen]
		}
	}
	return strings.TrimSpace(q)
}
