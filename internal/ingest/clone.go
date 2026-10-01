package ingest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/credscrub"
	"github.com/anatolykoptev/vaelor/internal/slugparse"
	"golang.org/x/sync/singleflight"
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

	// GithubToken authenticates clones and refreshes (private repos, higher
	// rate limits). It is sent as an http.extraheader scoped to the clone URL's
	// host and is never placed in a URL.
	GithubToken string

	// AuthUser is the HTTP Basic username paired with the token.
	// Empty means "x-access-token" (GitHub); GitLab uses "oauth2".
	AuthUser string

	// CloneURL is a pre-built HTTPS clone URL. When non-empty, it is used
	// directly and the default github.com URL is skipped. It must not carry
	// credentials. Slug is still required for directory naming.
	CloneURL string

	// TokenFunc returns a fresh token for authenticated git operations.
	// When set, it is called before each refreshClone to obtain a current
	// installation token (ghs_, ~1h TTL). Overrides GithubToken for refreshes.
	// When nil, refreshClone falls back to GithubToken (if any).
	TokenFunc func(ctx context.Context) (string, error)
}

// CloneResult contains the result of a successful clone.
type CloneResult struct {
	// LocalPath is the absolute path to the cloned repository root.
	LocalPath string

	// Ref is the actual ref checked out (may differ if HEAD was resolved)..
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
//
// When a cached clone already exists, it is refreshed in-place via
// git fetch + reset. If the in-place refresh fails (corrupt repo, network blip,
// missing ref), a fresh clone is performed into a temporary sibling directory
// which is then atomically swapped into place (see atomicDirectorySwap).
//
// The atomic swap guarantees that concurrent readers (e.g. WalkDir +
// indexParseParallel) always see either the old snapshot or the new one —
// never a half-deleted intermediate state that causes ENOENT during file reads.
//
// Disk note: during the swap the workspace directory briefly holds both
// the old clone and the new tmp clone. If the volume is critically full
// (< 2× repo size free) the clone step may fail; the tmp directory is
// cleaned up before the error is returned.
func CloneRepo(ctx context.Context, opts CloneOpts) (*CloneResult, error) {
	// A caller whose ctx is already done gets ctx.Err() BEFORE the
	// single-flight fn launches: the cold clone runs on a decoupled ctx
	// (cloneOpTimeout), so a clone started here would keep writing into
	// DestDir for up to 10m after this call returned — racing any
	// caller-owned cleanup of DestDir (#798/#734).
	// Cancellation landing in the window between this check and the fn's
	// first statement still launches the clone — accepted: the flight
	// belongs to all same-key waiters, not to the dead caller (a per-caller
	// check inside the fn would reintroduce leader-ctx poisoning), and in
	// production DestDir is the shared workspace where a completed clone
	// stays useful to the next caller.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slug, err := NormalizeSlug(opts.Slug)
	if err != nil {
		return nil, err
	}

	repoName := filepath.Base(slug)
	localPath := filepath.Join(opts.DestDir, strings.ReplaceAll(slug, "/", "_"))

	// Cache hit — refresh to remote HEAD instead of trusting on-disk state.
	if _, statErr := os.Stat(localPath); statErr == nil {
		if err := refreshClone(ctx, localPath, opts.Ref, buildCloneURL(opts, slug), opts.AuthUser, refreshTokenFunc(opts)); err == nil {
			return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
		}
		// Refresh failed (corrupt repo, network blip, missing ref) — perform
		// an atomic re-clone so localPath is never absent for concurrent readers.
		if err := atomicReclone(ctx, opts, localPath, repoName); err != nil {
			return nil, err
		}
		return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
	}

	cloneURL := buildCloneURL(opts, slug)
	auth := gitAuthEnv(cloneURL, opts.AuthUser, opts.GithubToken)
	// Single-flight per localPath on the cold-clone path: the first caller
	// clones; concurrent callers for the SAME slug-deterministic localPath
	// await its result instead of each launching their own git clone into the
	// same directory. Pre-fix, two code_search calls on a never-cloned
	// owner/repo both entered runClone and raced into the same localPath
	// (partial trees, double fetch, one call's ReleaseCloneRef→CleanupCloneDir
	// wiping a dir another was mid-write on — #676/#677). Distinct localPaths
	// are distinct single-flight keys, so unrelated repos still clone in
	// parallel — no false serialization.
	//
	// Modeled on golang.org/x/sync/singleflight.Group.DoChan + the in-house
	// internal/goanalysis/cached_loader.go usage (which itself models on
	// canonical singleflight, the same primitive Sourcegraph's gitserver
	// lock.go prevents concurrent same-repo clones with). The shared clone
	// runs under a DECOUPLED context (context.Background()+cloneOpTimeout, not
	// any caller's ctx) so a caller giving up early returns its own ctx.Err()
	// without killing the in-flight clone for the other waiters — the
	// leader-ctx-poisoning failure goanalysis PR #294 fixed.
	//
	// Cleanup interaction with cloneRefs: runCloneFn completes the full tree
	// BEFORE any waiter receives the CloneResult (singleflight shares the
	// result only after the fn returns). Each waiter then acquires its own
	// cloneRefs refcount in workspace.RemoteSource.Root (after CloneRepo
	// returns), so the dir is removed only at refcount=0 — a waiter never
	// observes a half-written or wiped tree.
	ch := cloneGroup.DoChan(localPath, func() (any, error) {
		cloneCtx, cancel := context.WithTimeout(context.Background(), cloneOpTimeout)
		defer cancel()
		if err := os.MkdirAll(opts.DestDir, dirPerm); err != nil {
			return nil, fmt.Errorf("create dest dir: %w", err)
		}
		if err := runCloneFn(cloneCtx, cloneURL, opts.Ref, localPath, auth); err != nil {
			return nil, fmt.Errorf("git clone %s: %w", repoName, err)
		}
		return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*CloneResult), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// atomicReclone clones into a sibling tmp directory, then atomically swaps it
// into finalDest using atomicDirectorySwap (renameat2 RENAME_EXCHANGE on Linux,
// two-step rename on other platforms).
//
// On Linux: the RENAME_EXCHANGE syscall is a single atomic kernel operation —
// finalDest is never absent at any point during the swap.
//
// On other platforms: a two-step rename is used with a sub-microsecond window
// where finalDest may be absent (see clone_swap_other.go).
func atomicReclone(ctx context.Context, opts CloneOpts, finalDest, repoName string) error {
	slug, _ := NormalizeSlug(opts.Slug) // already validated by caller
	tmpDest := filepath.Join(opts.DestDir,
		strings.ReplaceAll(slug, "/", "_")+".tmp."+strconv.FormatInt(time.Now().UnixNano(), 36))

	cloneURL := buildCloneURL(opts, slug)
	auth := gitAuthEnv(cloneURL, opts.AuthUser, opts.GithubToken)
	if err := runClone(ctx, cloneURL, opts.Ref, tmpDest, auth); err != nil {
		// Clone failed — clean up the (possibly partial) tmp directory.
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("git clone %s (atomic re-clone): %w", repoName, err)
	}

	if err := atomicDirectorySwap(tmpDest, finalDest); err != nil {
		return fmt.Errorf("atomic re-clone swap: %w", err)
	}
	return nil
}

// buildCloneURL constructs the credential-free git clone URL from CloneOpts.
func buildCloneURL(opts CloneOpts, slug string) string {
	if opts.CloneURL != "" {
		return opts.CloneURL
	}
	return fmt.Sprintf("https://github.com/%s.git", slug)
}

// cloneOpTimeout bounds a single cold git clone performed under the per-
// localPath single-flight. It is applied to a DECOUPLED context
// (context.Background(), not any caller's ctx) so a caller giving up early
// cannot cancel the in-flight clone out from under other waiters — see
// CloneRepo. Matches internal/goanalysis's defaultTimeout (10m), the
// in-house convention for a shared, caller-decoupled expensive op.
const cloneOpTimeout = 10 * time.Minute

// cloneGroup coalesces concurrent cold-clone calls for the same localPath
// into a single in-flight git clone — see CloneRepo.
var cloneGroup singleflight.Group

// runCloneFn is the indirection through which the cold-clone path calls the
// real git clone. It exists purely for testability: white-box tests in this
// package swap it for a counting/barrier-controlled fake to deterministically
// exercise the per-localPath single-flight without shelling out to git — the
// same seam shape as internal/goanalysis/cached_loader.go's loadPackagesFn.
var runCloneFn = runClone

// refreshTokenFunc returns the token source for a cache-hit refresh: the
// caller's TokenFunc, else a static func over GithubToken, else nil.
func refreshTokenFunc(opts CloneOpts) func(ctx context.Context) (string, error) {
	if opts.TokenFunc != nil {
		return opts.TokenFunc
	}
	if opts.GithubToken != "" {
		tok := opts.GithubToken
		return func(context.Context) (string, error) { return tok, nil }
	}
	return nil
}

// runClone executes the git clone command into dest. authEnv is the
// credential environment from gitAuthEnv (nil for anonymous clones).
func runClone(ctx context.Context, cloneURL, ref, dest string, authEnv []string) error {
	args := []string{"clone", "--depth=2", "--single-branch", "--filter=blob:none"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, cloneURL, dest)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), authEnv...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, sanitizeGitOutput(string(out)))
	}
	return nil
}

// refreshClone updates an existing shallow clone at localPath to match
// remote origin's tip of ref (or default HEAD when ref is empty).
// When tokenFunc is non-nil it is called to obtain a fresh token; the token is
// injected via GIT_CONFIG_COUNT env variables (see gitAuthEnv) so .git/config
// is never modified. remoteURL (credential-free) scopes the credential to its
// host; user is the Basic username ("" = x-access-token).
// Returns an error if any git operation fails so the caller can wipe
// and re-clone instead of trusting potentially stale state.
func refreshClone(ctx context.Context, localPath, ref, remoteURL, user string, tokenFunc func(ctx context.Context) (string, error)) error {
	branch := ref
	if branch == "" {
		branch = "HEAD"
	}
	fetch := exec.CommandContext(ctx, "git", "-C", localPath,
		"fetch", "--depth=2", "origin", branch)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if tokenFunc != nil {
		tok, err := tokenFunc(ctx)
		if err != nil {
			return fmt.Errorf("refresh token: %w", err)
		}
		env = append(env, gitAuthEnv(remoteURL, user, tok)...)
	}
	fetch.Env = env
	if out, err := fetch.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %w\n%s", err, sanitizeGitOutput(string(out)))
	}
	reset := exec.CommandContext(ctx, "git", "-C", localPath,
		"reset", "--hard", "FETCH_HEAD")
	if out, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset: %w\n%s", err, sanitizeGitOutput(string(out)))
	}
	return nil
}

// sanitizeGitOutput removes credential material from git output before it is
// wrapped into an error: lines carrying the Authorization header injected via
// GIT_CONFIG_VALUE_0, then any URL userinfo or token-shaped string.
func sanitizeGitOutput(s string) string {
	lines := strings.Split(s, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "Authorization:") || strings.Contains(line, "extraheader") {
			continue
		}
		filtered = append(filtered, line)
	}
	return credscrub.Scrub(strings.Join(filtered, "\n"))
}

// CleanupCloneDir removes a cloned repository directory from disk.
func CleanupCloneDir(localPath string) error {
	return os.RemoveAll(localPath)
}
