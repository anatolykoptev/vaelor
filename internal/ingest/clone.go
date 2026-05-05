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

// ownerRepoSegRe validates a single owner or repo segment: alphanumeric, dash, underscore, dot.
var ownerRepoSegRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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
	_, err := NormalizeSlug(input)
	return err == nil
}

// NormalizeSlug extracts the canonical "owner/repo" form from any of:
//   - owner/repo
//   - github.com/owner/repo[.git]
//   - https?://github.com/owner/repo[.git]
//   - git@github.com:owner/repo[.git]
//
// Returns an error if the input does not match any recognised form.
func NormalizeSlug(input string) (string, error) {
	s := input

	// Strip SSH form: git@github.com:owner/repo[.git]
	if strings.HasPrefix(s, "git@") {
		colon := strings.Index(s, ":")
		if colon < 0 {
			return "", fmt.Errorf("invalid github slug or url: %q", input)
		}
		host := s[len("git@"):colon]
		if host != "github.com" && host != "gitlab.com" {
			return "", fmt.Errorf("invalid github slug or url: %q", input)
		}
		s = s[colon+1:]
	} else {
		// Strip scheme.
		for _, pfx := range []string{"https://", "http://"} {
			if strings.HasPrefix(s, pfx) {
				s = s[len(pfx):]
				break
			}
		}
		// Strip known forge hosts.
		for _, host := range []string{"github.com/", "gitlab.com/"} {
			if strings.HasPrefix(s, host) {
				s = s[len(host):]
				break
			}
		}
	}

	// Strip trailing .git — but reject double-suffix like owner/repo.git.git.
	s = strings.TrimSuffix(s, ".git")
	if strings.HasSuffix(s, ".git") {
		return "", fmt.Errorf("invalid github slug or url: %q", input)
	}

	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid github slug or url: %q", input)
	}
	owner, repo := parts[0], parts[1]
	if !ownerRepoSegRe.MatchString(owner) || !ownerRepoSegRe.MatchString(repo) {
		return "", fmt.Errorf("invalid github slug or url: %q", input)
	}
	return owner + "/" + repo, nil
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
		if err := refreshClone(ctx, localPath, opts.Ref); err == nil {
			return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
		}
		// Refresh failed (corrupt repo, network blip, missing ref) — wipe
		// and fall through to a fresh shallow clone below.
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


// refreshClone updates an existing shallow clone at localPath to match
// remote origin's tip of ref (or default HEAD when ref is empty).
// Returns an error if any git operation fails so the caller can wipe
// and re-clone instead of trusting potentially stale state.
func refreshClone(ctx context.Context, localPath, ref string) error {
	branch := ref
	if branch == "" {
		branch = "HEAD"
	}
	fetch := exec.CommandContext(ctx, "git", "-C", localPath,
		"fetch", "--depth=1", "origin", branch)
	fetch.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
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
