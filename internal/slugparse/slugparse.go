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
		_, ok := SSHHostKind(s)
		if !ok {
			return "", fmt.Errorf("invalid repo slug or url: %q", s)
		}
		colon := strings.Index(s, ":")
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

// Options controls optional behaviour of ParseWithOptions.
type Options struct {
	// AllowSubgroups, when true, accepts GitLab-style paths with more than two
	// segments (e.g. "group/subgroup/repo") and returns the full path as the
	// slug.  The strict two-segment contract is still enforced when this field
	// is false (the default, and the behaviour of Parse).
	AllowSubgroups bool
}

// SSHHostKind returns the SSH host when input starts with the "git@" SSH
// prefix, e.g. "git@gitlab.com:group/repo.git" → "gitlab.com", true.
// Returns ("", false) for any other input form.
//
// This is the single source of truth for SSH-host extraction; both
// DetectForge and stripPrefix rely on it so that extending knownSSHHosts
// propagates automatically.
func SSHHostKind(input string) (host string, ok bool) {
	if !strings.HasPrefix(input, "git@") {
		return "", false
	}
	colon := strings.Index(input, ":")
	if colon < 0 {
		return "", false
	}
	h := input[len("git@"):colon]
	if !knownSSHHosts[h] {
		return "", false
	}
	return h, true
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
//
// Use ParseWithOptions for GitLab subgroup support.
func Parse(input string) (string, error) {
	return ParseWithOptions(input, Options{})
}

// ParseWithOptions is like Parse but respects the given Options.
// With Options{AllowSubgroups: true}, paths of the form
// "group/sub1/sub2/repo" (2+ segments) are accepted and returned verbatim as
// the slug (e.g. "group/sub/repo").  The minimum of two segments is always
// required.
func ParseWithOptions(input string, opts Options) (string, error) {
	form := classifyForm(input)

	if input == "" || isLocalPath(input) {
		slugNormalizeTotal.WithLabelValues(form, "reject").Inc()
		return "", fmt.Errorf("invalid repo slug or url: %q", input)
	}

	s, err := stripPrefix(input)
	if err != nil {
		slugNormalizeTotal.WithLabelValues(form, "reject").Inc()
		return "", err
	}

	// Strip trailing .git — but reject double-suffix like owner/repo.git.git.
	s = strings.TrimSuffix(s, ".git")
	if strings.HasSuffix(s, ".git") {
		slugNormalizeTotal.WithLabelValues(form, "reject").Inc()
		return "", fmt.Errorf("invalid repo slug or url: %q", input)
	}

	parts := strings.Split(s, "/")
	minSegments := 2
	if opts.AllowSubgroups {
		if len(parts) < minSegments {
			slugNormalizeTotal.WithLabelValues(form, "reject").Inc()
			return "", fmt.Errorf("invalid repo slug or url: %q", input)
		}
	} else {
		if len(parts) != 2 {
			slugNormalizeTotal.WithLabelValues(form, "reject").Inc()
			return "", fmt.Errorf("invalid repo slug or url: %q", input)
		}
	}
	for _, seg := range parts {
		if !ownerRepoSegRe.MatchString(seg) {
			slugNormalizeTotal.WithLabelValues(form, "reject").Inc()
			return "", fmt.Errorf("invalid repo slug or url: %q", input)
		}
	}
	slugNormalizeTotal.WithLabelValues(form, "accept").Inc()
	return strings.Join(parts, "/"), nil
}
