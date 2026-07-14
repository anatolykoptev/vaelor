package forge

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultPerPage         = 10
	maxResultsMax          = 1000
	maxResultsWithMinStars = 100
)

// ownerRepoSegRe validates a single GitHub owner or repo path segment.
var ownerRepoSegRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// normalizeGitHubRepo converts a variety of GitHub repository identifier forms
// to the canonical "owner/repo" slug.
func normalizeGitHubRepo(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("empty repo identifier")
	}
	if isLocalPath(input) {
		return "", fmt.Errorf("invalid repo identifier: %q is a local path", input)
	}

	// SSH form: git@github.com:owner/repo[.git]
	if strings.HasPrefix(input, "git@") {
		if !strings.HasPrefix(input, "git@github.com:") {
			return "", fmt.Errorf("invalid repo identifier: %q", input)
		}
		s := input[len("git@github.com:"):]
		return normalizeGitHubSlug(s)
	}

	// Strip scheme if present.
	for _, pfx := range []string{"https://", "http://"} {
		if strings.HasPrefix(input, pfx) {
			input = input[len(pfx):]
			break
		}
	}

	// Strip known forge host prefix.
	if strings.HasPrefix(input, "github.com/") {
		input = input[len("github.com/"):]
		return normalizeGitHubSlug(input)
	}

	// Bare owner/repo.
	return normalizeGitHubSlug(input)
}

// normalizeGitHubSlug validates and returns a clean owner/repo slug.
func normalizeGitHubSlug(s string) (string, error) {
	s = strings.Trim(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if strings.HasSuffix(s, ".git") {
		return "", fmt.Errorf("invalid repo slug: %q", s)
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo slug: %q", s)
	}
	for _, p := range parts {
		if !ownerRepoSegRe.MatchString(p) {
			return "", fmt.Errorf("invalid repo slug: %q", s)
		}
	}
	return strings.Join(parts, "/"), nil
}

// normalizeRepoSet normalizes a slice of repo identifiers into a set.
func normalizeRepoSet(repos []string) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		normalized, err := normalizeGitHubRepo(r)
		if err != nil {
			return nil, fmt.Errorf("invalid repo %q: %w", r, err)
		}
		set[normalized] = struct{}{}
	}
	return set, nil
}

// buildGitHubCodeSearchQuery constructs the q parameter for the GitHub Code
// Search API, appending repo:/language:/extension:/-repo: qualifiers only when
// they are not already present.
func buildGitHubCodeSearchQuery(query string, repos, excludeRepos, fileExtensions []string, language string) (string, error) {
	excluded, err := normalizeRepoSet(excludeRepos)
	if err != nil {
		return "", err
	}

	q, err := addRepoQualifiers(query, repos, excluded)
	if err != nil {
		return "", err
	}

	q = addNegativeRepoQualifiers(q, excluded)
	q = addExtensionQualifiers(q, fileExtensions)
	q = addLanguageQualifier(q, language)
	return q, nil
}

// addRepoQualifiers appends repo: qualifiers for included repos, skipping excluded ones.
func addRepoQualifiers(q string, repos []string, excluded map[string]struct{}) (string, error) {
	for _, r := range repos {
		normalized, err := normalizeGitHubRepo(r)
		if err != nil {
			return "", fmt.Errorf("invalid repo %q: %w", r, err)
		}
		if _, ok := excluded[normalized]; ok {
			continue
		}
		if hasRepoQualifier(q, normalized) {
			continue
		}
		q = appendQualifier(q, "repo", normalized)
	}
	return q, nil
}

// addNegativeRepoQualifiers appends -repo: qualifiers for excluded repos.
func addNegativeRepoQualifiers(q string, excluded map[string]struct{}) string {
	for r := range excluded {
		if hasNegativeRepoQualifier(q, r) {
			continue
		}
		q = appendQualifier(q, "-repo", r)
	}
	return q
}

// addExtensionQualifiers appends extension: qualifiers for file extensions.
func addExtensionQualifiers(q string, fileExtensions []string) string {
	for _, ext := range fileExtensions {
		ext = strings.TrimPrefix(ext, ".")
		ext = strings.ToLower(ext)
		if ext == "" {
			continue
		}
		if hasExtensionQualifier(q, ext) {
			continue
		}
		q = appendQualifier(q, "extension", ext)
	}
	return q
}

// addLanguageQualifier appends a language: qualifier if not already present.
func addLanguageQualifier(q, language string) string {
	if language != "" && !hasLanguageQualifier(q, language) {
		q = appendQualifier(q, "language", language)
	}
	return q
}

// appendQualifier appends a qualifier to a query string, handling empty query.
func appendQualifier(q, qualifier, value string) string {
	if q == "" {
		return qualifier + ":" + value
	}
	return q + " " + qualifier + ":" + value
}

// hasQualifier reports whether query already contains a qualifier: value for
// the given qualifier and value (case-insensitive). It does not match negated
// qualifiers.
func hasQualifier(query, qualifier, value string) bool {
	pattern := `(?i)(?:^|[^-\w])` + regexp.QuoteMeta(qualifier) + `:\s*"?([^"\s]+)`
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	want := strings.ToLower(value)
	for _, m := range re.FindAllStringSubmatch(query, -1) {
		got := strings.Trim(m[1], `"`)
		if strings.ToLower(got) == want {
			return true
		}
	}
	return false
}

// hasRepoQualifier reports whether query already contains a repo: qualifier
// for the given canonical owner/repo.
func hasRepoQualifier(query, repo string) bool {
	return hasQualifier(query, "repo", repo)
}

// hasNegativeRepoQualifier reports whether query already contains a -repo:
// qualifier for the given repo.
func hasNegativeRepoQualifier(query, repo string) bool {
	if repo == "" {
		return false
	}
	want := strings.ToLower(repo)
	q := " " + strings.ToLower(query) + " "
	return strings.Contains(q, " -repo:"+want+" ") ||
		strings.Contains(q, " -repo:\""+want+"\"")
}

// hasExtensionQualifier reports whether query already contains an extension:
// or ext: qualifier for the given extension.
func hasExtensionQualifier(query, ext string) bool {
	return hasQualifier(query, "extension", ext) || hasQualifier(query, "ext", ext)
}

// hasLanguageQualifier reports whether query already contains a language:
// qualifier for the given language.
func hasLanguageQualifier(query, language string) bool {
	return hasQualifier(query, "language", language)
}

// validateGitHubCodeSearchSortOrder validates sort/order for the GitHub Code
// Search API. Code search supports only sort=indexed.
func validateGitHubCodeSearchSortOrder(sort, order string) (string, string, error) {
	if sort != "" && sort != "indexed" {
		return "", "", fmt.Errorf("invalid sort %q: code search supports only 'indexed'", sort)
	}
	order = strings.ToLower(order)
	if order != "" && order != "asc" && order != "desc" {
		return "", "", fmt.Errorf("invalid order %q: must be 'asc' or 'desc'", order)
	}
	if order != "" && sort == "" {
		return "", "", fmt.Errorf("order %q requires sort to be set", order)
	}
	return sort, order, nil
}

// normalizePerPage clamps per_page to the GitHub-allowed range 1..100.
func normalizePerPage(perPage int) int {
	if perPage <= 0 {
		return defaultPerPage
	}
	if perPage > 100 {
		return 100
	}
	return perPage
}

// normalizePage ensures GitHub API pages start at 1.
func normalizePage(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

// resolveSearchParams applies perPage normalization and maxResults/minStars overrides.
func resolveSearchParams(perPage int, opt SearchCodeOptions) (int, int) {
	perPage = normalizePerPage(perPage)

	maxResults := opt.MaxResults
	if maxResults > 0 {
		if maxResults > maxResultsMax {
			maxResults = maxResultsMax
		}
		if opt.MinStars > 0 && maxResults > maxResultsWithMinStars {
			maxResults = maxResultsWithMinStars
		}
		if maxResults < perPage {
			perPage = maxResults
		} else if perPage < 100 {
			perPage = min(maxResults, 100)
		}
	}

	if opt.MinStars > 0 && perPage == defaultPerPage {
		perPage = 100
	}

	return perPage, maxResults
}

// buildGitHubCodeSearchURL builds the GitHub Code Search API URL with the given parameters.
func buildGitHubCodeSearchURL(apiBase, q, sort, order string, perPage, page int) string {
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	apiBase = strings.TrimSuffix(apiBase, "/")
	values := url.Values{
		"q":        {q},
		"per_page": {strconv.Itoa(perPage)},
		"page":     {strconv.Itoa(page)},
	}
	if sort != "" {
		values.Set("sort", sort)
	}
	if order != "" {
		values.Set("order", order)
	}
	return apiBase + "/search/code?" + values.Encode()
}
