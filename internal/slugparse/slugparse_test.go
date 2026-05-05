package slugparse

import (
	"testing"
)

func TestParseWithOptions(t *testing.T) {
	t.Run("AllowSubgroups=true", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			want    string
			wantErr bool
		}{
			// Subgroup HTTPS URLs.
			{name: "gitlab 3-segment https", input: "https://gitlab.com/group/sub/repo", want: "group/sub/repo"},
			{name: "gitlab 4-segment https", input: "https://gitlab.com/group/sub/sub2/repo", want: "group/sub/sub2/repo"},
			{name: "gitlab 3-segment https with .git", input: "https://gitlab.com/group/sub/repo.git", want: "group/sub/repo"},
			// Subgroup SSH URL.
			{name: "gitlab ssh 3-segment", input: "git@gitlab.com:group/sub/repo.git", want: "group/sub/repo"},
			// 2-segment still accepted.
			{name: "gitlab 2-segment https", input: "https://gitlab.com/group/sub", want: "group/sub"},
			// 1-segment rejected.
			{name: "gitlab 1-segment rejected", input: "https://gitlab.com/group", wantErr: true},
		}
		opts := Options{AllowSubgroups: true}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := ParseWithOptions(tc.input, opts)
				if tc.wantErr {
					if err == nil {
						t.Errorf("ParseWithOptions(%q) = %q, want error", tc.input, got)
					}
					return
				}
				if err != nil {
					t.Errorf("ParseWithOptions(%q) unexpected error: %v", tc.input, err)
					return
				}
				if got != tc.want {
					t.Errorf("ParseWithOptions(%q) = %q, want %q", tc.input, got, tc.want)
				}
			})
		}
	})

	t.Run("strict mode rejects subgroups", func(t *testing.T) {
		// Parse (no options) must still reject 3-segment paths.
		_, err := Parse("group/sub/repo")
		if err == nil {
			t.Error("Parse(\"group/sub/repo\") = nil error, want error (strict mode)")
		}
	})
}

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
