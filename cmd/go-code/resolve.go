package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/workspace"
)

// rewritePath applies path mappings, translating external prefixes to internal ones.
// Kept for callers in this package (tool_file_parse.go, tool_debug_investigate_body.go).
func rewritePath(path string, mappings []analyze.PathMapping) string {
	return workspace.RewritePath(path, mappings)
}

// makePathRewrite returns a func that applies mappings to a path, or nil when
// mappings is empty. The nil case signals callers that no rewriting is needed
// so they can skip worktree .git file parsing entirely.
func makePathRewrite(mappings []analyze.PathMapping) func(string) string {
	if len(mappings) == 0 {
		return nil
	}
	return func(path string) string {
		return workspace.RewritePath(path, mappings)
	}
}

// resolveRoot returns the local root path for the given repo input.
// For remote repos, it clones into the workspace (at the given ref) and returns
// the clone dir along with a cleanup function. For local paths, it validates the
// directory exists and returns a no-op cleanup.
//
// Input shape dispatch:
//   - WordPress plugin slug  → workspace.WPSource (download from wordpress.org)
//   - Remote owner/repo slug → workspace.RemoteSource (clone)
//   - Local path             → workspace.LocalSource (PATH_MAPPINGS applied)
func resolveRoot(ctx context.Context, repo, ref string, deps analyze.Deps) (root string, cleanup func(), err error) {
	// Strip common prefix mistakes from callers.
	repo = strings.TrimPrefix(repo, "path=")
	repo = strings.TrimPrefix(repo, "local:")

	var src workspace.Source
	switch {
	case ingest.IsWordPressPlugin(repo):
		src = workspace.WPSource{Slug: repo, DestDir: deps.WorkspaceDir}

	case forge.IsRemote(repo):
		slug, ok := forge.ExtractSlug(repo)
		if !ok {
			return "", nil, fmt.Errorf("invalid repo: cannot extract slug from %q", repo)
		}
		src = workspace.RemoteSource{
			Slug:        slug,
			RepoInput:   repo,
			Ref:         ref,
			DestDir:     deps.WorkspaceDir,
			TokenFunc:   deps.CloneTokenFunc,
			StaticToken: deps.GithubToken,
		}

	default:
		src = workspace.LocalSource{Path: repo, Mappings: deps.PathMappings}
	}

	return src.Root(ctx)
}

// cloneToken returns the token to use for authenticated git clones.
// Prefers CloneTokenFunc (GitHub App installation token) when set;
// falls back to the static GithubToken PAT.
func cloneToken(ctx context.Context, deps analyze.Deps) (string, error) {
	if deps.CloneTokenFunc != nil {
		return deps.CloneTokenFunc(ctx)
	}
	return deps.GithubToken, nil
}
