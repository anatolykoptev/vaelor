package artifactfilter_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/artifactfilter"
)

func TestIsCompiledArtifact_ServiceWorkers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"web/static/sw.js", true},
		{"assets/room/sw.js", true},
		{"sw.js", true},
		{"web/static/service-worker.js", true},
		{"web/static/workbox-abc123.js", true},
		{"src/lib/workbox-window.ts", false},           // a real .ts source file that merely starts with "workbox"
		{"web/src/lib/i18n/translations/ru.ts", false}, // real source — must NOT be filtered
		{"internal/sw/handler.go", false},              // "sw" path segment but a .go source file
	}
	for _, c := range cases {
		if got := artifactfilter.IsCompiledArtifact(c.path); got != c.want {
			t.Errorf("IsCompiledArtifact(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsCompiledArtifact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		// Known build-output directory components.
		{"web/_app/immutable/entry/app.js", true},
		{"assets/_app/index.html", true},
		{".svelte-kit/output/client/app.js", true},
		{".next/static/chunks/main.js", true},
		{".nuxt/dist/client/app.js", true},
		{"dist/bundle.js", true},
		{"dist/styles.css", true},
		// Compiled extensions.
		{"src/app.min.js", true},
		{"src/theme.min.css", true},
		// HTML under assets/.
		{"assets/pages/index.html", true},
		// Content-hashed JS/CSS (8+ char hash, mixed classes).
		{"static/chunk.CFDNM_nG.js", true},
		{"static/styles.BtfDG5yP.css", true},
		{"public/app.Ab3Cd4Ef.js", true},
		// Ordinary source files — must return false.
		{"internal/foo/bar.go", false},
		{"src/app.svelte", false},
		{"web/src/lib/store.ts", false},
		{"README.md", false},
		{"cmd/go-code/main.go", false},
		// JS without a content hash — must return false.
		{"src/helper.js", false},
		{"src/app.bundle.js", false}, // "bundle" is only 6 chars, not a hash
		// CSS without content hash.
		{"static/styles.css", false},
		// HTML not under assets/.
		{"templates/index.html", false},
		{"web/index.html", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got := artifactfilter.IsCompiledArtifact(tc.path)
			if got != tc.want {
				t.Errorf("IsCompiledArtifact(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
