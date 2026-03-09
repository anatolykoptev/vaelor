package ingest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ownerRepoRe matches GitHub slugs of the form "owner/repo" or full HTTPS URLs.
var ownerRepoRe = regexp.MustCompile(`^(?:https://github\.com/)?([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+?)(?:\.git)?$`)

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
	return ownerRepoRe.MatchString(input)
}

// NormalizeSlug extracts the canonical "owner/repo" form from a slug or URL.
// Returns an error if the input does not match the expected format.
func NormalizeSlug(input string) (string, error) {
	m := ownerRepoRe.FindStringSubmatch(input)
	if m == nil {
		return "", fmt.Errorf("invalid github slug or url: %q", input)
	}
	return m[1], nil
}

// CloneRepo performs a shallow git clone of the given repository into DestDir.
// If Ref is specified, it checks out that ref after cloning.
//
// TODO: implement full clone logic with auth token injection, ref checkout,
// depth limiting, and cleanup on error.
func CloneRepo(ctx context.Context, opts CloneOpts) (*CloneResult, error) {
	slug, err := NormalizeSlug(opts.Slug)
	if err != nil {
		return nil, err
	}

	repoName := filepath.Base(slug)
	localPath := filepath.Join(opts.DestDir, strings.ReplaceAll(slug, "/", "_"))

	// Check if already cloned (cache hit).
	if _, statErr := os.Stat(localPath); statErr == nil {
		return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
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

	args := []string{"clone", "--depth=1", "--single-branch", "--filter=blob:none"}
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

// CleanupCloneDir removes a cloned repository directory from disk.
func CleanupCloneDir(localPath string) error {
	return os.RemoveAll(localPath)
}
