package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		// Prefer a local checkout (e.g. /host/src/<name>) whose origin remote
		// matches the slug — the autoindexer already maintains it, and cloning a
		// private repo without a configured GitHub App yields an empty tree.
		if local := localCheckoutFor(ctx, slug, deps.LocalRepoDirs); local != "" {
			src = workspace.LocalSource{Path: local, Mappings: deps.PathMappings}
		} else {
			src = workspace.RemoteSource{
				Slug:        slug,
				RepoInput:   repo,
				Ref:         ref,
				DestDir:     deps.WorkspaceDir,
				TokenFunc:   deps.CloneTokenFunc,
				StaticToken: deps.GithubToken,
			}
		}

	default:
		src = workspace.LocalSource{Path: repo, Mappings: deps.PathMappings}
	}

	return src.Root(ctx)
}

// localCheckoutFor returns the path to a local git checkout of slug under one of
// dirs (e.g. /host/src/<repo>) when one exists and its origin remote resolves to
// the same slug. Returns "" when none matches, signalling the caller to clone.
// Preferring a local checkout avoids a redundant clone — which yields an empty
// tree for private repos when no GitHub App is configured.
func localCheckoutFor(ctx context.Context, slug string, dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	name := slug
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return ""
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(filepath.Join(candidate, ".git")); err != nil {
			continue // not a git checkout
		}
		out, err := exec.CommandContext(ctx, "git", "-C", candidate, "remote", "get-url", "origin").Output()
		if err != nil {
			continue
		}
		if s, ok := forge.ExtractSlug(strings.TrimSpace(string(out))); ok && strings.EqualFold(s, slug) {
			return candidate
		}
	}
	return ""
}
