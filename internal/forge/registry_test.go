package forge

import "testing"

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()
	r.Register(GitHub, NewGitHubForge(""))
	r.Register(GitLab, NewGitLabForge("", ""))

	if f := r.Get(GitHub); f == nil || f.Kind() != GitHub {
		t.Error("expected GitHub forge")
	}
	if f := r.Get(GitLab); f == nil || f.Kind() != GitLab {
		t.Error("expected GitLab forge")
	}
	if f := r.Get(Unknown); f != nil {
		t.Error("expected nil for Unknown")
	}
}

func TestRegistryForURL(t *testing.T) {
	r := NewRegistry()
	r.Register(GitHub, NewGitHubForge(""))
	r.Register(GitLab, NewGitLabForge("", ""))

	tests := []struct {
		input string
		kind  ForgeKind
	}{
		{"https://github.com/foo/bar", GitHub},
		{"https://gitlab.com/group/repo", GitLab},
		{"foo/bar", GitHub},
		{"/local/path", Unknown},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			f := r.ForURL(tc.input)
			if tc.kind == Unknown {
				if f != nil {
					t.Errorf("expected nil, got %v", f.Kind())
				}
			} else if f == nil || f.Kind() != tc.kind {
				t.Errorf("got %v, want %v", f, tc.kind)
			}
		})
	}
}

func TestRegistryDefault(t *testing.T) {
	r := NewRegistry()
	r.Register(GitHub, NewGitHubForge(""))

	// Bare slug resolves to GitHub.
	if f := r.ForURL("foo/bar"); f == nil || f.Kind() != GitHub {
		t.Error("bare slug should resolve to GitHub")
	}
	// GitLab URL returns nil if not registered.
	if f := r.ForURL("https://gitlab.com/foo/bar"); f != nil {
		t.Error("GitLab URL should return nil when not registered")
	}
}
