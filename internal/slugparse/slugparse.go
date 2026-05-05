// Package slugparse provides the canonical parser for source-code repository
// slugs.  It is a leaf package with no dependencies on other internal packages
// and is consumed by both internal/ingest and internal/forge.
package slugparse

import (
	"fmt"
	"regexp"
	"strings"
)

// ownerRepoSegRe validates a single owner or repo path segment.
// Segments may contain alphanumerics, dashes, underscores, and dots.
var ownerRepoSegRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// knownSSHHosts is the allowlist for SSH-form inputs (git@<host>:…).
var knownSSHHosts = map[string]bool{
	"github.com": true,
	"gitlab.com": true,
}

// knownForgeHosts is the allowlist for bare-host prefixes (host/owner/repo).
var knownForgeHosts = []string{
	"github.com/",
	"gitlab.com/",
}

// isLocalPath reports whether s is a local filesystem path.
func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../")
}

// stripPrefix removes the scheme, forge-host, or SSH prefix from s, returning
// the raw "owner/repo[.git]" path segment.
func stripPrefix(s string) (string, error) {
	// SSH form: git@<host>:owner/repo[.git]
	if strings.HasPrefix(s, "git@") {
		colon := strings.Index(s, ":")
		if colon < 0 {
			return "", fmt.Errorf("invalid repo slug or url: %q", s)
		}
		host := s[len("git@"):colon]
		if !knownSSHHosts[host] {
			return "", fmt.Errorf("invalid repo slug or url: %q", s)
		}
		return s[colon+1:], nil
	}
	// Strip scheme (https:// or http://).
	for _, pfx := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, pfx) {
			s = s[len(pfx):]
			break
		}
	}
	// Strip known forge host prefix (e.g. "github.com/").
	for _, host := range knownForgeHosts {
		if strings.HasPrefix(s, host) {
			return s[len(host):], nil
		}
	}
	return s, nil
}

// Parse extracts the canonical "owner/repo" slug from any of the following
// input forms:
//
//   - owner/repo
//   - github.com/owner/repo[.git]
//   - gitlab.com/owner/repo[.git]
//   - https://github.com/owner/repo[.git]
//   - https://gitlab.com/owner/repo[.git]
//   - http://github.com/owner/repo[.git]
//   - git@github.com:owner/repo[.git]
//   - git@gitlab.com:owner/repo[.git]
//
// It returns an error for:
//   - Local path prefixes ("/", "./", "../")
//   - SSH inputs with an unknown host (security: rejects git@evil.com:…)
//   - Double-suffix inputs (owner/repo.git.git)
//   - Inputs with wrong segment count (owner alone, owner/repo/extra)
//   - Segments that don't match [A-Za-z0-9._-]+
func Parse(input string) (string, error) {
	if input == "" || isLocalPath(input) {
		return "", fmt.Errorf("invalid repo slug or url: %q", input)
	}

	s, err := stripPrefix(input)
	if err != nil {
		return "", err
	}

	// Strip trailing .git — but reject double-suffix like owner/repo.git.git.
	s = strings.TrimSuffix(s, ".git")
	if strings.HasSuffix(s, ".git") {
		return "", fmt.Errorf("invalid repo slug or url: %q", input)
	}

	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo slug or url: %q", input)
	}
	owner, repo := parts[0], parts[1]
	if !ownerRepoSegRe.MatchString(owner) || !ownerRepoSegRe.MatchString(repo) {
		return "", fmt.Errorf("invalid repo slug or url: %q", input)
	}
	return owner + "/" + repo, nil
}
