// Package forge defines the Forge interface and shared types for source-code
// hosting integrations (GitHub, GitLab, …).
//
// Concrete implementations live in sub-packages such as internal/github.
package forge

import "context"

// ForgeKind identifies the type of a forge.
type ForgeKind int

const (
	// Unknown is returned when the forge type cannot be determined.
	Unknown ForgeKind = iota
	// GitHub represents github.com.
	GitHub
	// GitLab represents gitlab.com or a self-hosted GitLab instance.
	GitLab
)

// String returns the lowercase name of the forge kind.
func (k ForgeKind) String() string {
	switch k {
	case GitHub:
		return "github"
	case GitLab:
		return "gitlab"
	default:
		return "unknown"
	}
}

// RepoMeta contains key metadata about a repository.
// JSON tags are present so implementations can decode API responses directly
// into this type.
type RepoMeta struct {
	// FullName is the "owner/repo" slug.
	FullName string `json:"full_name"`

	// Description is the repository description.
	Description string `json:"description"`

	// DefaultBranch is the default branch name.
	DefaultBranch string `json:"default_branch"`

	// Language is the primary programming language.
	Language string `json:"language"`

	// Stars is the stargazer count.
	Stars int `json:"stargazers_count"`

	// Forks is the fork count.
	Forks int `json:"forks_count"`

	// CloneURL is the HTTPS clone URL.
	CloneURL string `json:"clone_url"`

	// Private indicates whether the repository is private.
	Private bool `json:"private"`

	// Size is the approximate disk size in kilobytes.
	Size int `json:"size"`
}

// CodeResult represents a single result from a code search.
type CodeResult struct {
	Name    string
	Path    string
	URL     string
	Repo    string
	Content string // joined text-match fragments
}

// IssueItem represents an issue or pull request from a forge search.
type IssueItem struct {
	Number    int
	Title     string
	URL       string
	State     string
	Author    string
	Labels    []string
	Body      string
	Comments  int
	CreatedAt string
	MergedAt  string
	Repo      string
}

// RepoSearchResult represents a repository returned by a forge search.
type RepoSearchResult struct {
	FullName    string
	Description string
	Stars       int
	Language    string
	Topics      []string
	LastPush    string
	Archived    bool
	HTMLURL     string
}

// Forge is the common interface that every forge implementation must satisfy.
type Forge interface {
	// Kind returns the ForgeKind for this implementation.
	Kind() ForgeKind

	// FetchRepoMeta returns metadata for the repository identified by slug
	// ("owner/repo" or a full URL).
	FetchRepoMeta(ctx context.Context, slug string) (*RepoMeta, error)

	// FetchREADME returns the raw README content for the repository.
	// Implementations should return an empty string (not an error) when no
	// README exists.
	FetchREADME(ctx context.Context, slug string) (string, error)

	// SearchCode searches for code matching query, optionally restricted to
	// the given repos ("owner/repo" slugs).
	SearchCode(ctx context.Context, query string, repos []string) ([]CodeResult, error)

	// SearchIssues searches issues and pull requests matching query.
	SearchIssues(ctx context.Context, query string) ([]IssueItem, error)

	// SearchRepos searches for repositories matching query.
	// sort may be forge-specific (e.g. "stars", "forks", "updated") or empty
	// for relevance ordering.
	SearchRepos(ctx context.Context, query, sort string) ([]RepoSearchResult, error)
}
