package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/ingest"
)

// rewritePath applies path mappings, translating external prefixes to internal ones.
func rewritePath(path string, mappings []analyze.PathMapping) string {
	for _, m := range mappings {
		if strings.HasPrefix(path, m.External) {
			return m.Internal + path[len(m.External):]
		}
	}
	return path
}

// makePathRewrite returns a func that applies mappings to a path, or nil when
// mappings is empty. The nil case signals callers that no rewriting is needed
// so they can skip worktree .git file parsing entirely.
func makePathRewrite(mappings []analyze.PathMapping) func(string) string {
	if len(mappings) == 0 {
		return nil
	}
	return func(path string) string {
		return rewritePath(path, mappings)
	}
}

// resolveRoot returns the local root path for the given repo input.
// For remote repos, it clones into the workspace (at the given ref) and returns
// the clone dir along with a cleanup function. For local paths, it validates the
// directory exists and returns a no-op cleanup.
func resolveRoot(ctx context.Context, repo, ref string, deps analyze.Deps) (root string, cleanup func(), err error) {
	// Strip common prefix mistakes from callers.
	repo = strings.TrimPrefix(repo, "path=")
	repo = strings.TrimPrefix(repo, "local:")

	// WordPress plugin — download from wordpress.org.
	if ingest.IsWordPressPlugin(repo) {
		slug, err := ingest.NormalizeWPSlug(repo)
		if err != nil {
			return "", nil, fmt.Errorf("invalid wp plugin: %w", err)
		}
		result, err := ingest.FetchWPPlugin(ctx, ingest.WPPluginOpts{
			Slug:    slug,
			DestDir: deps.WorkspaceDir,
		})
		if err != nil {
			return "", nil, fmt.Errorf("fetch wp plugin: %w", err)
		}
		dir := result.LocalPath
		return dir, func() {
			if err := ingest.CleanupCloneDir(dir); err != nil {
				slog.Warn("failed to cleanup wp plugin dir", slog.String("dir", dir), slog.Any("error", err))
			}
		}, nil
	}

	if forge.IsRemote(repo) {
		slug, ok := forge.ExtractSlug(repo)
		if !ok {
			return "", nil, fmt.Errorf("invalid repo: cannot extract slug from %q", repo)
		}
		token, tokErr := cloneToken(ctx, deps)
		if tokErr != nil {
			return "", nil, fmt.Errorf("get clone token: %w", tokErr)
		}
		kind := forge.DetectForge(repo)
		cloneURL := forge.CloneURL(kind, slug, "", token)
		result, err := ingest.CloneRepo(ctx, ingest.CloneOpts{
			Slug:        slug,
			Ref:         ref,
			DestDir:     deps.WorkspaceDir,
			GithubToken: token,
			CloneURL:    cloneURL,
			TokenFunc:   deps.CloneTokenFunc,
		})
		if err != nil {
			return "", nil, fmt.Errorf("clone: %w", err)
		}
		dir := result.LocalPath
		return dir, func() {
			if err := ingest.CleanupCloneDir(dir); err != nil {
				slog.Warn("failed to cleanup clone dir", slog.String("dir", dir), slog.Any("error", err))
			}
		}, nil
	}
	// Local path — apply path mappings (e.g. /path/to/repos:/host) from PATH_MAPPINGS.
	repo = rewritePath(repo, deps.PathMappings)
	fi, err := os.Stat(repo)
	if err != nil {
		return "", nil, fmt.Errorf("local path: %w", err)
	}
	if !fi.IsDir() {
		return "", nil, fmt.Errorf("local path %q: not a directory", repo)
	}
	return repo, func() {}, nil
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
