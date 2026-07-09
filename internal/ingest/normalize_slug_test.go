package ingest

import (
	"testing"
)

func TestNormalizeSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "bare owner/repo",
			input: "owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "github.com/owner/repo",
			input: "github.com/owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "github.com/owner/repo.git",
			input: "github.com/owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "https://github.com/owner/repo",
			input: "https://github.com/owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "https://github.com/owner/repo.git",
			input: "https://github.com/owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "http://github.com/owner/repo",
			input: "http://github.com/owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "git@github.com SSH form",
			input: "git@github.com:owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:    "garbage input",
			input:   "garbage",
			wantErr: true,
		},
		{
			name:    "owner only — missing repo",
			input:   "owner",
			wantErr: true,
		},
		{
			name:    "too many path segments",
			input:   "owner/repo/extra",
			wantErr: true,
		},
		{
			name:    "SSH unknown host rejected",
			input:   "git@evil.com:owner/repo.git",
			wantErr: true,
		},
		{
			name:  "git@gitlab.com SSH form",
			input: "git@gitlab.com:owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:    "double .git suffix rejected",
			input:   "owner/repo.git.git",
			wantErr: true,
		},
		{
			name:    "SSH no colon",
			input:   "git@github.com",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeSlug(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("NormalizeSlug(%q) = %q, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("NormalizeSlug(%q) unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("NormalizeSlug(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
