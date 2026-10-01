package forge

import (
	"net/url"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/slugparse"
)

// DetectForge infers the ForgeKind from a repository identifier.
//
// Rules (evaluated in order):
//  1. Empty string → Unknown.
//  2. Local path prefix ("/", "./", "../") → Unknown.
//  3. URL with host github.com → GitHub.
//  4. URL with host gitlab.com → GitLab.
//  5. Other URL → Unknown.
//  6. SSH form git@github.com:… → GitHub.
//  7. SSH form git@gitlab.com:… → GitLab.
//  8. Bare slug with host prefix (github.com/… or gitlab.com/…) → appropriate forge.
//  9. Bare owner/repo slug (no host prefix) → GitHub (default forge).
func DetectForge(input string) ForgeKind {
	if input == "" {
		return Unknown
	}
	if isLocalPath(input) {
		return Unknown
	}
	// URL with explicit scheme — use hostname.
	if u, err := url.Parse(input); err == nil && u.Scheme != "" {
		return kindFromHost(u.Hostname())
	}
	// SSH form: git@<host>:owner/repo[.git] — use slugparse as single source of truth.
	if strings.HasPrefix(input, "git@") {
		host, ok := slugparse.SSHHostKind(input)
		if !ok {
			return Unknown
		}
		return kindFromHost(host)
	}
	// Bare slug, possibly with forge-host prefix.
	if strings.Contains(input, "/") && !strings.Contains(input, " ") {
		s := input
		for _, pfx := range []string{"github.com/", "gitlab.com/"} {
			if strings.HasPrefix(s, pfx) {
				host := strings.TrimSuffix(pfx, "/")
				return kindFromHost(host)
			}
		}
		// Plain owner/repo — default to GitHub.
		parts := strings.Split(strings.Trim(s, "/"), "/")
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return GitHub
		}
	}
	return Unknown
}

// ExtractSlug returns the canonical slug from any of the standard repository
// identifier forms (bare slug, HTTPS URL, SSH URL, bare host-prefix form).
// Returns ("", false) for any input that cannot be parsed as a valid slug.
//
// GitLab subgroup paths (e.g. "group/sub/repo") are accepted and returned as
// the full slug ("group/sub/repo").  For strict two-segment enforcement use
// slugparse.Parse directly.
func ExtractSlug(input string) (string, bool) {
	kind := DetectForge(input)
	slug, err := slugparse.ParseWithOptions(input, slugparse.Options{AllowSubgroups: true})
	if err != nil {
		forgeResolveTotal.WithLabelValues(forgeLabel(kind), resolveOutcome(input)).Inc()
		return "", false
	}
	forgeResolveTotal.WithLabelValues(forgeLabel(kind), "success").Inc()
	return slug, true
}

// ExtractOwnerRepo parses a full repository URL and splits it into owner and
// repo components. Only github.com and gitlab.com hosts are accepted.
//
// GitLab nested groups are supported: "group/sub/repo" yields
// owner="group/sub", repo="repo".
func ExtractOwnerRepo(rawURL string) (owner, repo string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return "", "", false
	}
	if kindFromHost(u.Hostname()) == Unknown {
		return "", "", false
	}

	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.Split(path, "/")
	var clean []string
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) < 2 {
		return "", "", false
	}

	repo = clean[len(clean)-1]
	owner = strings.Join(clean[:len(clean)-1], "/")
	return owner, repo, true
}

// IsRemote reports whether input refers to a remote repository (i.e. it is
// not empty and not a local file-system path).
func IsRemote(input string) bool {
	if input == "" || isLocalPath(input) {
		return false
	}
	return DetectForge(input) != Unknown
}

// CloneURL constructs the credential-free HTTPS clone URL for a repository.
//
//   - host: optional custom base URL (e.g. "https://gitlab.example.com").
//     When empty the canonical host for the forge kind is used.
//
// Credentials are never embedded in the URL (git echoes URLs in its error
// output); callers pass the token separately, see CloneAuthUser.
func CloneURL(kind ForgeKind, slug, host string) string {
	if host == "" {
		switch kind {
		case GitHub:
			host = "https://github.com"
		case GitLab:
			host = "https://gitlab.com"
		}
	}
	return host + "/" + slug + ".git"
}

// CloneAuthUser returns the HTTP Basic username paired with an access token
// for kind. Empty means the clone default (GitHub's "x-access-token").
func CloneAuthUser(kind ForgeKind) string {
	if kind == GitLab {
		return "oauth2"
	}
	return ""
}

// isLocalPath reports whether input is a local file-system path.
func isLocalPath(input string) bool {
	return strings.HasPrefix(input, "/") ||
		strings.HasPrefix(input, "./") ||
		strings.HasPrefix(input, "../")
}

// kindFromHost maps a hostname to a ForgeKind.
func kindFromHost(host string) ForgeKind {
	switch host {
	case "github.com":
		return GitHub
	case "gitlab.com":
		return GitLab
	default:
		return Unknown
	}
}
