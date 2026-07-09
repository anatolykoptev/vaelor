package forge

import (
	"testing"
)

func TestDetectForge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  ForgeKind
	}{
		{"", Unknown},
		{"/home/user/repo", Unknown},
		{"./repo", Unknown},
		{"../repo", Unknown},
		// HTTPS URL forms.
		{"https://github.com/foo/bar", GitHub},
		{"https://github.com/foo/bar.git", GitHub},
		{"http://github.com/foo/bar", GitHub},
		{"https://gitlab.com/foo/bar", GitLab},
		{"https://gitlab.com/group/sub/repo", GitLab},
		{"https://bitbucket.org/foo/bar", Unknown},
		// SSH forms.
		{"git@github.com:owner/repo.git", GitHub},
		{"git@gitlab.com:owner/repo.git", GitLab},
		{"git@evil.com:owner/repo.git", Unknown},
		// Bare slug with host prefix.
		{"github.com/owner/repo", GitHub},
		{"github.com/owner/repo.git", GitHub},
		{"gitlab.com/owner/repo", GitLab},
		// Plain bare slug.
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
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		wantSlug string
		wantOK   bool
	}{
		// Empty / local paths → rejected.
		{name: "empty", input: "", wantSlug: "", wantOK: false},
		{name: "absolute path", input: "/home/user/repo", wantSlug: "", wantOK: false},
		{name: "relative path ./", input: "./repo", wantSlug: "", wantOK: false},
		// HTTPS URL forms.
		{name: "https github no .git", input: "https://github.com/foo/bar", wantSlug: "foo/bar", wantOK: true},
		{name: "https github with .git", input: "https://github.com/foo/bar.git", wantSlug: "foo/bar", wantOK: true},
		{name: "http github", input: "http://github.com/owner/repo", wantSlug: "owner/repo", wantOK: true},
		{name: "https gitlab no .git", input: "https://gitlab.com/owner/repo", wantSlug: "owner/repo", wantOK: true},
		{name: "https gitlab with .git", input: "https://gitlab.com/owner/repo.git", wantSlug: "owner/repo", wantOK: true},
		// SSH forms.
		{name: "ssh github", input: "git@github.com:owner/repo.git", wantSlug: "owner/repo", wantOK: true},
		{name: "ssh gitlab", input: "git@gitlab.com:owner/repo.git", wantSlug: "owner/repo", wantOK: true},
		{name: "ssh unknown host rejected", input: "git@evil.com:owner/repo.git", wantSlug: "", wantOK: false},
		// Bare host-prefix forms (the primary regression trigger).
		{name: "github.com/owner/repo", input: "github.com/owner/repo", wantSlug: "owner/repo", wantOK: true},
		{name: "github.com/owner/repo.git", input: "github.com/owner/repo.git", wantSlug: "owner/repo", wantOK: true},
		{name: "gitlab.com/owner/repo", input: "gitlab.com/owner/repo", wantSlug: "owner/repo", wantOK: true},
		{name: "gitlab.com/owner/repo.git", input: "gitlab.com/owner/repo.git", wantSlug: "owner/repo", wantOK: true},
		// Plain bare slug.
		{name: "bare owner/repo", input: "owner/repo", wantSlug: "owner/repo", wantOK: true},
		{name: "bare owner/repo.git", input: "owner/repo.git", wantSlug: "owner/repo", wantOK: true},
		// GitLab subgroup forms (regression: these were silently dropped).
		{name: "gitlab subgroup 3-segment", input: "https://gitlab.com/group/sub/repo", wantSlug: "group/sub/repo", wantOK: true},
		{name: "gitlab subgroup 4-segment", input: "https://gitlab.com/group/sub/sub2/repo", wantSlug: "group/sub/sub2/repo", wantOK: true},
		{name: "gitlab subgroup ssh", input: "git@gitlab.com:group/sub/repo.git", wantSlug: "group/sub/repo", wantOK: true},
		{name: "gitlab 2-segment still ok", input: "https://gitlab.com/group/sub", wantSlug: "group/sub", wantOK: true},
		{name: "gitlab 1-segment rejected", input: "https://gitlab.com/group", wantSlug: "", wantOK: false},
		// Rejection cases.
		{name: "single segment", input: "single", wantSlug: "", wantOK: false},
		{name: "double .git suffix", input: "owner/repo.git.git", wantSlug: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSlug, gotOK := ExtractSlug(tc.input)
			if gotSlug != tc.wantSlug || gotOK != tc.wantOK {
				t.Errorf("ExtractSlug(%q) = (%q, %v), want (%q, %v)",
					tc.input, gotSlug, gotOK, tc.wantSlug, tc.wantOK)
			}
		})
	}
}

func TestExtractOwnerRepo(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"/home/user/repo", false},
		{"./repo", false},
		{"../repo", false},
		// HTTPS URL forms.
		{"https://github.com/foo/bar", true},
		{"https://gitlab.com/foo/bar", true},
		{"https://bitbucket.org/foo/bar", false},
		// SSH forms.
		{"git@github.com:owner/repo.git", true},
		{"git@gitlab.com:owner/repo.git", true},
		{"git@evil.com:owner/repo.git", false},
		// Bare host-prefix forms.
		{"github.com/owner/repo", true},
		{"gitlab.com/owner/repo", true},
		// Plain bare slug.
		{"foo/bar", true},
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
	t.Parallel()
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
