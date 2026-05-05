package main

import (
"context"
"fmt"
"os"
"path/filepath"
"strings"
"time"

"github.com/anatolykoptev/go-code/internal/analyze"
"github.com/anatolykoptev/go-code/internal/compare"
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

var extras *repoAnalysisExtras
if input.Depth == analyze.DepthDeep {
apiSurface := compare.ExtractAPISurface(result.Symbols, result.Language)
var freshnessStats *compare.FreshnessStats
{
fctx, fcancel := context.WithTimeout(ctx, 10*time.Second)
defer fcancel()
freshnessStats, _, _ = compare.CollectFreshness(fctx, root)
}
extras = &repoAnalysisExtras{
APISurfaceSize: len(apiSurface),
FreshnessStats: freshnessStats,
}
if qs := collectQualityStats(ctx, root, result.Language, deps); qs != nil {
extras.QualityStats = qs
}
}

// Fetch architecturally central symbols from the AGE graph (any depth).
// deps.Graph is always non-nil; Noop returns nil, nil when no snapshot exists.
{
_ = root // root passed directly to TopPageRank
const archTopK = 5
signals, graphErr := deps.Graph.TopPageRank(ctx, root, archTopK)
if graphErr == nil && len(signals) > 0 {
syms := make([]xmlArchSymbol, 0, len(signals))
for _, sig := range signals {
syms = append(syms, xmlArchSymbol{
Name:     sig.Symbol.Name,
File:     sig.Symbol.File,
PageRank: sig.PageRank,
})
}
archCentral := &xmlArchCentral{Available: true, Symbols: syms}
if extras == nil {
extras = &repoAnalysisExtras{}
}
extras.ArchCentral = archCentral
}
}

formatted := formatAnalysisResult(result, input.Format, input.Depth, extras)

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
