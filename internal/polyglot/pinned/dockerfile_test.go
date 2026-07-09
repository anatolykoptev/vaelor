package pinned

import (
	"path/filepath"
	"testing"
)

func TestParseDockerfile(t *testing.T) {
	t.Parallel()
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
			name:    "ARG interpolation deferred \u2014 emit with Unresolved",
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
		{
			// Regression for intToStr bug: byte(0 + n%10) produced control chars
			// instead of ASCII digits for indices >= 1. Service names for unnamed
			// non-final stages must use printable ASCII digit characters.
			name:    "multi-stage with three unnamed stages \u2014 service names use ASCII digits",
			fixture: "Dockerfile.multistage-unnamed.dockerfile",
			want: []PinnedImage{
				{Image: "alpine", Tag: "3.20", Service: "stage0:builder", Line: 1},
				{Image: "debian", Tag: "12", Service: "stage1:builder", Line: 4},
				{Image: "ubuntu", Tag: "24.04", Service: "", Line: 7},
			},
		},
		{
			// FROM <stage-name> references an intra-Dockerfile AS-name and must not
			// emit a PinnedImage. Only the first FROM (the real external image) emits.
			//
			// Design choice A: skipped FROMs still count for final-stage determination
			// (physical FROM index). So node:18-alpine at idx=0 is non-final (last
			// physical FROM is idx=3), giving Service="base:builder".
			//
			// Skip predicate: image-ref is a bare identifier (no :, @, /) AND
			// matches a prior AS-name in this file. If base were declared elsewhere
			// but NOT as an AS-name here, it would still emit (treated as external).
			name:    "AS-stage reuse via FROM stage-name does not emit pinned",
			fixture: "Dockerfile.stage-reuse",
			want: []PinnedImage{
				// Only the first FROM (real external image) emits.
				// Three subsequent FROMs are intra-stage refs (base is declared AS-name)
				// and are skipped. Physical FROM count is 4, so node:18-alpine is
				// non-final => Service="base:builder".
				{Image: "node", Tag: "18-alpine", Service: "base:builder", Line: 1},
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
	t.Parallel()
	_, err := ParseDockerfile("testdata/nope.Dockerfile")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
