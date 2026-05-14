package ingest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/slugparse"
)

// dirPerm is the permission mode for created workspace directories.
const dirPerm = 0o750

// CloneOpts controls how a repository is cloned.
type CloneOpts struct {
	// Slug is the owner/repo slug used for directory naming.
	Slug string

	// Ref is the branch, tag, or commit SHA to check out.
	// Empty means the default branch.
	Ref string

	// DestDir is the parent directory where the clone will be placed.
	// A subdirectory named after the repo slug will be created inside.
	DestDir string

	// GithubToken is used for authenticated clones (private repos, higher rate limits).
	// Ignored when CloneURL is set (the URL already encodes credentials).
	GithubToken string

	// CloneURL is a pre-built HTTPS clone URL. When non-empty, it is used
	// directly and the GitHub-specific URL building logic is skipped.
	// Slug is still required for directory naming.
	CloneURL string

	// TokenFunc, when set, is called before each git fetch to obtain a fresh
	// credential token. The returned token is passed to git via
	// GIT_CONFIG_COUNT / GIT_CONFIG_KEY_0 / GIT_CONFIG_VALUE_0 so that it
	// is only visible to the subprocess and never persisted to .git/config.
	//
	// When nil, the static GithubToken embedded in the remote URL at clone
	// time is used (legacy behaviour). Callers that hold a token source
	// (e.g. a GitHub App installation-token refresher) should always supply
	// TokenFunc to avoid fetch failures after the ~1 h installation-token
	// expiry.
	TokenFunc func(ctx context.Context) (string, error)
}

// CloneResult contains the result of a successful clone.
type CloneResult struct {
	// LocalPath is the absolute path to the cloned repository root.
	LocalPath string

	// Ref is the actual ref checked out (may differ if HEAD was resolved).
	Ref string
}

// IsRemote returns true if the input looks like a GitHub slug or URL rather
// than a local filesystem path.
func IsRemote(input string) bool {
	if IsWordPressPlugin(input) {
		return false
	}
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return false
	}
	_, err := NormalizeSlug(input)
	return err == nil
}

// NormalizeSlug extracts the canonical "owner/repo" form from any of:
//   - owner/repo
//   - github.com/owner/repo[.git]
//   - gitlab.com/owner/repo[.git]
//   - https?://github.com/owner/repo[.git]
//   - https?://gitlab.com/owner/repo[.git]
//   - git@github.com:owner/repo[.git]
//   - git@gitlab.com:owner/repo[.git]
//
// Returns an error if the input does not match any recognised form.
// This is a thin wrapper around slugparse.Parse.
func NormalizeSlug(input string) (string, error) {
	return slugparse.Parse(input)
}

// CloneRepo performs a shallow git clone of the given repository into DestDir.
// If Ref is specified, it checks out that ref after cloning.
func CloneRepo(ctx context.Context, opts CloneOpts) (*CloneResult, error) {
	slug, err := NormalizeSlug(opts.Slug)
	if err != nil {
		return nil, err
	}

	repoName := filepath.Base(slug)
	localPath := filepath.Join(opts.DestDir, strings.ReplaceAll(slug, "/", "_"))

	// Cache hit — refresh to remote HEAD instead of trusting on-disk state.
	// Without this, the race window between concurrent calls' defer cleanup()
	// (cmd/go-code/resolve.go) and the lack of webhook-driven invalidation
	// can leave the workspace stale relative to a recent push.
	if _, statErr := os.Stat(localPath); statErr == nil {
		if err := refreshClone(ctx, localPath, opts.Ref, opts.TokenFunc); err == nil {
			return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
		}
		// Refresh failed (corrupt repo, network blip, missing ref, expired
		// token with no TokenFunc) — wipe and fall through to a fresh shallow
		// clone below.
		_ = os.RemoveAll(localPath)
	}

	if err := os.MkdirAll(opts.DestDir, dirPerm); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	cloneURL := opts.CloneURL
	if cloneURL == "" {
		cloneURL = fmt.Sprintf("https://github.com/%s.git", slug)
		if opts.GithubToken != "" {
			cloneURL = fmt.Sprintf("https://%s@github.com/%s.git", opts.GithubToken, slug)
		}
	}

	args := []string{"clone", "--depth=2", "--single-branch", "--filter=blob:none"}
	if opts.Ref != "" {
		args = append(args, "--branch", opts.Ref)
	}
	args = append(args, cloneURL, localPath)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git clone %s: %w\n%s", repoName, err, string(out))
	}

	return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
}

// refreshClone updates an existing shallow clone at localPath to match
// remote origin's tip of ref (or default HEAD when ref is empty).
//
// When tokenFunc is non-nil it is called to obtain a fresh credential token.
// The token is injected into git's subprocess environment via
// GIT_CONFIG_COUNT / GIT_CONFIG_KEY_0 / GIT_CONFIG_VALUE_0 so the credential
// is never written to .git/config and is safe for concurrent callers.
//
// Returns an error if any git operation fails so the caller can wipe
// and re-clone instead of trusting potentially stale state.
func refreshClone(ctx context.Context, localPath, ref string, tokenFunc func(context.Context) (string, error)) error {
	branch := ref
	if branch == "" {
		branch = "HEAD"
	}

	fetchEnv := append(os.Environ(), "GIT_TERMINAL_PROMPT=0") //nolint:gocritic // intentional copy-on-append
	if tokenFunc != nil {
		tok, err := tokenFunc(ctx)
		if err != nil {
			return fmt.Errorf("refresh token: %w", err)
		}
		if tok != "" {
			// Use git's per-process config injection to supply the credential.
			// This avoids mutating .git/config and is safe for concurrent calls.
			// GitHub accepts both "token <tok>" and "Bearer <tok>" for
			// installation tokens; "token" is the canonical form for PATs and
			// installation tokens alike (see GitHub REST API docs).
			fetchEnv = append(fetchEnv,
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.extraheader",
				"GIT_CONFIG_VALUE_0=Authorization: token "+tok,
			)
		}
	}

	fetch := exec.CommandContext(ctx, "git", "-C", localPath,
		"fetch", "--depth=2", "origin", branch)
	fetch.Env = fetchEnv
	if out, err := fetch.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %w\n%s", err, string(out))
	}
	reset := exec.CommandContext(ctx, "git", "-C", localPath,
		"reset", "--hard", "FETCH_HEAD")
	if out, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset: %w\n%s", err, string(out))
	}
	return nil
}

// CleanupCloneDir removes a cloned repository directory from disk.
func CleanupCloneDir(localPath string) error {
	return os.RemoveAll(localPath)
}
