package forge

import (
	"strings"
	"testing"
)

func TestBuildGitHubCodeSearchURL(t *testing.T) {
	t.Parallel()
	u := buildGitHubCodeSearchURL("https://api.github.com", "foo repo:bar/baz", "indexed", "desc", 25, 2)
	if !strings.Contains(u, "q=foo+repo%3Abar%2Fbaz") {
		t.Errorf("URL missing escaped query: %s", u)
	}
	if !strings.Contains(u, "sort=indexed") {
		t.Errorf("URL missing sort: %s", u)
	}
	if !strings.Contains(u, "order=desc") {
		t.Errorf("URL missing order: %s", u)
	}
	if !strings.Contains(u, "per_page=25") {
		t.Errorf("URL missing per_page: %s", u)
	}
	if !strings.Contains(u, "page=2") {
		t.Errorf("URL missing page: %s", u)
	}
}

func TestBuildGitHubCodeSearchQuery(t *testing.T) {
	t.Parallel()
	q, err := buildGitHubCodeSearchQuery("func main", []string{"foo/bar"}, []string{"baz/qux"}, []string{"go"}, "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(q, "func main") {
		t.Errorf("query missing base: %s", q)
	}
	if !strings.Contains(q, "repo:foo/bar") {
		t.Errorf("query missing included repo: %s", q)
	}
	if !strings.Contains(q, "-repo:baz/qux") {
		t.Errorf("query missing excluded repo: %s", q)
	}
	if !strings.Contains(q, "extension:go") {
		t.Errorf("query missing extension: %s", q)
	}
	if !strings.Contains(q, "language:go") {
		t.Errorf("query missing language: %s", q)
	}
}

func TestResolveSearchParams(t *testing.T) {
	t.Parallel()
	perPage, maxResults := resolveSearchParams(0, SearchCodeOptions{MaxResults: 10})
	if perPage != 10 || maxResults != 10 {
		t.Errorf("resolveSearchParams(0, {MaxResults:10}) = (%d, %d), want (10, 10)", perPage, maxResults)
	}

	perPage, maxResults = resolveSearchParams(0, SearchCodeOptions{MaxResults: 2000, MinStars: 50})
	if perPage != 100 || maxResults != 100 {
		t.Errorf("resolveSearchParams min_stars cap = (%d, %d), want (100, 100)", perPage, maxResults)
	}
}
