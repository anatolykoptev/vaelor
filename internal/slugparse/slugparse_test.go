package slugparse

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		// Empty and local paths rejected.
		{name: "empty", input: "", wantErr: true},
		{name: "absolute path", input: "/home/user/repo", wantErr: true},
		{name: "relative path ./", input: "./repo", wantErr: true},
		{name: "relative path ../", input: "../repo", wantErr: true},
		// Bare owner/repo.
		{name: "bare owner/repo", input: "owner/repo", want: "owner/repo"},
		{name: "bare owner/repo.git", input: "owner/repo.git", want: "owner/repo"},
		// Bare host-prefix forms (primary regression trigger).
		{name: "github.com/owner/repo", input: "github.com/owner/repo", want: "owner/repo"},
		{name: "github.com/owner/repo.git", input: "github.com/owner/repo.git", want: "owner/repo"},
		{name: "gitlab.com/owner/repo", input: "gitlab.com/owner/repo", want: "owner/repo"},
		{name: "gitlab.com/owner/repo.git", input: "gitlab.com/owner/repo.git", want: "owner/repo"},
		// HTTPS URL forms.
		{name: "https github no .git", input: "https://github.com/owner/repo", want: "owner/repo"},
		{name: "https github with .git", input: "https://github.com/owner/repo.git", want: "owner/repo"},
		{name: "http github", input: "http://github.com/owner/repo", want: "owner/repo"},
		{name: "https gitlab no .git", input: "https://gitlab.com/owner/repo", want: "owner/repo"},
		{name: "https gitlab with .git", input: "https://gitlab.com/owner/repo.git", want: "owner/repo"},
		// SSH forms.
		{name: "ssh github", input: "git@github.com:owner/repo.git", want: "owner/repo"},
		{name: "ssh gitlab", input: "git@gitlab.com:owner/repo.git", want: "owner/repo"},
		{name: "ssh github no .git", input: "git@github.com:owner/repo", want: "owner/repo"},
		// Rejection cases.
		{name: "ssh unknown host rejected", input: "git@evil.com:owner/repo.git", wantErr: true},
		{name: "ssh no colon", input: "git@github.com", wantErr: true},
		{name: "single segment", input: "owner", wantErr: true},
		{name: "too many segments", input: "owner/repo/extra", wantErr: true},
		{name: "double .git suffix", input: "owner/repo.git.git", wantErr: true},
		{name: "garbage input", input: "garbage", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Parse(%q) = %q, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("Parse(%q) unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("Parse(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
