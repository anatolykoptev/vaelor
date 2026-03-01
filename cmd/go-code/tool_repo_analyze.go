package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/github"
	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Mode constants for repo_analyze.
const (
	modeQuick = "quick"
	modeRaw   = "raw"
)

// RepoAnalyzeInput is the input schema for the repo_analyze tool.
type RepoAnalyzeInput struct {
	Repo    string   `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Query   string   `json:"query" jsonschema_description:"What to search for / analyze in the repository"`
	Ref     string   `json:"ref,omitempty" jsonschema_description:"Branch, tag, or commit SHA (default: HEAD)"`
	Focus   string   `json:"focus,omitempty" jsonschema_description:"Subdirectory or glob pattern to focus on (e.g. internal/auth or **/*.go)"`
	Mode    string   `json:"mode,omitempty" jsonschema_description:"quick (GitHub Code Search, no clone) | raw (code fragments without summary). Default: full AST analysis."`
	Depth   string   `json:"depth,omitempty" jsonschema_description:"Analysis depth: overview (compact) | module (balanced, default) | deep (all files, all symbols)"`
	Type    string   `json:"type,omitempty" jsonschema_description:"Search type: pr (pull requests) or issue (GitHub issues). Switches to GitHub Issues Search API."`
	Repos   []string `json:"repos,omitempty" jsonschema_description:"Multiple repos for quick mode (e.g. ['owner/repo1','owner/repo2'])"`
	Pattern string   `json:"pattern,omitempty" jsonschema_description:"File include pattern for filtering"`
	Format  string   `json:"format,omitempty" jsonschema_description:"Output format: xml (default, structured for AI agents) | text (human-readable) | json (structured envelope)"`
}

// registerRepoAnalyze registers the repo_analyze MCP tool.
func registerRepoAnalyze(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "repo_analyze",
		Description: "Analyze a code repository (GitHub or local) using AST parsing. " +
			"Returns structured mechanical data: symbols with complexity, " +
			"import graph, file relevance scores (BM25F+PageRank), directory tree. " +
			"No LLM involved — all data extracted from tree-sitter ASTs. " +
			"Use mode=quick for fast GitHub Code Search without cloning. " +
			"Use type=pr or type=issue to search pull requests and issues.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RepoAnalyzeInput) (*mcp.CallToolResult, any, error) {
		if input.Type != "" && input.Type != "pr" && input.Type != "issue" {
			return errResult(fmt.Sprintf("invalid type %q: use pr or issue", input.Type)), nil, nil
		}
		if input.Type == "pr" || input.Type == "issue" {
			return handleIssuesMode(ctx, input, deps)
		}
		if input.Mode == modeQuick || input.Mode == modeRaw {
			return handleQuickMode(ctx, input, deps)
		}
		return handleDeepMode(ctx, input, deps)
	})
}

// handleDeepMode performs a full clone + AST analysis of a repository.
func handleDeepMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil, nil
	}
	if input.Depth != "" && !analyze.ValidDepth(input.Depth) {
		return errResult(fmt.Sprintf("invalid depth %q: use overview, module, or deep", input.Depth)), nil, nil
	}
	if input.Format != "" && input.Format != "text" && input.Format != "json" && input.Format != "xml" {
		return errResult(fmt.Sprintf("invalid format %q: use xml, text, or json", input.Format)), nil, nil
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
		Depth: input.Depth,
	}, deps)
	if err != nil {
		return errResult(fmt.Sprintf("analyze: %s", err)), nil, nil
	}

	return textResult(formatAnalysisResult(result, input.Format, input.Depth)), nil, nil
}

// handleQuickMode performs a fast GitHub Code Search without cloning the repo.
func handleQuickMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if isLocalPath(input.Repo) {
		return errResult("quick mode requires a GitHub repo (owner/repo), not a local path. Use deep mode for local paths."), nil, nil
	}
	repos := resolveQuickRepos(input)
	if len(repos) == 0 {
		return errResult("repo or repos is required for quick mode"), nil, nil
	}

	repoSlug := strings.Join(repos, ", ")
	codeQuery := sanitizeCodeSearchQuery(input.Query)

	results, err := deps.GitHub.SearchCode(ctx, codeQuery, repos)
	if err != nil {
		return errResult(fmt.Sprintf("code search: %s", err)), nil, nil
	}

	if len(results) == 0 {
		return handleQuickFallback(ctx, input, repos, repoSlug, deps)
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s (%s)\n%s\n\n", i+1, r.Path, r.Repo, r.Content)
	}

	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d code matches in %s:\n\n%s", len(results), repoSlug, sb.String())), nil, nil
	}

	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptQuickSearch,
		fmt.Sprintf("Query: %s\n\nCode search results:\n%s", input.Query, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d code matches (LLM unavailable):\n\n%s", len(results), sb.String())), nil, nil
	}

	return textResult(fmt.Sprintf("# Quick Search: %s\nRepos: %s\n\n%s", input.Query, repoSlug, summary)), nil, nil
}

// handleIssuesMode searches GitHub issues or pull requests via the Issues Search API.
func handleIssuesMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	kind := input.Type
	if isLocalPath(input.Repo) {
		return errResult(kind + " search requires a GitHub repo (owner/repo), not a local path"), nil, nil
	}
	repos := resolveQuickRepos(input)
	if len(repos) == 0 {
		return errResult(fmt.Sprintf("repo is required for %s search", kind)), nil, nil
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
		return errResult(fmt.Sprintf("issues search: %s", err)), nil, nil
	}

	if len(issues) == 0 {
		return textResult(fmt.Sprintf("No %ss found for query: %s", kind, input.Query)), nil, nil
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
		return textResult(fmt.Sprintf("Found %d %ss:\n\n%s", len(issues), kind, sb.String())), nil, nil
	}

	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptIssuesAnalysis,
		fmt.Sprintf("Query: %s\n\n%s results:\n%s", input.Query, kind, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d %ss (LLM unavailable):\n\n%s", len(issues), kind, sb.String())), nil, nil
	}

	return textResult(fmt.Sprintf("# %s Search: %s\nRepo: %s | Found: %d\n\n%s",
		capitalizeFirst(kind), input.Query, repoSlug, len(issues), summary)), nil, nil
}

// handleQuickFallback fetches repo metadata and README when code search returns nothing.
func handleQuickFallback(ctx context.Context, input RepoAnalyzeInput, repos []string, repoSlug string, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
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
		return textResult("No code matches found. Try mode=deep for full repository analysis."), nil, nil
	}

	summary, err := deps.LLM.Complete(ctx, prompts.SystemPromptQuickSearch,
		fmt.Sprintf("Query: %s\n\nRepository overview:\n%s", input.Query, sb.String()))
	if err != nil {
		return textResult("No code matches found. Try mode=deep for full repository analysis."), nil, nil
	}

	return textResult(fmt.Sprintf("# Quick Search: %s\nRepos: %s\n(No code matches — overview from README)\n\n%s",
		input.Query, repoSlug, summary)), nil, nil
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
