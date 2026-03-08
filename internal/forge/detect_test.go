package forge

import (
	"testing"
)

func TestDetectForge(t *testing.T) {
	tests := []struct {
		input string
		want  ForgeKind
	}{
		{"", Unknown},
		{"/home/user/repo", Unknown},
		{"./repo", Unknown},
		{"../repo", Unknown},
		{"https://github.com/foo/bar", GitHub},
		{"https://github.com/foo/bar.git", GitHub},
		{"http://github.com/foo/bar", GitHub},
		{"https://gitlab.com/foo/bar", GitLab},
		{"https://gitlab.com/group/sub/repo", GitLab},
		{"https://bitbucket.org/foo/bar", Unknown},
		{"foo/bar", GitHub},
		{"owner/repo", GitHub},
		{"single", Unknown},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := DetectForge(tc.input)
			if got != tc.want {
				t.Errorf("DetectForge(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestExtractSlug(t *testing.T) {
	tests := []struct {
		input     string
		wantSlug  string
		wantOK    bool
	}{
		{"", "", false},
		{"/home/user/repo", "", false},
		{"./repo", "", false},
		{"https://github.com/foo/bar", "foo/bar", true},
		{"https://github.com/foo/bar.git", "foo/bar", true},
		{"https://gitlab.com/group/sub/repo", "group/sub/repo", true},
		{"https://gitlab.com/group/sub/repo.git", "group/sub/repo", true},
		{"foo/bar", "foo/bar", true},
		{"foo/bar.git", "foo/bar", true},
		{"single", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			gotSlug, gotOK := ExtractSlug(tc.input)
			if gotSlug != tc.wantSlug || gotOK != tc.wantOK {
				t.Errorf("ExtractSlug(%q) = (%q, %v), want (%q, %v)",
					tc.input, gotSlug, gotOK, tc.wantSlug, tc.wantOK)
			}
		})
	}
}

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		rawURL    string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{"", "", "", false},
		{"foo/bar", "", "", false},
		{"https://bitbucket.org/foo/bar", "", "", false},
		{"https://github.com/foo/bar", "foo", "bar", true},
		{"https://github.com/foo/bar.git", "foo", "bar", true},
		{"https://gitlab.com/foo/bar", "foo", "bar", true},
		{"https://gitlab.com/group/sub/repo", "group/sub", "repo", true},
		{"https://gitlab.com/group/sub/repo.git", "group/sub", "repo", true},
	}
	for _, tc := range tests {
		t.Run(tc.rawURL, func(t *testing.T) {
			gotOwner, gotRepo, gotOK := ExtractOwnerRepo(tc.rawURL)
			if gotOwner != tc.wantOwner || gotRepo != tc.wantRepo || gotOK != tc.wantOK {
				t.Errorf("ExtractOwnerRepo(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.rawURL, gotOwner, gotRepo, gotOK,
					tc.wantOwner, tc.wantRepo, tc.wantOK)
			}
		})
	}
}

func TestIsRemote(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"/home/user/repo", false},
		{"./repo", false},
		{"../repo", false},
		{"https://github.com/foo/bar", true},
		{"https://gitlab.com/foo/bar", true},
		{"foo/bar", true},
		{"https://bitbucket.org/foo/bar", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := IsRemote(tc.input)
			if got != tc.want {
				t.Errorf("IsRemote(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCloneURL(t *testing.T) {
	tests := []struct {
		kind  ForgeKind
		slug  string
		host  string
		token string
		want  string
	}{
		{GitHub, "foo/bar", "", "", "https://github.com/foo/bar.git"},
		{GitLab, "foo/bar", "", "", "https://gitlab.com/foo/bar.git"},
		{GitHub, "foo/bar", "", "mytoken", "https://mytoken@github.com/foo/bar.git"},
		{GitLab, "foo/bar", "", "mytoken", "https://oauth2:mytoken@gitlab.com/foo/bar.git"},
		{GitLab, "grp/sub/repo", "https://gitlab.example.com", "tok", "https://oauth2:tok@gitlab.example.com/grp/sub/repo.git"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := CloneURL(tc.kind, tc.slug, tc.host, tc.token)
			if got != tc.want {
				t.Errorf("CloneURL(%v, %q, %q, %q) = %q, want %q",
					tc.kind, tc.slug, tc.host, tc.token, got, tc.want)
			}
		})
	}
}
