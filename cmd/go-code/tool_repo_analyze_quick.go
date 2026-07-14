package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

	f := deps.Forges.ForURL(input.Repo)
	if f == nil {
		f = deps.Forges.Get(forge.GitHub)
	}
	if f == nil {
		return errResult("no forge configured for code search"), nil
	}

	var searchOpts forge.SearchCodeOptions
	searchOpts.MaxResults = 10
	if input.Language != "" {
		searchOpts.Language = input.Language
	}

	result, err := f.SearchCode(ctx, codeQuery, repos, searchOpts)
	if err != nil {
		return errResult(fmt.Sprintf("code search: %s", err)), nil
	}

	if len(result.Results) == 0 {
		return handleQuickFallback(ctx, input, repos, repoSlug, deps)
	}

	var sb strings.Builder
	for i, r := range result.Results {
		fmt.Fprintf(&sb, "[%d] %s (%s)\n%s\n\n", i+1, r.Path, r.Repo, r.Content)
	}

	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d code matches in %s:\n\n%s", len(result.Results), repoSlug, sb.String())), nil
	}

	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptQuickSearch,
		fmt.Sprintf("Query: %s\n\nCode search results:\n%s", input.Query, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d code matches (LLM unavailable):\n\n%s", len(result.Results), sb.String())), nil
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

	return textResult(formatQuickLocal(filepath.Base(root), tree, readme)), nil
}

// quickLocalRespXML is the local-quick-mode response, migrated from hand-rolled
// fmt.Fprintf onto encoding/xml.Marshal (failure class: manual XML
// string-concatenation). The prior formatter emitted `repo=%q` (Go quoting, not
// XML escaping) on the attribute -- now an xml.Marshal attr, escaped by
// construction. The tree/readme bodies keep their CDATA form via the shared
// wrapCDATA helper (byte-identical to the prior inline "]]>" split), reused
// through xmlCDATA's ,innerxml carrier. No xml.Header prolog (fragment consumed
// by the MCP caller).
type quickLocalRespXML struct {
	XMLName xml.Name      `xml:"response"`
	Quick   quickLocalXML `xml:"quick"`
}

type quickLocalXML struct {
	Repo   string    `xml:"repo,attr"`
	Type   string    `xml:"type,attr"`
	Tree   xmlCDATA  `xml:"tree"`
	Readme *xmlCDATA `xml:"readme,omitempty"`
}

// formatQuickLocal renders the local-quick-mode <response><quick> document.
func formatQuickLocal(repoName, tree, readme string) string {
	resp := quickLocalRespXML{
		Quick: quickLocalXML{
			Repo: repoName,
			Type: "local",
			Tree: xmlCDATA{Inner: wrapCDATA(tree)},
		},
	}
	if readme != "" {
		resp.Quick.Readme = &xmlCDATA{Inner: wrapCDATA(readme)}
	}
	return xmlMarshalFragment(resp)
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

// handleQuickFallback fetches repo metadata and README when code search returns nothing.
func handleQuickFallback(ctx context.Context, input RepoAnalyzeInput, repos []string, repoSlug string, deps analyze.Deps) (*mcp.CallToolResult, error) {
	fb := deps.Forges.ForURL(input.Repo)
	if fb == nil {
		fb = deps.Forges.Get(forge.GitHub)
	}

	var sb strings.Builder
	for _, r := range repos {
		if fb == nil {
			continue
		}
		meta, err := fb.FetchRepoMeta(ctx, r)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "Repository: %s\nDescription: %s\nLanguage: %s\nStars: %d\n\n",
			meta.FullName, meta.Description, meta.Language, meta.Stars)
		readme, err := fb.FetchREADME(ctx, r)
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
		owner, repo, ok := forge.ExtractOwnerRepo(input.Repo)
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
