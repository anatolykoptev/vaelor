package argnorm

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResolveToolName_GithubRepoSearchAlias(t *testing.T) {
	canon, aliased := ResolveToolName("github_repo_search")
	if !aliased || canon != "github_code_search" {
		t.Errorf("github_repo_search → github_code_search, got %q (aliased=%v)", canon, aliased)
	}
}

func TestResolveToolName_RealToolUnchanged(t *testing.T) {
	canon, aliased := ResolveToolName("code_search")
	if aliased || canon != "code_search" {
		t.Errorf("real tool should pass through, got %q (aliased=%v)", canon, aliased)
	}
}

func TestDidYouMean_GithubRepoSearch(t *testing.T) {
	candidates := []string{"code_search", "github_code_search", "repo_search", "semantic_search"}
	got := DidYouMean("github_repo_search", candidates, maxDidYouMean)
	if len(got) == 0 {
		t.Fatalf("expected suggestions, got none")
	}
	if got[0] != "github_code_search" {
		t.Errorf("first suggestion should be github_code_search, got %v", got)
	}
}

func TestDidYouMean_FindBugsHint(t *testing.T) {
	// find_bugs has an explicit hint → debug_investigate forced to front.
	candidates := []string{"debug_investigate", "code_health", "dead_code"}
	got := DidYouMean("find_bugs", candidates, maxDidYouMean)
	if len(got) == 0 || got[0] != "debug_investigate" {
		t.Errorf("find_bugs should hint debug_investigate first, got %v", got)
	}
}

func TestDidYouMean_FlakyTestsHint(t *testing.T) {
	candidates := []string{"debug_investigate", "code_health", "dead_code"}
	got := DidYouMean("flaky_tests", candidates, maxDidYouMean)
	if len(got) == 0 || got[0] != "debug_investigate" {
		t.Errorf("flaky_tests should hint debug_investigate first, got %v", got)
	}
}

func TestDidYouMean_TestReliabilityHint(t *testing.T) {
	candidates := []string{"debug_investigate", "code_health", "dead_code"}
	got := DidYouMean("test_reliability", candidates, maxDidYouMean)
	if len(got) == 0 || got[0] != "code_health" {
		t.Errorf("test_reliability should hint code_health first, got %v", got)
	}
}

func TestDidYouMean_RespectsMaxSuggest(t *testing.T) {
	candidates := []string{"code_search", "repo_search", "semantic_search", "symbol_search"}
	got := DidYouMean("search", candidates, 2)
	if len(got) > 2 {
		t.Errorf("expected at most 2 suggestions, got %d (%v)", len(got), got)
	}
}

func TestDidYouMean_NoDistantNoise(t *testing.T) {
	// A name with no close match and no prefix relation should yield nothing
	// (or only an explicit hint), not random distant tools.
	candidates := []string{"code_search", "repo_search", "semantic_search"}
	got := DidYouMean("zzzzzzzzzz", candidates, 3)
	for _, s := range got {
		// Anything returned must either be a prefix/contains match or close.
		// "zzzzzzzzzz" has no relation to any candidate → expect empty.
		t.Errorf("expected no suggestions for unrelated name, got %q", s)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"abc", "", 3},
		{"same", "same", 0},
		{"Repo_Search", "repo_search", 0}, // case-insensitive
		{"github_repo_search", "github_code_search", 4},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDidYouMeanResult_MessageFormat(t *testing.T) {
	reg := NewRegistry()
	reg.Register("github_code_search", []string{"query"})
	reg.Register("repo_search", []string{"query"})
	r := didYouMeanResult("github_repo_search", reg.Names())
	if !r.IsError {
		t.Errorf("did-you-mean result must be an error")
	}
	txt := r.Content[0].(*mcp.TextContent).Text
	if !strings.HasPrefix(txt, `unknown tool "github_repo_search"`) {
		t.Errorf("message must start with unknown tool prefix: %q", txt)
	}
	if !strings.Contains(txt, "did you mean") {
		t.Errorf("message must contain did-you-mean: %q", txt)
	}
	if !strings.Contains(txt, "github_code_search") {
		t.Errorf("message must suggest github_code_search: %q", txt)
	}
}
