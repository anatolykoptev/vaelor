package forge

import (
	"net/url"
	"strings"
)

// DetectForge infers the ForgeKind from a repository identifier.
//
// Rules (evaluated in order):
//  1. Empty string → Unknown.
//  2. Local path prefix ("/", "./", "../") → Unknown.
//  3. URL with host github.com → GitHub.
//  4. URL with host gitlab.com → GitLab.
//  5. Other URL → Unknown.
//  6. Bare slug with 2+ "/"-separated parts → GitHub (default forge).
func DetectForge(input string) ForgeKind {
	if input == "" {
		return Unknown
	}
	if isLocalPath(input) {
		return Unknown
	}
	if u, err := url.Parse(input); err == nil && u.Scheme != "" {
		return kindFromHost(u.Hostname())
	}
	// Bare slug: must contain at least one "/" and no spaces.
	if strings.Contains(input, "/") && !strings.Contains(input, " ") {
		parts := strings.Split(strings.Trim(input, "/"), "/")
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return GitHub
		}
	}
	return Unknown
}

// ExtractSlug returns the "owner/repo[/sub…]" portion of a repository
// identifier. Returns ("", false) for local paths, empty input, or inputs
// that do not contain at least two path segments.
func ExtractSlug(input string) (string, bool) {
	if input == "" || isLocalPath(input) {
		return "", false
	}

	var rawPath string
	if u, err := url.Parse(input); err == nil && u.Scheme != "" {
		rawPath = u.Path
	} else {
		rawPath = input
	}

	rawPath = strings.Trim(rawPath, "/")
	rawPath = strings.TrimSuffix(rawPath, ".git")

	parts := strings.Split(rawPath, "/")
	// Filter empty segments that can appear after trimming.
	var clean []string
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) < 2 {
		return "", false
	}
	return strings.Join(clean, "/"), true
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

// CloneURL constructs the HTTPS clone URL for a repository.
//
//   - host: optional custom base URL (e.g. "https://gitlab.example.com").
//     When empty the canonical host for the forge kind is used.
//   - token: optional authentication token.
//     GitHub → "https://{token}@{host}/{slug}.git"
//     GitLab → "https://oauth2:{token}@{host}/{slug}.git"
func CloneURL(kind ForgeKind, slug, host, token string) string {
	if host == "" {
		switch kind {
		case GitHub:
			host = "https://github.com"
		case GitLab:
			host = "https://gitlab.com"
		}
	}

	if token == "" {
		return host + "/" + slug + ".git"
	}

	// Strip scheme so we can embed credentials in the authority.
	hostNoScheme := strings.TrimPrefix(host, "https://")
	hostNoScheme = strings.TrimPrefix(hostNoScheme, "http://")

	switch kind {
	case GitHub:
		return "https://" + token + "@" + hostNoScheme + "/" + slug + ".git"
	case GitLab:
		return "https://oauth2:" + token + "@" + hostNoScheme + "/" + slug + ".git"
	default:
		return host + "/" + slug + ".git"
	}
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
