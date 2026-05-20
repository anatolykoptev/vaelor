package pinned

import (
	"path/filepath"
	"testing"
)

func TestParseDockerfile(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    []PinnedImage
	}{
		{
			name:    "simple single-stage with explicit tag",
			fixture: "Dockerfile.simple",
			want: []PinnedImage{
				{Image: "nginx", Tag: "1.27-alpine", Service: "", Line: 2},
			},
		},
		{
			name:    "implicit latest tag",
			fixture: "Dockerfile.implicit-latest",
			want: []PinnedImage{
				{Image: "nginx", Tag: "latest", Service: "", Line: 1},
			},
		},
		{
			name:    "multi-stage marks non-final as builder",
			fixture: "Dockerfile.multistage",
			want: []PinnedImage{
				{Image: "golang", Tag: "1.26-alpine", Service: "builder:builder", Line: 2},
				{Image: "alpine", Tag: "3.20", Service: "runtime", Line: 7},
			},
		},
		{
			name:    "digest-only and tag+digest combined",
			fixture: "Dockerfile.digest",
			want: []PinnedImage{
				{
					Image:   "redis",
					Tag:     "",
					Digest:  "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					Service: "stage0:builder",
					Line:    1,
				},
				{
					Image:   "postgres",
					Tag:     "16",
					Digest:  "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					Service: "db",
					Line:    2,
				},
			},
		},
		{
			name:    "ARG interpolation deferred — emit with Unresolved",
			fixture: "Dockerfile.arg",
			want: []PinnedImage{
				{
					Image:      "$BASE",
					Tag:        "",
					Service:    "builder:builder",
					Line:       2,
					Unresolved: "ARG interpolation not supported: FROM $BASE",
				},
				{Image: "alpine", Tag: "3.20", Service: "", Line: 4},
			},
		},
		{
			name:    "line continuation collapses to logical FROM",
			fixture: "Dockerfile.continuation",
			want: []PinnedImage{
				{Image: "golang", Tag: "1.26-alpine", Service: "", Line: 1},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", tc.fixture)
			got, err := ParseDockerfile(path)
			if err != nil {
				t.Fatalf("ParseDockerfile(%s) error: %v", tc.fixture, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d want %d\ngot: %#v", len(got), len(tc.want), got)
			}
			for i := range got {
				w := tc.want[i]
				w.Source = path
				if got[i] != w {
					t.Errorf("image[%d]: got %#v want %#v", i, got[i], w)
				}
			}
		})
	}
}

func TestParseDockerfile_NonExistent(t *testing.T) {
	_, err := ParseDockerfile("testdata/nope.Dockerfile")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
