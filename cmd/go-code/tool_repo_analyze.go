package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/github"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/anatolykoptev/go-code/internal/render"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Mode constants for repo_analyze.
const (
	modeQuick = "quick"
	modeRaw   = "raw"
)

// RepoAnalyzeInput is the input schema for the repo_analyze tool.
type RepoAnalyzeInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema_description:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	// Query is a natural-language question about the repository.
	Query string `json:"query" jsonschema_description:"What you want to understand about the repository"`

	// Ref is the branch, tag, or commit SHA to analyze (default: default branch).
	Ref string `json:"ref,omitempty" jsonschema_description:"Branch, tag, or commit SHA (default: HEAD)"`

	// Focus narrows analysis to a subdirectory or file glob pattern.
	Focus string `json:"focus,omitempty" jsonschema_description:"Subdirectory or glob pattern to focus on (e.g. internal/auth or **/*.go)"`

	// Mode controls how file contents are rendered for LLM context.
	Mode string `json:"mode,omitempty" jsonschema_description:"Rendering mode: signatures (API only) | skeleton (structure with ... placeholders) | focused (full for relevant symbols, signatures for rest) | quick (GitHub Code Search, no clone) | raw (code fragments without LLM summary). Default: full content."`

	// Depth controls analysis depth: overview (high-level, compact), module (default, balanced), deep (detailed).
	Depth string `json:"depth,omitempty" jsonschema_description:"Analysis depth: overview (high-level, 50K context) | module (balanced, 150K context, default) | deep (detailed, 200K context)"`

	// Type selects search type: pr (pull requests) or issue (GitHub issues). Switches to GitHub Issues Search API.
	Type string `json:"type,omitempty" jsonschema_description:"Search type: pr (pull requests) or issue (GitHub issues). Switches to GitHub Issues Search API."`

	// Repos lists multiple repos for quick mode (e.g. ['owner/repo1','owner/repo2']).
	Repos []string `json:"repos,omitempty" jsonschema_description:"Multiple repos for quick mode (e.g. ['owner/repo1','owner/repo2'])"`

	// Pattern is a file include pattern for filtering.
	Pattern string `json:"pattern,omitempty" jsonschema_description:"File include pattern for filtering"`

	// Format controls output format: xml (default), text, or json.
	Format string `json:"format,omitempty" jsonschema_description:"Output format: xml (default, structured for AI agents) | text (human-readable) | json (structured envelope)"`
}

// registerRepoAnalyze registers the repo_analyze MCP tool.
// Analyzes a repository: clones (if remote), walks the file tree, parses ASTs,
// and produces a structured LLM-powered answer about the codebase.
func registerRepoAnalyze(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "repo_analyze",
		Description: "Analyze a code repository (GitHub or local). " +
			"Clones the repo if remote, walks the file tree, parses ASTs with tree-sitter, " +
			"and answers a natural-language question about the codebase structure, " +
			"architecture, or implementation details. " +
			"Use mode=quick for fast GitHub Code Search without cloning. " +
			"Use type=pr or type=issue to search pull requests and issues.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RepoAnalyzeInput) (*mcp.CallToolResult, any, error) {
		// Validate type parameter if provided.
		if input.Type != "" && input.Type != "pr" && input.Type != "issue" {
			return errResult(fmt.Sprintf("invalid type %q: use pr or issue", input.Type)), nil, nil
		}
		// Issues/PRs mode — uses GitHub Issues Search API, no cloning required.
		if input.Type == "pr" || input.Type == "issue" {
			return handleIssuesMode(ctx, input, deps)
		}
		// Quick/raw mode — uses GitHub Code Search, no cloning required.
		if input.Mode == modeQuick || input.Mode == modeRaw {
			return handleQuickMode(ctx, input, deps)
		}
		return handleDeepMode(ctx, input, deps)
	})
}

// handleDeepMode performs a full clone + AST analysis of a repository.
// It is the default path when no lightweight mode is selected.
func handleDeepMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil, nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil, nil
	}
	if input.Mode != "" && !render.ValidMode(input.Mode) {
		return errResult(fmt.Sprintf("invalid mode %q: use signatures, skeleton, or focused", input.Mode)), nil, nil
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

	// If user specified depth but not mode, use the default mode for that depth.
	renderMode := input.Mode
	if renderMode == "" && input.Depth != "" {
		renderMode = analyze.DefaultModeForDepth(input.Depth)
	}

	result, err := analyze.AnalyzeRepo(ctx, analyze.RepoAnalysisInput{
		Root:       root,
		Query:      input.Query,
		Focus:      input.Focus,
		RenderMode: renderMode,
		Depth:      input.Depth,
	}, deps)
	if err != nil {
		return errResult(fmt.Sprintf("analyze: %s", err)), nil, nil
	}

	return textResult(formatAnalysisResult(result, input.Format)), nil, nil
}

// handleQuickMode performs a fast GitHub Code Search without cloning the repo.
// Falls back to README + repo metadata if no code matches are found.
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

	// Raw mode: return fragments without LLM summarization.
	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d code matches in %s:\n\n%s", len(results), repoSlug, sb.String())), nil, nil
	}

	// Quick mode: summarize with LLM.
	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptQuickSearch,
		fmt.Sprintf("Query: %s\n\nCode search results:\n%s", input.Query, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d code matches (LLM unavailable):\n\n%s", len(results), sb.String())), nil, nil
	}

	return textResult(fmt.Sprintf("# Quick Search: %s\nRepos: %s\n\n%s", input.Query, repoSlug, summary)), nil, nil
}

// handleIssuesMode searches GitHub issues or pull requests via the Issues Search API.
func handleIssuesMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, any, error) {
	kind := input.Type // "pr" or "issue"
	if isLocalPath(input.Repo) {
		return errResult(kind + " search requires a GitHub repo (owner/repo), not a local path"), nil, nil
	}
	repos := resolveQuickRepos(input)
	if len(repos) == 0 {
		return errResult(fmt.Sprintf("repo is required for %s search", kind)), nil, nil
	}

	repoSlug := strings.Join(repos, ", ")

	// Build GitHub Issues search query.
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

	// Raw mode: return items without LLM summarization.
	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d %ss:\n\n%s", len(issues), kind, sb.String())), nil, nil
	}

	// LLM summarize.
	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptIssuesAnalysis,
		fmt.Sprintf("Query: %s\n\n%s results:\n%s", input.Query, kind, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d %ss (LLM unavailable):\n\n%s", len(issues), kind, sb.String())), nil, nil
	}

	return textResult(fmt.Sprintf("# %s Search: %s\nRepo: %s | Found: %d\n\n%s",
		capitalizeFirst(kind), input.Query, repoSlug, len(issues), summary)), nil, nil
}

// isLocalPath returns true if the repo string looks like a local filesystem path.
func isLocalPath(repo string) bool {
	return strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "~")
}

// resolveQuickRepos returns the list of owner/repo slugs for quick/issues modes.
// Prefers input.Repos; falls back to parsing input.Repo.
// Returns nil for local paths (quick/issues modes require GitHub repos).
func resolveQuickRepos(input RepoAnalyzeInput) []string {
	if len(input.Repos) > 0 {
		return input.Repos
	}
	if input.Repo == "" || isLocalPath(input.Repo) {
		return nil
	}
	// Try as a full GitHub URL (https://github.com/owner/repo).
	if strings.HasPrefix(input.Repo, "http") {
		owner, repo, ok := github.ExtractOwnerRepo(input.Repo)
		if ok {
			return []string{owner + "/" + repo}
		}
		return nil
	}
	// Try direct slug (owner/repo).
	parts := strings.SplitN(input.Repo, "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return []string{input.Repo}
	}
	return nil
}

// sanitizeCodeSearchQuery trims the query to a form safe for GitHub Code Search:
// strips everything after a comma or semicolon and truncates to 60 characters
// at a word boundary.
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

// capitalizeFirst returns s with the first Unicode letter uppercased.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// responseEnvelope wraps analysis results in a structured JSON envelope.
type responseEnvelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	Data          envelopeData    `json:"data"`
	Meta          envelopeMeta    `json:"meta"`
	SuggestedNext []suggestedCall `json:"suggestedNextCalls,omitempty"`
}

type envelopeData struct {
	Answer    string   `json:"answer"`
	RepoName  string   `json:"repoName"`
	Language  string   `json:"language"`
	FileCount int      `json:"fileCount"`
	Packages  []string `json:"packages,omitempty"`
}

type envelopeMeta struct {
	FilesAnalyzed int  `json:"filesAnalyzed"`
	Truncated     bool `json:"truncated"`
}

type suggestedCall struct {
	Tool   string            `json:"tool"`
	Params map[string]string `json:"params"`
	Reason string            `json:"reason"`
}

// formatAnalysisResult dispatches to xml, text, or JSON formatting based on format.
// Defaults to XML when format is empty.
func formatAnalysisResult(r *analyze.RepoAnalysisResult, format string) string {
	switch format {
	case "json":
		return formatAnalysisJSON(r)
	case "text":
		return formatAnalysisText(r)
	default:
		return formatAnalysisXML(r)
	}
}

// formatAnalysisJSON formats a RepoAnalysisResult as a structured JSON envelope.
func formatAnalysisJSON(r *analyze.RepoAnalysisResult) string {
	env := responseEnvelope{
		SchemaVersion: "1.0",
		Data: envelopeData{
			Answer:    r.Answer,
			RepoName:  r.RepoName,
			Language:  r.Language,
			FileCount: r.FileCount,
			Packages:  r.Packages,
		},
		Meta: envelopeMeta{
			FilesAnalyzed: r.FileCount,
		},
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// xmlResponse is the top-level XML envelope for repo_analyze output.
type xmlResponse struct {
	XMLName       xml.Name    `xml:"response"`
	SchemaVersion string      `xml:"schemaVersion,attr"`
	Repo          xmlRepo     `xml:"repo"`
	Packages      xmlPackages `xml:"packages"`
	Symbols       xmlSymbols  `xml:"symbols"`
	Analysis      string      `xml:"analysis"`
}

type xmlRepo struct {
	Name     string `xml:"name,attr"`
	Language string `xml:"language,attr"`
	Files    int    `xml:"files,attr"`
}

type xmlPackages struct {
	Items []string `xml:"package"`
}

type xmlSymbol struct {
	Kind      string `xml:"kind,attr"`
	Name      string `xml:"name,attr"`
	File      string `xml:"file,attr"`
	Line      uint32 `xml:"line,attr"`
	Signature string `xml:"signature,omitempty"`
}

type xmlSymbols struct {
	Items []xmlSymbol `xml:"symbol"`
}

// formatAnalysisXML formats a RepoAnalysisResult as structured XML.
func formatAnalysisXML(r *analyze.RepoAnalysisResult) string {
	resp := xmlResponse{
		SchemaVersion: "1.0",
		Repo: xmlRepo{
			Name:     r.RepoName,
			Language: r.Language,
			Files:    r.FileCount,
		},
		Packages: xmlPackages{Items: r.Packages},
		Analysis: r.Answer,
	}

	symbols := make([]xmlSymbol, 0, len(r.Symbols))
	for _, sym := range r.Symbols {
		xs := xmlSymbol{
			Kind: string(sym.Kind),
			Name: sym.Name,
			File: sym.File,
			Line: sym.StartLine,
		}
		if sym.Signature != "" {
			xs.Signature = truncateSignature(sym.Signature)
		}
		symbols = append(symbols, xs)
	}
	resp.Symbols = xmlSymbols{Items: symbols}

	b, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return fmt.Sprintf("<error>%s</error>", err.Error())
	}
	return xml.Header + string(b)
}

// formatAnalysisText formats a RepoAnalysisResult as human-readable text.
func formatAnalysisText(r *analyze.RepoAnalysisResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# Repository: %s\n", r.RepoName)
	fmt.Fprintf(&sb, "Language: %s | Files analyzed: %d\n\n", r.Language, r.FileCount)

	if len(r.Packages) > 0 {
		fmt.Fprintf(&sb, "## Packages (%d)\n", len(r.Packages))
		for _, pkg := range r.Packages {
			fmt.Fprintf(&sb, "  - %s\n", pkg)
		}
		sb.WriteString("\n")
	}

	if len(r.Symbols) > 0 {
		fmt.Fprintf(&sb, "## Key Symbols (%d)\n", len(r.Symbols))
		for _, sym := range r.Symbols {
			writeSymbolLine(&sb, sym)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "## Analysis\n%s\n", r.Answer)

	return sb.String()
}

// maxSignatureLen is the maximum length for symbol signatures before truncation.
const maxSignatureLen = 200

// truncateSignature truncates a signature to maxSignatureLen characters.
func truncateSignature(sig string) string {
	if len(sig) <= maxSignatureLen {
		return sig
	}
	return sig[:maxSignatureLen] + "..."
}

// writeSymbolLine writes a single symbol summary line into sb.
func writeSymbolLine(sb *strings.Builder, sym *parser.Symbol) {
	if sym.Signature != "" {
		fmt.Fprintf(sb, "  [%s] %s — %s (line %d)\n", sym.Kind, sym.Name, truncateSignature(sym.Signature), sym.StartLine)
	} else {
		fmt.Fprintf(sb, "  [%s] %s (line %d)\n", sym.Kind, sym.Name, sym.StartLine)
	}
}

// errResult returns a CallToolResult representing a tool-level error.
func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// textResult returns a CallToolResult with text content.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
