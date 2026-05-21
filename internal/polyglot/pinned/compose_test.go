package pinned

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestParseCompose(t *testing.T) {
	// Strict interpolation rule: NEVER honour os.LookupEnv. Set a process env
	// var that would tempt a buggy implementation; the parser must ignore it.
	t.Setenv("PG_VER", "leaked-from-env")
	t.Setenv("BARE_IMAGE", "leaked-from-env")
	t.Setenv("MARIADB_VER", "leaked-from-env")

	tests := []struct {
		name    string
		fixture string
		want    []PinnedImage
	}{
		{
			name:    "simple two-service compose",
			fixture: "compose-simple.yml",
			want: []PinnedImage{
				{Image: "nginx", Tag: "1.27-alpine", Service: "web"},
				{Image: "redis", Tag: "7.4", Service: "cache"},
			},
		},
		{
			name:    "yaml anchor resolves to same image",
			fixture: "compose-anchor.yml",
			want: []PinnedImage{
				{Image: "redis", Tag: "7.4", Service: "a"},
				{Image: "redis", Tag: "7.4", Service: "b"},
			},
		},
		{
			name:    "variable interpolation uses defaults only, never process env",
			fixture: "compose-vars.yml",
			want: []PinnedImage{
				// with-default: postgres:${PG_VER:-16} -> uses ":-16" literal,
				// NOT the leaked-from-env in process.
				{Image: "postgres", Tag: "16", Service: "with-default"},
				// bare: ${BARE_IMAGE}:latest -> no default, must NOT honour
				// process env; emit Unresolved.
				{
					Image:      "",
					Tag:        "latest",
					Service:    "bare",
					Unresolved: "${BARE_IMAGE} has no default in compose, not honouring process env",
				},
				// full-default: ${IMG:-mariadb}:${MARIADB_VER:-11.4}
				{Image: "mariadb", Tag: "11.4", Service: "full-default"},
			},
		},
		{
			name:    "build-only services skipped, image-bearing kept",
			fixture: "compose-build-only.yml",
			want: []PinnedImage{
				{Image: "ghcr.io/example/worker", Tag: "1.0", Service: "worker"},
			},
		},
		{
			name:    "registry with port colon does not break tag split",
			fixture: "compose-registry-port.yml",
			want: []PinnedImage{
				{Image: "localhost:5000/my/image", Tag: "1.2.3", Service: "custom"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", tc.fixture)
			got, err := ParseCompose(path)
			if err != nil {
				t.Fatalf("ParseCompose(%s) error: %v", tc.fixture, err)
			}
			// Compose ordering depends on yaml map iteration, normalise.
			sortPinnedByService(got)
			want := append([]PinnedImage(nil), tc.want...)
			sortPinnedByService(want)
			if len(got) != len(want) {
				t.Fatalf("len(got)=%d want %d\ngot: %#v", len(got), len(want), got)
			}
			for i := range got {
				w := want[i]
				w.Source = path
				// Compose line numbers are sensitive to yaml lib; assert
				// non-zero rather than exact value.
				if got[i].Line <= 0 {
					t.Errorf("image[%d]: expected Line>0, got %d", i, got[i].Line)
				}
				gotNoLine := got[i]
				gotNoLine.Line = 0
				if gotNoLine != w {
					t.Errorf("image[%d]: got %#v want %#v", i, got[i], w)
				}
			}
		})
	}
}

func TestParseCompose_Malformed(t *testing.T) {
	_, err := ParseCompose("testdata/compose-malformed.yml")
	if err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

func TestParseCompose_Missing(t *testing.T) {
	_, err := ParseCompose("testdata/nope.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		// Wrap acceptable too — just want non-nil.
		t.Logf("non-IsNotExist error (acceptable): %v", err)
	}
}

func sortPinnedByService(p []PinnedImage) {
	sort.SliceStable(p, func(i, j int) bool { return p[i].Service < p[j].Service })
}
