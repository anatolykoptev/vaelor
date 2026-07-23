package main

import (
	"reflect"
	"testing"
)

// TestResolveQuickRepos_DefaultsFallback proves the GithubSearchRepos config
// field (GITHUB_SEARCH_REPOS env var) is honored: when the caller supplies
// neither Repo nor Repos, resolveQuickRepos falls back to the configured
// default repos instead of returning nil (which previously made quick mode
// error with "repo or repos is required").
//
// Falsification: revert the defaultRepos fallback in resolveQuickRepos →
// the no-input case returns nil again → the assertion `got == defaults`
// goes RED.
func TestResolveQuickRepos_DefaultsFallback(t *testing.T) {
	defaults := []string{"owner/default1", "owner/default2"}

	// No Repo, no Repos → fall back to defaults.
	got := resolveQuickRepos(RepoAnalyzeInput{}, defaults)
	if !reflect.DeepEqual(got, defaults) {
		t.Errorf("no input: got %v, want defaults %v", got, defaults)
	}

	// Explicit Repos wins over defaults.
	got = resolveQuickRepos(RepoAnalyzeInput{Repos: []string{"a/b"}}, defaults)
	if want := []string{"a/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("explicit Repos: got %v, want %v", got, want)
	}

	// Explicit Repo wins over defaults.
	got = resolveQuickRepos(RepoAnalyzeInput{Repo: "owner/explicit"}, defaults)
	if want := []string{"owner/explicit"}; !reflect.DeepEqual(got, want) {
		t.Errorf("explicit Repo: got %v, want %v", got, want)
	}

	// No defaults configured and no input → nil (preserves prior behavior).
	got = resolveQuickRepos(RepoAnalyzeInput{}, nil)
	if got != nil {
		t.Errorf("no input, no defaults: got %v, want nil", got)
	}
}
